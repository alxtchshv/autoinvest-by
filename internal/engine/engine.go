// Package engine — бизнес-логика копилок: пополнение (списание с карты + покупка активов),
// вывод, оценка портфеля. Каждая операция идемпотентна и защищена от двойного списания.
package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"autoinvest/internal/config"
	"autoinvest/internal/market"
	"autoinvest/internal/money"
	"autoinvest/internal/notify"
	"autoinvest/internal/payments"
	"autoinvest/internal/store"
)

var (
	ErrPlanClosed     = errors.New("Копилка закрыта")
	ErrNoCard         = errors.New("Привяжите карту, чтобы пополнять копилку")
	ErrKYCRequired    = errors.New("Пройдите верификацию личности, чтобы инвестировать")
	ErrProRequired    = errors.New("Акции и токены компаний доступны на тарифе Pro")
	ErrAmountTooSmall = errors.New("Сумма меньше минимальной")
	ErrAmountTooLarge = errors.New("Сумма больше максимальной")
	ErrNotEnough      = errors.New("В копилке меньше, чем вы хотите вывести")
	ErrMarket         = errors.New("Биржевые котировки сейчас недоступны, попробуйте через несколько минут")
	ErrBusy           = errors.New("По копилке уже выполняется операция")
)

// Broker исполняет сделки. Paper — учебный счёт по реальным ценам.
type Broker interface {
	Name() string
	// Price — цена исполнения покупки/продажи в BYN за 1 единицу.
	Price(q market.Quote, side string) (float64, error)
}

type PaperBroker struct{ Hub *market.Hub }

func (PaperBroker) Name() string { return config.BrokerPaper }
func (b PaperBroker) Price(q market.Quote, side string) (float64, error) {
	if side == "buy" {
		return b.Hub.BuyPriceBYN(q), nil
	}
	return b.Hub.SellPriceBYN(q), nil
}

// WhitebirdBroker — место подключения партнёрского API после подписания договора.
// Публичного API для агрегаторов нет, спецификацию выдаёт партнёр; до этого сделки не исполняются.
type WhitebirdBroker struct{ URL, Key, Secret string }

func (WhitebirdBroker) Name() string { return config.BrokerWhitebird }
func (WhitebirdBroker) Price(q market.Quote, side string) (float64, error) {
	return 0, errors.New("шлюз Whitebird не подключён: нужна спецификация API от партнёра")
}

type Engine struct {
	St     *store.Store
	DB     *sql.DB
	Hub    *market.Hub
	Pay    payments.Provider
	Broker Broker
	N      *notify.Notifier
	Cfg    *config.Config
	Log    *slog.Logger

	locks sync.Map // planID → *sync.Mutex
}

func (e *Engine) lock(planID int64) (func(), bool) {
	v, _ := e.locks.LoadOrStore(planID, &sync.Mutex{})
	m := v.(*sync.Mutex)
	if !m.TryLock() {
		return nil, false
	}
	return m.Unlock, true
}

// FeeBP — комиссия сервиса для пользователя, в базисных пунктах.
func (e *Engine) FeeBP(u *store.User) int64 {
	if u == nil {
		return e.Cfg.FeeFreeBP
	}
	return e.feeBP(u)
}

func (e *Engine) feeBP(u *store.User) int64 {
	if u.IsPro() {
		return e.Cfg.FeeProBP
	}
	return e.Cfg.FeeFreeBP
}

// ---------------------------------------------------------------- покупка

// BuyResult — итог пополнения.
type BuyResult struct {
	Order   *store.Order
	Retry   bool // временная ошибка (рынок/сеть) — повторить позже
	Decline bool // отказ банка
}

