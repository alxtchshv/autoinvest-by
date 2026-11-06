package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"autoinvest/internal/money"
	"autoinvest/internal/notify"
	"autoinvest/internal/payments"
	"autoinvest/internal/store"
)

// Run — фоновый планировщик: пополнения по расписанию, сверка платежей, докупка на просадках,
// почасовые снимки стоимости, продление подписок.
func (e *Engine) Run(ctx context.Context) {
	tick := time.NewTicker(e.Cfg.SchedulerEvery)
	defer tick.Stop()
	lastHour, lastDip, lastDaily := int64(0), time.Time{}, ""
	for {
		e.runDue(ctx)
		e.reconcile(ctx)
		if time.Since(lastDip) > 10*time.Minute {
			e.runDips(ctx)
			lastDip = time.Now()
		}
		if h := time.Now().Truncate(time.Hour).Unix(); h != lastHour {
			e.snapshotAll(ctx)
			lastHour = h
		}
		if d := time.Now().In(Minsk).Format("2006-01-02T15"); d[:10] != lastDaily && d[11:] >= "09" {
			e.renewPro(ctx)
			lastDaily = d[:10]
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (e *Engine) runDue(ctx context.Context) {
	ids, err := e.St.DuePlans(ctx, store.Now(), 200)
	if err != nil {
		e.Log.Error("due plans", "err", err)
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		e.runPlan(ctx, id)
	}
}

// ErrNotScheduled — у копилки нет расписания (пауза, закрыта или пополняется вручную).
var ErrNotScheduled = errors.New("Копилка на паузе или пополняется вручную — включите её и выберите расписание")

// RunScheduleNow выполняет ближайшее плановое пополнение сразу, тем же путём, что и планировщик:
// триггер «по расписанию», учёт отказов, сдвиг следующего запуска. Только для тестового стенда —
// чтобы проверить расписание, не дожидаясь пятницы.
func (e *Engine) RunScheduleNow(ctx context.Context, planID int64) error {
	res, err := e.DB.ExecContext(ctx, `UPDATE plans SET next_run_at=? WHERE id=? AND status='active' AND frequency!='manual'`, store.Now(), planID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotScheduled
	}
	e.runPlan(ctx, planID)
	return nil
}

func (e *Engine) runPlan(ctx context.Context, id int64) {
	p, err := e.St.PlanByID(ctx, id)
	if err != nil || p.NextRunAt == nil {
		return
	}
	scheduled := *p.NextRunAt
	key := fmt.Sprintf("sch_%d_%d", p.ID, scheduled)
	res, err := e.Buy(ctx, p.ID, p.AmountKop, "schedule", key)
	now := time.Now()
	// Следующий запуск всегда в будущем: пропущенные периоды не «догоняются» пачкой списаний.
	next := NextRun(p, time.Unix(scheduled, 0), true)
	for next != nil && *next <= now.Unix() {
		next = NextRun(p, time.Unix(*next, 0), true)
	}
	switch {
	case errors.Is(err, ErrBusy):
		return
	case errors.Is(err, ErrMarket):
		retry := now.Add(10 * time.Minute).Unix()
		e.setNext(ctx, p.ID, &retry)
		return
	case errors.Is(err, ErrNoCard):
		e.pausePlan(ctx, p.ID, p.UserID, "Копилка «"+p.Name+"» на паузе", "К копилке не привязана карта. Привяжите карту и включите копилку снова.")
		return
	case errors.Is(err, ErrKYCRequired):
		e.pausePlan(ctx, p.ID, p.UserID, "Копилка «"+p.Name+"» на паузе", "Для покупок нужна верификация личности. Пройдите её в профиле и включите копилку.")
		return
	case errors.Is(err, ErrProRequired):
		e.pausePlan(ctx, p.ID, p.UserID, "Копилка «"+p.Name+"» на паузе", "В составе копилки есть акции или токены компаний — они доступны на тарифе Pro. Продлите Pro или измените состав и включите копилку.")
		return
	case errors.Is(err, ErrAmountTooSmall), errors.Is(err, ErrAmountTooLarge):
		e.pausePlan(ctx, p.ID, p.UserID, "Копилка «"+p.Name+"» на паузе", err.Error()+". Измените сумму пополнения и включите копилку.")
		return
	case err != nil:
		e.Log.Warn("plan run failed", "plan", p.ID, "err", err)
		e.setNext(ctx, p.ID, next)
		return
	}
	// отказ банка: учёт и пауза — в settleBuy; здесь только время следующей попытки
	if res.Decline {
		cur, err := e.St.PlanByID(ctx, p.ID)
		if err != nil || cur.Status != "active" {
			return
		}
		if cur.FailCount == 1 {
			retry := now.Add(6 * time.Hour).Unix()
			if next == nil || retry < *next {
				next = &retry
			}
		}
	}
	e.setNext(ctx, p.ID, next)
}

func (e *Engine) setNext(ctx context.Context, planID int64, next *int64) {
	e.DB.ExecContext(ctx, `UPDATE plans SET next_run_at=?, updated_at=? WHERE id=? AND status='active'`, next, store.Now(), planID)
}

// reconcile доводит заявки, по которым банк не дал окончательного ответа, и неподтверждённые оплаты Pro.
// Отказ ставится только если банк его подтвердил; без ответа больше суток заявка уходит на ручную сверку.
func (e *Engine) reconcile(ctx context.Context) {
	now := store.Now()
	rows, err := e.DB.QueryContext(ctx, `SELECT o.id, o.payment_id, p.tracking_id, p.status, o.created_at, p.updated_at FROM orders o JOIN payments p ON p.id=o.payment_id
		WHERE o.status='pending' AND o.side='buy' AND o.created_at<? ORDER BY p.updated_at LIMIT 50`, now-120)
	if err != nil {
		return
	}
	type item struct {
		oid, pid          int64
		tracking, pstatus string
		created, polled   int64
	}
	var items []item
	for rows.Next() {
		var it item
		rows.Scan(&it.oid, &it.pid, &it.tracking, &it.pstatus, &it.created, &it.polled)
		items = append(items, it)
	}
	rows.Close()
	for _, it := range items {
		age := now - it.created
		// бэк-офф: чем старше заявка, тем реже опрос (от 2 минут до часа)
		if gap := min(max(age/10, 120), 3600); now-it.polled < gap && it.pstatus != "successful" {
			continue
		}
		e.DB.ExecContext(ctx, `UPDATE payments SET updated_at=? WHERE id=?`, now, it.pid)
		if it.pstatus == "successful" {
			// деньги получены, ждали свежих котировок
			e.settleBuy(ctx, it.oid, it.pid, payments.ChargeResult{Status: "successful"})
			continue
		}
		cr, err := e.Pay.ChargeStatus(ctx, it.tracking)
		if err == nil && (cr.Status == "successful" || cr.Status == "failed") {
			e.settleBuy(ctx, it.oid, it.pid, cr)
			continue
		}
		if age > 86400 {
			e.markManual(ctx, it.oid, "pending", "Банк не дал окончательного ответа за сутки — требуется сверка с выпиской шлюза")
		}
	}
	// оплаты подписки
	prow, err := e.DB.QueryContext(ctx, `SELECT id, tracking_id, created_at FROM payments WHERE kind='subscription' AND status='pending' AND updated_at<? ORDER BY updated_at LIMIT 20`, now-120)
	if err != nil {
		return
	}
	type pi struct {
		id       int64
		tracking string
		created  int64
	}
	var pis []pi
	for prow.Next() {
		var x pi
		prow.Scan(&x.id, &x.tracking, &x.created)
		pis = append(pis, x)
	}
	prow.Close()
	for _, x := range pis {
		e.DB.ExecContext(ctx, `UPDATE payments SET updated_at=? WHERE id=?`, now, x.id)
		if cr, err := e.Pay.ChargeStatus(ctx, x.tracking); err == nil && (cr.Status == "successful" || cr.Status == "failed") {
			e.SettlePro(ctx, x.id, cr, false)
		} else if now-x.created > 86400 {
			e.DB.ExecContext(ctx, `UPDATE payments SET status='manual', message='Требуется сверка с выпиской шлюза' WHERE id=? AND status='pending'`, x.id)
			e.Log.Error("subscription payment unresolved", "payment", x.id)
		}
	}
}

// runDips — «докупка на просадке»: если актив копилки упал за сутки сильнее порога, делаем дополнительное пополнение.
func (e *Engine) runDips(ctx context.Context) {
	ids, err := e.St.DipPlans(ctx)
	if err != nil {
		return
	}
	day := time.Now().In(Minsk).Format("20060102")
	for _, id := range ids {
		p, err := e.St.PlanByID(ctx, id)
		if err != nil {
			continue
		}
		if p.LastDipAt != nil && store.Now()-*p.LastDipAt < 86400 {
			continue
		}
		u, err := e.St.UserByID(ctx, p.UserID)
		if err != nil || !u.IsPro() {
			continue
		}
		var worst float64
		var worstAsset string
		for _, a := range p.Allocations {
			if q, err := e.Hub.Quote(a.Asset); err == nil && !q.Stale && q.Change24h < worst {
				worst, worstAsset = q.Change24h, a.Asset
			}
		}
		if worst > -float64(p.DipThresholdBP)/100 {
			continue
		}
		amount := p.AmountKop * p.DipMultiplierPct / 100
		res, err := e.Buy(ctx, p.ID, amount, "dip", fmt.Sprintf("dip_%d_%s", p.ID, day))
		if err == nil && res.Order != nil && res.Order.Status == "filled" {
			e.DB.ExecContext(ctx, `UPDATE plans SET last_dip_at=? WHERE id=?`, store.Now(), p.ID)
			e.N.Send(ctx, p.UserID, notify.Message{Kind: "market", Title: fmt.Sprintf("%s упал на %.1f%% — копилка докупила", worstAsset, -worst),
				Body: "Дополнительное пополнение на " + money.FormatBYN(amount) + " по сниженной цене.", Link: fmt.Sprintf("/app/plans/%d", p.ID)})
		}
	}
}

func (e *Engine) snapshotAll(ctx context.Context) {
	ids, err := e.St.UsersWithPositions(ctx)
	if err != nil {
		return
	}
	for _, id := range ids {
		e.SnapshotUser(ctx, id)
	}
}

// renewPro — продление подписок и отключение истёкших.
func (e *Engine) renewPro(ctx context.Context) {
	now := store.Now()
	rows, err := e.DB.QueryContext(ctx, `SELECT id, pro_period, pro_autorenew FROM users WHERE tier='pro' AND pro_until IS NOT NULL AND pro_until<? AND status='active'`, now+86400)
	if err != nil {
		return
	}
	type item struct {
		id     int64
		period string
		auto   bool
	}
	var items []item
	for rows.Next() {
		var it item
		rows.Scan(&it.id, &it.period, &it.auto)
		items = append(items, it)
	}
	rows.Close()
	for _, it := range items {
		if it.auto {
			if err := e.ChargePro(ctx, it.id, it.period, true); err == nil {
				continue
			}
		}
		u, err := e.St.UserByID(ctx, it.id)
		if err == nil && u.ProUntil != nil && *u.ProUntil < now {
			e.St.UpdateUserFields(ctx, it.id, map[string]any{"tier": "free", "pro_autorenew": false})
			e.N.Send(ctx, it.id, notify.Message{Kind: "billing", Title: "Подписка Pro закончилась", Body: "Копилки продолжают работать на тарифе Старт.", Link: "/app/profile/billing"})
		}
	}
}