// Buy пополняет копилку: списывает amount с карты и покупает активы по долям.
// idemKey гарантирует, что одна и та же операция не выполнится дважды.
func (e *Engine) Buy(ctx context.Context, planID, amount int64, trigger, idemKey string) (BuyResult, error) {
	unlock, ok := e.lock(planID)
	if !ok {
		return BuyResult{Retry: true}, ErrBusy
	}
	defer unlock()

	if prev := e.orderByIdem(ctx, idemKey); prev != nil {
		return BuyResult{Order: prev}, nil
	}
	p, err := e.St.PlanByID(ctx, planID)
	if err != nil {
		return BuyResult{}, err
	}
	if p.Status == "closed" {
		return BuyResult{}, ErrPlanClosed
	}
	u, err := e.St.UserByID(ctx, p.UserID)
	if err != nil {
		return BuyResult{}, err
	}
	if e.Cfg.RequireKYC && u.KYCStatus != "approved" {
		return BuyResult{}, ErrKYCRequired
	}
	// Акции и токены компаний доступны только на Pro: если подписка закончилась, пополнение не проходит.
	for _, a := range p.Allocations {
		if asset, ok := market.Find(a.Asset); ok && asset.ProOnly && !u.IsPro() {
			return BuyResult{}, fmt.Errorf("%w: %s", ErrProRequired, asset.Name)
		}
	}
	if amount < e.Cfg.MinContribKop {
		return BuyResult{}, fmt.Errorf("%w: %s", ErrAmountTooSmall, money.FormatBYN(e.Cfg.MinContribKop))
	}
	if amount > e.Cfg.MaxContribKop {
		return BuyResult{}, fmt.Errorf("%w: %s", ErrAmountTooLarge, money.FormatBYN(e.Cfg.MaxContribKop))
	}
	if p.CardID == nil {
		return BuyResult{}, ErrNoCard
	}
	card, err := e.St.Card(ctx, p.UserID, *p.CardID)
	if err != nil {
		return BuyResult{}, ErrNoCard
	}
	// Котировки проверяем ДО списания: без цены деньги не трогаем.
	quotes := map[string]market.Quote{}
	for _, a := range p.Allocations {
		q, err := e.Hub.TradeQuote(a.Asset)
		if err != nil {
			return BuyResult{Retry: true}, ErrMarket
		}
		quotes[a.Asset] = q
	}
	if len(quotes) == 0 {
		return BuyResult{}, errors.New("у копилки не задан состав")
	}

	fee := money.BP(amount, e.feeBP(u))
	now := store.Now()
	rates := e.Hub.Rates()
	// 1. Заявка и платёж (pending) — фиксируем намерение до похода в банк.
	var orderID, paymentID int64
	err = withTx(ctx, e.DB, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO orders(user_id, plan_id, side, trigger, status, amount_kop, fee_kop, idem_key, usd_byn, broker, created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`, u.ID, p.ID, "buy", trigger, "pending", amount, fee, idemKey, rates.USD, e.Broker.Name(), now)
		if err != nil {
			return err
		}
		orderID, _ = res.LastInsertId()
		res, err = tx.ExecContext(ctx, `INSERT INTO payments(user_id, card_id, provider, kind, tracking_id, amount_kop, status, test, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, u.ID, card.ID, e.Pay.Name(), "charge", fmt.Sprintf("ord_%d", orderID), amount, "pending", e.Pay.Test(), now, now)
		if err != nil {
			return err
		}
		paymentID, _ = res.LastInsertId()
		_, err = tx.ExecContext(ctx, `UPDATE orders SET payment_id=? WHERE id=?`, paymentID, orderID)
		return err
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			if prev := e.orderByIdem(ctx, idemKey); prev != nil {
				return BuyResult{Order: prev}, nil
			}
		}
		return BuyResult{}, err
	}

	// 2. Списание.
	desc := fmt.Sprintf("Пополнение копилки «%s»", p.Name)
	cr, err := e.Pay.Charge(ctx, payments.ChargeRequest{Token: card.Token, AmountKop: amount, Description: desc, TrackingID: fmt.Sprintf("ord_%d", orderID), Email: u.Email})
	if err != nil {
		// Сеть оборвалась: уточняем статус по tracking_id; если не вышло — сверка доведёт заявку позже.
		cr, err = e.Pay.ChargeStatus(ctx, fmt.Sprintf("ord_%d", orderID))
		if err != nil {
			e.Log.Warn("charge status unknown, left pending", "order", orderID, "err", err)
			o, _ := e.St.Order(ctx, 0, orderID)
			return BuyResult{Order: o, Retry: true}, nil
		}
	}
	return e.settleBuy(ctx, orderID, paymentID, cr)
}

func (e *Engine) orderByIdem(ctx context.Context, key string) *store.Order {
	var id int64
	if err := e.DB.QueryRowContext(ctx, `SELECT id FROM orders WHERE idem_key=?`, key).Scan(&id); err != nil {
		return nil
	}
	o, _ := e.St.Order(ctx, 0, id)
	return o
}

// settleBuy доводит заявку после ответа банка: исполняет покупку или фиксирует отказ.
// Все переходы статусов условные (WHERE status=...), поэтому повторные и конкурентные вызовы безопасны.
func (e *Engine) settleBuy(ctx context.Context, orderID, paymentID int64, cr payments.ChargeResult) (BuyResult, error) {
	o, err := e.St.Order(ctx, 0, orderID)
	if err != nil {
		return BuyResult{}, err
	}
	now := store.Now()
	if o.Status != "pending" {
		if cr.Status == "successful" && o.Status == "failed" {
			// Банк подтвердил списание уже после отказа: деньги у клиента взяты — нужен ручной разбор (исполнить или вернуть).
			res, _ := e.DB.ExecContext(ctx, `UPDATE orders SET status='manual', error=? WHERE id=? AND status='failed'`,
				"Банк подтвердил списание после отказа — исполнить покупку или оформить возврат", orderID)
			if n, _ := res.RowsAffected(); n > 0 {
				e.DB.ExecContext(ctx, `UPDATE payments SET status='successful', provider_uid=?, updated_at=? WHERE id=?`, cr.UID, now, paymentID)
				e.Log.Error("late success after failed order", "order", orderID)
				e.St.Audit(ctx, o.UserID, 0, "order.late_success", fmt.Sprintf("order %d", orderID), "")
				e.N.Send(ctx, o.UserID, notify.Message{Kind: "purchase_failed", Title: "Списание " + money.FormatBYN(o.AmountKop) + " подтверждено банком позже",
					Body: "Мы исполним покупку или вернём деньги в течение рабочего дня.", Link: fmt.Sprintf("/app/history/%d", orderID), Email: true})
			}
			o, _ = e.St.Order(ctx, 0, orderID)
		}
		return BuyResult{Order: o}, nil
	}
	switch cr.Status {
	case "pending":
		e.DB.ExecContext(ctx, `UPDATE payments SET provider_uid=?, message=?, updated_at=? WHERE id=?`, cr.UID, cr.Message, now, paymentID)
		return BuyResult{Order: o, Retry: true}, nil
	case "failed":
		msg := cr.Message
		if msg == "" {
			msg = "Банк отклонил списание"
		}
		res, err := e.DB.ExecContext(ctx, `UPDATE orders SET status='failed', error=? WHERE id=? AND status='pending'`, msg, orderID)
		if err != nil {
			return BuyResult{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			o, _ = e.St.Order(ctx, 0, orderID)
			return BuyResult{Order: o}, nil
		}
		e.DB.ExecContext(ctx, `UPDATE payments SET status='failed', provider_uid=?, message=?, updated_at=? WHERE id=?`, cr.UID, msg, now, paymentID)
		e.N.Send(ctx, o.UserID, notify.Message{Kind: "purchase_failed", Title: "Не удалось пополнить копилку «" + o.PlanName + "»",
			Body: msg + ". Сумма " + money.FormatBYN(o.AmountKop) + " не списана.", Link: fmt.Sprintf("/app/plans/%d", o.PlanID), Email: true})
		if o.Trigger == "schedule" {
			e.registerDecline(ctx, o.PlanID)
		}
		o.Status, o.Error = "failed", msg
		return BuyResult{Order: o, Decline: true}, nil
	}
	// successful: фиксируем, что деньги получены, и исполняем покупку
	e.DB.ExecContext(ctx, `UPDATE payments SET status='successful', provider_uid=?, message=?, updated_at=? WHERE id=?`, cr.UID, cr.Message, now, paymentID)
	return e.fill(ctx, o, "pending")
}

// registerDecline — счётчик отказов по расписанию; после трёх подряд копилка встаёт на паузу.
func (e *Engine) registerDecline(ctx context.Context, planID int64) {
	e.DB.ExecContext(ctx, `UPDATE plans SET fail_count=fail_count+1, updated_at=? WHERE id=?`, store.Now(), planID)
	var fails int
	var uid int64
	var name string
	e.DB.QueryRowContext(ctx, `SELECT fail_count, user_id, name FROM plans WHERE id=?`, planID).Scan(&fails, &uid, &name)
	if fails >= 3 {
		e.pausePlan(ctx, planID, uid, "Копилка «"+name+"» поставлена на паузу", "Три списания подряд не прошли. Проверьте карту и включите копилку снова.")
	}
}

// pausePlan ставит активную копилку на паузу (точечным UPDATE, чтобы не затереть параллельные изменения).
func (e *Engine) pausePlan(ctx context.Context, planID, userID int64, title, body string) {
	res, err := e.DB.ExecContext(ctx, `UPDATE plans SET status='paused', next_run_at=NULL, updated_at=? WHERE id=? AND status='active'`, store.Now(), planID)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		e.N.Send(ctx, userID, notify.Message{Kind: "purchase_failed", Title: title, Body: body, Link: fmt.Sprintf("/app/plans/%d", planID), Email: true})
		e.N.Publish(userID, notify.UserEvent{Type: "plans"})
	}
}

// fill исполняет покупку по оплаченной заявке. from — ожидаемый текущий статус (pending или manual).
func (e *Engine) fill(ctx context.Context, o *store.Order, from string) (BuyResult, error) {
	p, err := e.St.PlanByID(ctx, o.PlanID)
	if err != nil {
		return BuyResult{}, err
	}
	net := o.AmountKop - o.FeeKop
	type fill struct {
		asset       string
		qty, amount int64
		pb, pu      float64
	}
	var fills []fill
	var rest = net
	for i, a := range p.Allocations {
		part := net * a.WeightBP / 10000
		if i == len(p.Allocations)-1 {
			part = rest
		}
		rest -= part
		if part <= 0 {
			continue
		}
		q, err := e.Hub.TradeQuote(a.Asset)
		if err != nil {
			// деньги получены, но свежей цены нет — заявка остаётся в обработке, сверка исполнит её, когда появятся котировки
			e.Log.Warn("no fresh quote after charge, will retry", "order", o.ID, "asset", a.Asset)
			return BuyResult{Order: o, Retry: true}, nil
		}
		price, err := e.Broker.Price(q, "buy")
		if err != nil || price <= 0 {
			e.markManual(ctx, o.ID, from, "Брокер не исполнил заявку: "+errString(err))
			o, _ = e.St.Order(ctx, 0, o.ID)
			return BuyResult{Order: o}, nil
		}
		qty := money.QtyFromFloat(float64(part) / 100 / price)
		fills = append(fills, fill{a.Asset, qty, part, price, price / math.Max(e.Hub.Rates().USD, 1e-9)})
	}
	now := store.Now()
	err = withTx(ctx, e.DB, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE orders SET status='filled', filled_at=?, error='' WHERE id=? AND status=?`, now, o.ID, from)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errAlreadySettled
		}
		for _, f := range fills {
			if _, err := tx.ExecContext(ctx, `INSERT INTO fills(order_id, asset, qty, price_byn, price_usd, amount_kop) VALUES(?,?,?,?,?,?)`, o.ID, f.asset, f.qty, f.pb, f.pu, f.amount); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO positions(plan_id, asset, qty, cost_kop) VALUES(?,?,?,?)
				ON CONFLICT(plan_id, asset) DO UPDATE SET qty=qty+excluded.qty, cost_kop=cost_kop+excluded.cost_kop`, o.PlanID, f.asset, f.qty, f.amount); err != nil {
				return err
			}
		}
		if o.Trigger == "schedule" {
			tx.ExecContext(ctx, `UPDATE plans SET fail_count=0, updated_at=? WHERE id=?`, now, o.PlanID)
		}
		return nil
	})
	if errors.Is(err, errAlreadySettled) {
		o, _ = e.St.Order(ctx, 0, o.ID)
		return BuyResult{Order: o}, nil
	}
	if err != nil {
		return BuyResult{}, err
	}
	o, _ = e.St.Order(ctx, 0, o.ID)
	var parts []string
	for _, f := range fills {
		parts = append(parts, fmt.Sprintf("%s %s", trimQty(f.qty), f.asset))
	}
	title := "Копилка «" + o.PlanName + "» пополнена на " + money.FormatBYN(o.AmountKop)
	e.N.Send(ctx, o.UserID, notify.Message{Kind: "purchase", Title: title, Body: "Куплено: " + strings.Join(parts, ", "), Link: fmt.Sprintf("/app/plans/%d", o.PlanID)})
	e.N.Publish(o.UserID, notify.UserEvent{Type: "plans"})
	e.SnapshotUser(ctx, o.UserID)
	e.rewardReferral(ctx, o.UserID)
	return BuyResult{Order: o}, nil
}

var errAlreadySettled = errors.New("already settled")

func errString(err error) string {
	if err == nil {
		return "неизвестная ошибка"
	}
	return err.Error()
}

func (e *Engine) markManual(ctx context.Context, orderID int64, from, msg string) {
	e.DB.ExecContext(ctx, `UPDATE orders SET status='manual', error=? WHERE id=? AND status=?`, msg, orderID, from)
	e.Log.Error("order needs manual handling", "order", orderID, "reason", msg)
}

// FillManual — оператор исполняет оплаченную заявку, застрявшую на ручной проверке.
func (e *Engine) FillManual(ctx context.Context, orderID, actor int64) error {
	o, err := e.St.Order(ctx, 0, orderID)
	if err != nil {
		return err
	}
	if o.Status != "manual" || o.Side != "buy" {
		return errors.New("заявка не ожидает ручного исполнения")
	}
	unlock, ok := e.lock(o.PlanID)
	if !ok {
		return ErrBusy
	}
	defer unlock()
	res, err := e.fill(ctx, o, "manual")
	if err != nil {
		return err
	}
	if res.Retry {
		return ErrMarket
	}
	e.St.Audit(ctx, o.UserID, actor, "order.manual_fill", fmt.Sprintf("order %d", orderID), "")
	return nil
}

// RefundManual — оператор возвращает деньги по заявке, которую нельзя исполнить.
func (e *Engine) RefundManual(ctx context.Context, orderID, actor int64) error {
	o, err := e.St.Order(ctx, 0, orderID)
	if err != nil {
		return err
	}
	if o.Status != "manual" || o.Side != "buy" {
		return errors.New("заявка не ожидает ручного разбора")
	}
	if o.PaymentUID != "" {
		if _, err := e.Pay.Refund(ctx, o.PaymentUID, o.AmountKop, "Покупка не исполнена"); err != nil {
			return fmt.Errorf("возврат через платёжный шлюз не прошёл: %w", err)
		}
	}
	res, _ := e.DB.ExecContext(ctx, `UPDATE orders SET status='refunded', error='Деньги возвращены на карту' WHERE id=? AND status='manual'`, orderID)
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("заявка уже обработана")
	}
	e.DB.ExecContext(ctx, `UPDATE payments SET status='refunded', updated_at=? WHERE id=(SELECT payment_id FROM orders WHERE id=?)`, store.Now(), orderID)
	e.St.Audit(ctx, o.UserID, actor, "order.refund", fmt.Sprintf("order %d, %s", orderID, money.FormatBYN(o.AmountKop)), "")
	e.N.Send(ctx, o.UserID, notify.Message{Kind: "payout", Title: "Возврат " + money.FormatBYN(o.AmountKop), Body: "Покупку по копилке «" + o.PlanName + "» не удалось исполнить, деньги возвращены на карту.", Link: fmt.Sprintf("/app/history/%d", orderID), Email: true})
	return nil
}

func trimQty(q int64) string {
	s := fmt.Sprintf("%.8f", money.QtyToFloat(q))
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return strings.Replace(s, ".", ",", 1)
}

func withTx(ctx context.Context, d *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ---------------------------------------------------------------- вывод

// Withdraw продаёт часть активов копилки и выплачивает деньги на карту. amount=0 — вывести всё.
func (e *Engine) Withdraw(ctx context.Context, userID, planID, amount int64, trigger, idemKey string) (*store.Order, error) {
	unlock, ok := e.lock(planID)
	if !ok {
		return nil, ErrBusy
	}
	defer unlock()
	return e.withdrawLocked(ctx, userID, planID, amount, trigger, idemKey)
}

func (e *Engine) withdrawLocked(ctx context.Context, userID, planID, amount int64, trigger, idemKey string) (*store.Order, error) {
	if prev := e.orderByIdem(ctx, idemKey); prev != nil {
		return prev, nil
	}
	p, err := e.St.Plan(ctx, userID, planID)
	if err != nil {
		return nil, err
	}
	var card *store.Card
	if p.CardID != nil {
		card, _ = e.St.Card(ctx, userID, *p.CardID)
	}
	if card == nil {
		card, _ = e.St.DefaultCard(ctx, userID)
	}
	if card == nil {
		return nil, errors.New("Привяжите карту, на которую выплатить деньги")
	}
	type sale struct {
		asset           string
		qty, cost, amt  int64
		price, priceUSD float64
	}
	var total int64
	var sales []sale
	for _, x := range p.Positions {
		if x.Qty <= 0 {
			continue
		}
		q, err := e.Hub.TradeQuote(x.Asset)
		if err != nil {
			return nil, ErrMarket
		}
		price, err := e.Broker.Price(q, "sell")
		if err != nil {
			return nil, err
		}
		v := money.ValueKop(x.Qty, price)
		total += v
		sales = append(sales, sale{asset: x.Asset, qty: x.Qty, cost: x.CostKop, amt: v, price: price, priceUSD: q.PriceUSD})
	}
	if total <= 0 {
		return nil, ErrNotEnough
	}
	if amount > total {
		return nil, ErrNotEnough
	}
	// «почти всё» (остаток меньше 0,5%) выводим целиком, чтобы не оставлять пыль
	all := amount <= 0 || amount >= total*995/1000
	if !all && amount < 100 {
		return nil, errors.New("Минимальная сумма вывода — 1,00 руб.")
	}
	frac := 1.0
	payout := total
	if !all {
		frac = float64(amount) / float64(total)
		payout = amount
	}
	now := store.Now()
	var orderID, paymentID int64
	// Продажа и долг перед клиентом фиксируются одной транзакцией: заявка сразу «ожидает выплаты»,
	// поэтому сбой после продажи не потеряет деньги — оператор увидит её в админке.
	err = withTx(ctx, e.DB, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO orders(user_id, plan_id, side, trigger, status, amount_kop, fee_kop, idem_key, usd_byn, broker, error, created_at)
			VALUES(?,?,?,?,?,?,0,?,?,?,?,?)`, userID, planID, "sell", trigger, "payout_pending", payout, idemKey, e.Hub.Rates().USD, e.Broker.Name(), "Выплата в обработке", now)
		if err != nil {
			return err
		}
		orderID, _ = res.LastInsertId()
		for _, s := range sales {
			qty, cost, amt := s.qty, s.cost, s.amt
			if !all {
				qty = int64(math.Floor(float64(s.qty) * frac))
				cost = int64(math.Round(float64(s.cost) * frac))
				amt = money.ValueKop(qty, s.price)
			}
			if qty <= 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO fills(order_id, asset, qty, price_byn, price_usd, amount_kop) VALUES(?,?,?,?,?,?)`, orderID, s.asset, qty, s.price, s.priceUSD, amt); err != nil {
				return err
			}
			res, err := tx.ExecContext(ctx, `UPDATE positions SET qty=qty-?, cost_kop=MAX(cost_kop-?,0) WHERE plan_id=? AND asset=? AND qty>=?`, qty, cost, planID, s.asset, qty)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return errors.New("позиция изменилась, повторите операцию")
			}
		}
		res, err = tx.ExecContext(ctx, `INSERT INTO payments(user_id, card_id, provider, kind, tracking_id, amount_kop, status, test, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, userID, card.ID, e.Pay.Name(), "payout", fmt.Sprintf("po_%d", orderID), payout, "pending", e.Pay.Test(), now, now)
		if err != nil {
			return err
		}
		paymentID, _ = res.LastInsertId()
		_, err = tx.ExecContext(ctx, `UPDATE orders SET payment_id=? WHERE id=?`, paymentID, orderID)
		return err
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			if prev := e.orderByIdem(ctx, idemKey); prev != nil {
				return prev, nil
			}
		}
		return nil, err
	}
	pr, perr := e.Pay.Payout(ctx, payments.ChargeRequest{Token: card.Token, AmountKop: payout, Description: "Вывод из копилки «" + p.Name + "»", TrackingID: fmt.Sprintf("po_%d", orderID)})
	msg := "Деньги отправлены на карту •• " + card.Last4
	if perr == nil && pr.Status == "successful" {
		e.DB.ExecContext(ctx, `UPDATE payments SET status='successful', provider_uid=?, message=?, updated_at=? WHERE id=?`, pr.UID, msg, store.Now(), paymentID)
		e.DB.ExecContext(ctx, `UPDATE orders SET status='filled', filled_at=?, error='' WHERE id=? AND status='payout_pending'`, store.Now(), orderID)
	} else {
		msg = "Активы проданы, выплата на карту •• " + card.Last4 + " будет проведена в течение 1–3 рабочих дней"
		if perr == nil && pr.Status == "failed" {
			e.Log.Warn("payout declined, manual", "order", orderID, "msg", pr.Message)
		}
		e.DB.ExecContext(ctx, `UPDATE orders SET error=? WHERE id=?`, msg, orderID)
	}
	e.N.Send(ctx, userID, notify.Message{Kind: "payout", Title: "Вывод " + money.FormatBYN(payout) + " из копилки «" + p.Name + "»", Body: msg, Link: fmt.Sprintf("/app/plans/%d", planID), Email: true})
	e.N.Publish(userID, notify.UserEvent{Type: "plans"})
	e.SnapshotUser(ctx, userID)
	return e.St.Order(ctx, userID, orderID)
}

// ClosePlan закрывает копилку: под блокировкой копилки прекращает пополнения и выводит всё на карту.
func (e *Engine) ClosePlan(ctx context.Context, userID, planID int64) (*store.Order, error) {
	unlock, ok := e.lock(planID)
	if !ok {
		return nil, ErrBusy
	}
	defer unlock()
	p, err := e.St.Plan(ctx, userID, planID)
	if err != nil {
		return nil, err
	}
	if p.Status == "closed" {
		return nil, nil
	}
	var pending int
	e.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE plan_id=? AND side='buy' AND status IN ('pending','manual')`, planID).Scan(&pending)
	if pending > 0 {
		return nil, errors.New("Дождитесь завершения пополнения в обработке")
	}
	prev := p.Status
	now := store.Now()
	if _, err := e.DB.ExecContext(ctx, `UPDATE plans SET status='closed', next_run_at=NULL, closed_at=?, updated_at=? WHERE id=?`, now, now, planID); err != nil {
		return nil, err
	}
	var order *store.Order
	if v := e.Value(p.Positions); v.ValueKop > 0 {
		order, err = e.withdrawLocked(ctx, userID, planID, 0, "close", fmt.Sprintf("close_%d_%d", planID, now))
		if err != nil {
			// вывести не получилось — копилка остаётся открытой на паузе
			e.DB.ExecContext(ctx, `UPDATE plans SET status=?, closed_at=NULL, updated_at=? WHERE id=?`, map[bool]string{true: "paused", false: prev}[prev == "active"], store.Now(), planID)
			return nil, err
		}
	}
	return order, nil
}

// MarkPayoutDone — оператор провёл выплату вручную.
func (e *Engine) MarkPayoutDone(ctx context.Context, orderID int64, actor int64) error {
	o, err := e.St.Order(ctx, 0, orderID)
	if err != nil {
		return err
	}
	if o.Status != "payout_pending" {
		return errors.New("заявка не ожидает выплаты")
	}
	now := store.Now()
	withTx(ctx, e.DB, func(tx *sql.Tx) error {
		tx.ExecContext(ctx, `UPDATE orders SET status='filled', error='', filled_at=? WHERE id=?`, now, orderID)
		tx.ExecContext(ctx, `UPDATE payments SET status='successful', message='Выплачено оператором', updated_at=? WHERE id=(SELECT payment_id FROM orders WHERE id=?)`, now, orderID)
		return nil
	})
	e.St.Audit(ctx, o.UserID, actor, "payout.manual", fmt.Sprintf("order %d, %s", orderID, money.FormatBYN(o.AmountKop)), "")
	e.N.Send(ctx, o.UserID, notify.Message{Kind: "payout", Title: "Выплата " + money.FormatBYN(o.AmountKop) + " проведена", Body: "Деньги отправлены на вашу карту.", Link: "/app/history", Email: true})
	return nil
}

// ---------------------------------------------------------------- оценка

type PositionView struct {
	Asset     string  `json:"asset"`
	Qty       int64   `json:"qty"`
	CostKop   int64   `json:"cost_kop"`
	ValueKop  int64   `json:"value_kop"`
	PriceBYN  float64 `json:"price_byn"`
	Change24h float64 `json:"change_24h"`
	ShareBP   int64   `json:"share_bp"`
	Stale     bool    `json:"stale"`
}

type Valuation struct {
	ValueKop    int64          `json:"value_kop"`
	InvestedKop int64          `json:"invested_kop"`
	PnLKop      int64          `json:"pnl_kop"`
	PnLPct      float64        `json:"pnl_pct"`
	DayPnLKop   int64          `json:"day_pnl_kop"`
	Positions   []PositionView `json:"positions"`
	Stale       bool           `json:"stale"`
}

// Value оценивает набор позиций по рыночной цене (без спреда).
func (e *Engine) Value(pos []store.Position) Valuation {
	v := Valuation{Positions: []PositionView{}}
	for _, x := range pos {
		if x.Qty <= 0 && x.CostKop <= 0 {
			continue
		}
		pv := PositionView{Asset: x.Asset, Qty: x.Qty, CostKop: x.CostKop}
		if q, err := e.Hub.Quote(x.Asset); err == nil {
			pv.PriceBYN, pv.Change24h, pv.Stale = q.PriceBYN, q.Change24h, q.Stale
			pv.ValueKop = money.ValueKop(x.Qty, q.PriceBYN)
			if q.Change24h > -100 {
				prev := float64(pv.ValueKop) / (1 + q.Change24h/100)
				v.DayPnLKop += pv.ValueKop - int64(math.Round(prev))
			}
		} else {
			pv.ValueKop, pv.Stale = x.CostKop, true
		}
		v.Stale = v.Stale || pv.Stale
		v.ValueKop += pv.ValueKop
		v.InvestedKop += x.CostKop
		v.Positions = append(v.Positions, pv)
	}
	for i := range v.Positions {
		if v.ValueKop > 0 {
			v.Positions[i].ShareBP = v.Positions[i].ValueKop * 10000 / v.ValueKop
		}
	}
	v.PnLKop = v.ValueKop - v.InvestedKop
	if v.InvestedKop > 0 {
		v.PnLPct = float64(v.PnLKop) / float64(v.InvestedKop) * 100
	}
	return v
}

// SnapshotUser записывает почасовой снимок стоимости портфеля и каждой копилки.
func (e *Engine) SnapshotUser(ctx context.Context, userID int64) {
	plans, err := e.St.Plans(ctx, userID, true)
	if err != nil {
		return
	}
	ts := time.Now().Truncate(time.Hour).Unix()
	var all []store.Position
	for _, p := range plans {
		v := e.Value(p.Positions)
		if v.Stale && v.ValueKop == v.InvestedKop && len(p.Positions) > 0 {
			continue // без котировок снимок исказил бы график
		}
		if len(p.Positions) > 0 || p.Status != "closed" {
			e.St.PutSnapshot(ctx, userID, p.ID, ts, v.ValueKop, v.InvestedKop)
		}
		all = append(all, p.Positions...)
	}
	v := e.Value(all)
	e.St.PutSnapshot(ctx, userID, 0, ts, v.ValueKop, v.InvestedKop)
}

// ---------------------------------------------------------------- рефералка

// rewardReferral: после первой покупки приглашённого оба получают месяц Pro.
func (e *Engine) rewardReferral(ctx context.Context, userID int64) {
	u, err := e.St.UserByID(ctx, userID)
	if err != nil || u.ReferredBy == nil || u.ReferralRewarded {
		return
	}
	res, err := e.DB.ExecContext(ctx, `UPDATE users SET referral_rewarded=1 WHERE id=? AND referral_rewarded=0`, userID)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	for _, id := range []int64{userID, *u.ReferredBy} {
		e.ExtendPro(ctx, id, 30*24*time.Hour)
	}
	e.N.Send(ctx, *u.ReferredBy, notify.Message{Kind: "referral", Title: "Ваш друг начал копить — вам месяц Pro", Body: u.Name + " сделал первое пополнение по вашему приглашению.", Link: "/app/profile/referral"})
	e.N.Send(ctx, userID, notify.Message{Kind: "referral", Title: "Месяц Pro в подарок", Body: "Спасибо, что пришли по приглашению. Подписка Pro включена на 30 дней.", Link: "/app/profile/billing"})
}

// ExtendPro атомарно продлевает Pro на d от max(сейчас, текущей даты окончания).
func (e *Engine) ExtendPro(ctx context.Context, userID int64, d time.Duration) error {
	now := store.Now()
	_, err := e.DB.ExecContext(ctx, `UPDATE users SET tier='pro', pro_until=MAX(COALESCE(pro_until,0), ?)+?, updated_at=? WHERE id=?`, now, int64(d/time.Second), now, userID)
	return err
}

func proDuration(period string) time.Duration {
	if period == "year" {
		return 365 * 24 * time.Hour
	}
	return 30 * 24 * time.Hour
}

var ErrPaymentPending = errors.New("Оплата обрабатывается банком — статус придёт уведомлением")

// ChargePro — оплата подписки с карты. Одновременно может идти только одна оплата на пользователя.
func (e *Engine) ChargePro(ctx context.Context, userID int64, period string, renewal bool) error {
	unlock, ok := e.lock(-userID) // отрицательный ключ — блокировка пользователя, не копилки
	if !ok {
		return ErrPaymentPending
	}
	defer unlock()
	var pendingN int
	e.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE user_id=? AND kind='subscription' AND status='pending'`, userID).Scan(&pendingN)
	if pendingN > 0 {
		return ErrPaymentPending
	}
	if !renewal {
		// защита от повторной покупки: в течение 10 минут после успешной оплаты новая не списывается
		var recent int
		e.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE user_id=? AND kind='subscription' AND status='successful' AND created_at>?`, userID, store.Now()-600).Scan(&recent)
		if recent > 0 {
			return errors.New("Подписка только что оплачена")
		}
	}
	u, err := e.St.UserByID(ctx, userID)
	if err != nil {
		return err
	}
	card, err := e.St.DefaultCard(ctx, userID)
	if err != nil {
		return ErrNoCard
	}
	if period != "year" {
		period = "month"
	}
	price := e.Cfg.ProMonthKop
	if period == "year" {
		price = e.Cfg.ProYearKop
	}
	tracking := fmt.Sprintf("pro_%d_%s_%d", userID, period, time.Now().UnixNano())
	now := store.Now()
	res, err := e.DB.ExecContext(ctx, `INSERT INTO payments(user_id, card_id, provider, kind, tracking_id, amount_kop, status, test, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		userID, card.ID, e.Pay.Name(), "subscription", tracking, price, "pending", e.Pay.Test(), now, now)
	if err != nil {
		return err
	}
	pid, _ := res.LastInsertId()
	cr, err := e.Pay.Charge(ctx, payments.ChargeRequest{Token: card.Token, AmountKop: price, Description: "Подписка AutoInvest Pro", TrackingID: tracking, Email: u.Email})
	if err != nil {
		if cr, err = e.Pay.ChargeStatus(ctx, tracking); err != nil {
			return ErrPaymentPending // сверка доведёт платёж
		}
	}
	return e.SettlePro(ctx, pid, cr, renewal)
}

// SettlePro применяет результат оплаты подписки (из ответа, сверки или вебхука). Идемпотентно.
func (e *Engine) SettlePro(ctx context.Context, paymentID int64, cr payments.ChargeResult, renewal bool) error {
	var uid int64
	var tracking string
	var price int64
	var last4 string
	if err := e.DB.QueryRowContext(ctx, `SELECT p.user_id, p.tracking_id, p.amount_kop, COALESCE(c.last4,'') FROM payments p LEFT JOIN cards c ON c.id=p.card_id WHERE p.id=?`, paymentID).Scan(&uid, &tracking, &price, &last4); err != nil {
		return err
	}
	period := "month"
	if strings.Contains(tracking, "_year_") {
		period = "year"
	}
	switch cr.Status {
	case "pending":
		e.DB.ExecContext(ctx, `UPDATE payments SET provider_uid=?, updated_at=? WHERE id=?`, cr.UID, store.Now(), paymentID)
		return ErrPaymentPending
	case "failed":
		msg := cr.Message
		if msg == "" {
			msg = "Банк отклонил оплату"
		}
		res, _ := e.DB.ExecContext(ctx, `UPDATE payments SET status='failed', provider_uid=?, message=?, updated_at=? WHERE id=? AND status='pending'`, cr.UID, msg, store.Now(), paymentID)
		if n, _ := res.RowsAffected(); n > 0 && renewal {
			e.N.Send(ctx, uid, notify.Message{Kind: "billing", Title: "Не удалось продлить Pro", Body: msg + ". Проверьте карту •• " + last4 + ".", Link: "/app/profile/billing", Email: true})
		}
		return errors.New(msg)
	}
	// successful: продлеваем ровно один раз — только если платёж ещё не был учтён
	res, err := e.DB.ExecContext(ctx, `UPDATE payments SET status='successful', provider_uid=?, message=?, updated_at=? WHERE id=? AND status!='successful'`, cr.UID, cr.Message, store.Now(), paymentID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	if err := e.ExtendPro(ctx, uid, proDuration(period)); err != nil {
		return err
	}
	e.DB.ExecContext(ctx, `UPDATE users SET pro_period=?, pro_autorenew=CASE WHEN ? THEN pro_autorenew ELSE 1 END WHERE id=?`, period, renewal, uid)
	title := "Подписка Pro оплачена"
	if renewal {
		title = "Подписка Pro продлена"
	}
	e.N.Send(ctx, uid, notify.Message{Kind: "billing", Title: title, Body: "Списано " + money.FormatBYN(price) + " с карты •• " + last4 + ".", Link: "/app/profile/billing", Email: true})
	return nil
}

// SettleExternal — доведение заявки по уведомлению платёжного шлюза.
func (e *Engine) SettleExternal(ctx context.Context, orderID, paymentID int64, cr payments.ChargeResult) {
	if cr.Status != "successful" && cr.Status != "failed" {
		return
	}
	if _, err := e.settleBuy(ctx, orderID, paymentID, cr); err != nil {
		e.Log.Error("settle external", "order", orderID, "err", err)
	}
}
