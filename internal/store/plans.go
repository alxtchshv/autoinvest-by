package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ---------------------------------------------------------------- cards

type Card struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Provider  string `json:"provider"`
	Token     string `json:"-"`
	Brand     string `json:"brand"`
	Last4     string `json:"last4"`
	ExpMonth  int    `json:"exp_month"`
	ExpYear   int    `json:"exp_year"`
	Holder    string `json:"holder"`
	Issuer    string `json:"issuer"`
	BIN       string `json:"bin"`  // первые 6 цифр номера: PCI DSS разрешает хранить их вместе с последними 4
	Mask      string `json:"mask"` // «4200 00•• •••• 0000»
	IsDefault bool   `json:"is_default"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

const cardCols = `id, user_id, provider, token, brand, last4, exp_month, exp_year, holder, issuer, bin, is_default, status, created_at`

func scanCard(r scanner) (*Card, error) {
	var c Card
	err := r.Scan(&c.ID, &c.UserID, &c.Provider, &c.Token, &c.Brand, &c.Last4, &c.ExpMonth, &c.ExpYear, &c.Holder, &c.Issuer, &c.BIN, &c.IsDefault, &c.Status, &c.CreatedAt)
	c.Mask = CardMask(c.BIN, c.Last4)
	c.Token = unseal(c.Token) // токен платёжки хранится зашифрованным
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (s *Store) Cards(ctx context.Context, userID int64) ([]Card, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cardCols+` FROM cards WHERE user_id=? AND status='active' ORDER BY is_default DESC, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) Card(ctx context.Context, userID, id int64) (*Card, error) {
	return scanCard(s.DB.QueryRowContext(ctx, `SELECT `+cardCols+` FROM cards WHERE id=? AND user_id=? AND status='active'`, id, userID))
}

func (s *Store) DefaultCard(ctx context.Context, userID int64) (*Card, error) {
	return scanCard(s.DB.QueryRowContext(ctx, `SELECT `+cardCols+` FROM cards WHERE user_id=? AND status='active' ORDER BY is_default DESC, id LIMIT 1`, userID))
}

func (s *Store) AddCard(ctx context.Context, c Card) (*Card, error) {
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM cards WHERE user_id=? AND status='active'`, c.UserID).Scan(&n)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO cards(user_id, provider, token, brand, last4, exp_month, exp_year, holder, issuer, bin, is_default, created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.UserID, c.Provider, c.Token, c.Brand, onlyDigits(c.Last4, 4), c.ExpMonth, c.ExpYear, c.Holder, c.Issuer, onlyDigits(c.BIN, 6), n == 0, Now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.Card(ctx, c.UserID, id)
}

func (s *Store) SetDefaultCard(ctx context.Context, userID, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE cards SET is_default=1 WHERE id=? AND user_id=? AND status='active'`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET is_default=0 WHERE user_id=? AND id!=?`, userID, id); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveCard удаляет карту; активные копилки на ней ставятся на паузу (возвращает их число).
func (s *Store) RemoveCard(ctx context.Context, userID, id int64) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE cards SET status='removed', is_default=0, token='' WHERE id=? AND user_id=? AND status='active'`, id, userID)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}
	// переносим копилки на основную карту, если она есть, иначе — пауза
	var other sql.NullInt64
	tx.QueryRowContext(ctx, `SELECT id FROM cards WHERE user_id=? AND status='active' ORDER BY is_default DESC, id LIMIT 1`, userID).Scan(&other)
	var paused int64
	if other.Valid {
		tx.ExecContext(ctx, `UPDATE cards SET is_default=1 WHERE id=?`, other.Int64)
		tx.ExecContext(ctx, `UPDATE plans SET card_id=?, updated_at=? WHERE card_id=? AND status!='closed'`, other.Int64, Now(), id)
	} else {
		r, _ := tx.ExecContext(ctx, `UPDATE plans SET status='paused', updated_at=? WHERE card_id=? AND status='active'`, Now(), id)
		paused, _ = r.RowsAffected()
		tx.ExecContext(ctx, `UPDATE plans SET card_id=NULL WHERE card_id=?`, id)
	}
	return paused, tx.Commit()
}

type Binding struct {
	Ref         string
	UserID      int64
	Provider    string
	ProviderRef string
	Status      string
	CardID      *int64
	Error       string
	CreatedAt   int64
}

func (s *Store) CreateBinding(ctx context.Context, b Binding) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO card_bindings(ref, user_id, provider, provider_ref, created_at) VALUES(?,?,?,?,?)`,
		b.Ref, b.UserID, b.Provider, b.ProviderRef, Now())
	return err
}

func (s *Store) SetBindingProviderRef(ctx context.Context, ref, pref string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE card_bindings SET provider_ref=? WHERE ref=?`, pref, ref)
	return err
}

func (s *Store) GetBinding(ctx context.Context, ref string) (*Binding, error) {
	var b Binding
	var cid sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT ref, user_id, provider, provider_ref, status, card_id, error, created_at FROM card_bindings WHERE ref=?`, ref).
		Scan(&b.Ref, &b.UserID, &b.Provider, &b.ProviderRef, &b.Status, &cid, &b.Error, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	b.CardID = nullInt(cid)
	return &b, err
}

// FinishBinding атомарно завершает привязку (повторный вызов из вебхука и return_url безопасен).
func (s *Store) FinishBinding(ctx context.Context, ref string, card *Card, errMsg string) (*Card, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status string
	var uid int64
	if err := tx.QueryRowContext(ctx, `SELECT status, user_id FROM card_bindings WHERE ref=?`, ref).Scan(&status, &uid); err != nil {
		return nil, ErrNotFound
	}
	if status != "pending" {
		return nil, tx.Commit()
	}
	if card == nil {
		tx.ExecContext(ctx, `UPDATE card_bindings SET status='failed', error=? WHERE ref=?`, errMsg, ref)
		return nil, tx.Commit()
	}
	// одна и та же карта (тот же токен) не дублируется
	// токены в базе зашифрованы (каждый раз по-разному) — сравниваем расшифрованные
	var existing sql.NullInt64
	if rows, err := tx.QueryContext(ctx, `SELECT id, token FROM cards WHERE user_id=? AND status='active'`, uid); err == nil {
		for rows.Next() {
			var cid int64
			var tok string
			if rows.Scan(&cid, &tok) == nil && unseal(tok) == card.Token {
				existing = sql.NullInt64{Int64: cid, Valid: true}
			}
		}
		rows.Close()
	}
	var id int64
	if existing.Valid {
		id = existing.Int64
	} else {
		var n int
		tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cards WHERE user_id=? AND status='active'`, uid).Scan(&n)
		res, err := tx.ExecContext(ctx, `INSERT INTO cards(user_id, provider, token, brand, last4, exp_month, exp_year, holder, issuer, bin, is_default, created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			uid, card.Provider, seal(card.Token), card.Brand, onlyDigits(card.Last4, 4), card.ExpMonth, card.ExpYear, card.Holder, card.Issuer, onlyDigits(card.BIN, 6), n == 0, Now())
		if err != nil {
			return nil, err
		}
		id, _ = res.LastInsertId()
	}
	tx.ExecContext(ctx, `UPDATE card_bindings SET status='done', card_id=? WHERE ref=?`, id, ref)
	// копилки без карты получают новую карту (включает их пользователь сам)
	tx.ExecContext(ctx, `UPDATE plans SET card_id=?, updated_at=? WHERE user_id=? AND card_id IS NULL AND status!='closed'`, id, Now(), uid)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Card(ctx, uid, id)
}

// ---------------------------------------------------------------- plans

type Alloc struct {
	Asset    string `json:"asset"`
	WeightBP int64  `json:"weight_bp"`
}

type Position struct {
	Asset   string `json:"asset"`
	Qty     int64  `json:"qty"`
	CostKop int64  `json:"cost_kop"`
}

type Plan struct {
	ID               int64      `json:"id"`
	UserID           int64      `json:"-"`
	Name             string     `json:"name"`
	Icon             string     `json:"icon"`
	Strategy         string     `json:"strategy"`
	GoalKop          *int64     `json:"goal_kop"`
	GoalDate         *string    `json:"goal_date"`
	AmountKop        int64      `json:"amount_kop"`
	Frequency        string     `json:"frequency"`
	Weekday          int        `json:"weekday"`
	Monthday         int        `json:"monthday"`
	RunMinute        int        `json:"run_minute"`
	CardID           *int64     `json:"card_id"`
	Status           string     `json:"status"`
	DipEnabled       bool       `json:"dip_enabled"`
	DipThresholdBP   int64      `json:"dip_threshold_bp"`
	DipMultiplierPct int64      `json:"dip_multiplier_pct"`
	LastDipAt        *int64     `json:"last_dip_at"`
	NextRunAt        *int64     `json:"next_run_at"`
	FailCount        int        `json:"fail_count"`
	CreatedAt        int64      `json:"created_at"`
	UpdatedAt        int64      `json:"updated_at"`
	ClosedAt         *int64     `json:"closed_at"`
	Allocations      []Alloc    `json:"allocations"`
	Positions        []Position `json:"positions"`
}

const planCols = `id, user_id, name, icon, strategy, goal_kop, goal_date, amount_kop, frequency, weekday, monthday, run_minute, card_id,
status, dip_enabled, dip_threshold_bp, dip_multiplier_pct, last_dip_at, next_run_at, fail_count, created_at, updated_at, closed_at`

func scanPlan(r scanner) (*Plan, error) {
	var p Plan
	var goal, card, lastDip, next, closed sql.NullInt64
	var gdate sql.NullString
	err := r.Scan(&p.ID, &p.UserID, &p.Name, &p.Icon, &p.Strategy, &goal, &gdate, &p.AmountKop, &p.Frequency, &p.Weekday, &p.Monthday,
		&p.RunMinute, &card, &p.Status, &p.DipEnabled, &p.DipThresholdBP, &p.DipMultiplierPct, &lastDip, &next, &p.FailCount,
		&p.CreatedAt, &p.UpdatedAt, &closed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.GoalKop, p.GoalDate, p.CardID, p.LastDipAt, p.NextRunAt, p.ClosedAt = nullInt(goal), nullStr(gdate), nullInt(card), nullInt(lastDip), nullInt(next), nullInt(closed)
	return &p, nil
}

type querier interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

func (s *Store) loadPlanChildren(ctx context.Context, q querier, plans []*Plan) error {
	if len(plans) == 0 {
		return nil
	}
	idx := map[int64]*Plan{}
	for _, p := range plans {
		idx[p.ID] = p
		p.Allocations = []Alloc{}
		p.Positions = []Position{}
	}
	rows, err := q.QueryContext(ctx, `SELECT plan_id, asset, weight_bp FROM plan_allocations WHERE plan_id IN (SELECT id FROM plans WHERE user_id=?) ORDER BY weight_bp DESC`, plans[0].UserID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var a Alloc
		rows.Scan(&id, &a.Asset, &a.WeightBP)
		if p := idx[id]; p != nil {
			p.Allocations = append(p.Allocations, a)
		}
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT plan_id, asset, qty, cost_kop FROM positions WHERE plan_id IN (SELECT id FROM plans WHERE user_id=?) AND (qty>0 OR cost_kop>0)`, plans[0].UserID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var x Position
		rows.Scan(&id, &x.Asset, &x.Qty, &x.CostKop)
		if p := idx[id]; p != nil {
			p.Positions = append(p.Positions, x)
		}
	}
	return rows.Err()
}

// Plans — копилки пользователя. includeClosed=false скрывает закрытые.
func (s *Store) Plans(ctx context.Context, userID int64, includeClosed bool) ([]*Plan, error) {
	q := `SELECT ` + planCols + ` FROM plans WHERE user_id=?`
	if !includeClosed {
		q += ` AND status!='closed'`
	}
	q += ` ORDER BY CASE status WHEN 'active' THEN 0 WHEN 'paused' THEN 1 ELSE 2 END, id`
	rows, err := s.DB.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	var out []*Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	return out, s.loadPlanChildren(ctx, s.DB, out)
}

func (s *Store) Plan(ctx context.Context, userID, id int64) (*Plan, error) {
	p, err := scanPlan(s.DB.QueryRowContext(ctx, `SELECT `+planCols+` FROM plans WHERE id=? AND user_id=?`, id, userID))
	if err != nil {
		return nil, err
	}
	return p, s.loadPlanChildren(ctx, s.DB, []*Plan{p})
}

// PlanByID — копилка без проверки владельца (для движка и админки).
func (s *Store) PlanByID(ctx context.Context, id int64) (*Plan, error) {
	p, err := scanPlan(s.DB.QueryRowContext(ctx, `SELECT `+planCols+` FROM plans WHERE id=?`, id))
	if err != nil {
		return nil, err
	}
	return p, s.loadPlanChildren(ctx, s.DB, []*Plan{p})
}

func (s *Store) CountOpenPlans(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM plans WHERE user_id=? AND status!='closed'`, userID).Scan(&n)
	return n, err
}

func (s *Store) CreatePlan(ctx context.Context, p *Plan) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := Now()
	res, err := tx.ExecContext(ctx, `INSERT INTO plans(user_id, name, icon, strategy, goal_kop, goal_date, amount_kop, frequency, weekday, monthday, run_minute,
		card_id, status, dip_enabled, dip_threshold_bp, dip_multiplier_pct, next_run_at, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.UserID, p.Name, p.Icon, p.Strategy, p.GoalKop, p.GoalDate, p.AmountKop, p.Frequency, p.Weekday, p.Monthday, p.RunMinute,
		p.CardID, p.Status, p.DipEnabled, p.DipThresholdBP, p.DipMultiplierPct, p.NextRunAt, now, now)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	for _, a := range p.Allocations {
		if _, err := tx.ExecContext(ctx, `INSERT INTO plan_allocations(plan_id, asset, weight_bp) VALUES(?,?,?)`, p.ID, a.Asset, a.WeightBP); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpdatePlan(ctx context.Context, p *Plan) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `UPDATE plans SET name=?, icon=?, strategy=?, goal_kop=?, goal_date=?, amount_kop=?, frequency=?, weekday=?, monthday=?,
		run_minute=?, card_id=?, status=?, dip_enabled=?, dip_threshold_bp=?, dip_multiplier_pct=?, next_run_at=?, fail_count=?, updated_at=?, closed_at=? WHERE id=? AND user_id=?`,
		p.Name, p.Icon, p.Strategy, p.GoalKop, p.GoalDate, p.AmountKop, p.Frequency, p.Weekday, p.Monthday, p.RunMinute, p.CardID, p.Status,
		p.DipEnabled, p.DipThresholdBP, p.DipMultiplierPct, p.NextRunAt, p.FailCount, Now(), p.ClosedAt, p.ID, p.UserID)
	if err != nil {
		return err
	}
	if p.Allocations != nil {
		tx.ExecContext(ctx, `DELETE FROM plan_allocations WHERE plan_id=?`, p.ID)
		for _, a := range p.Allocations {
			if _, err := tx.ExecContext(ctx, `INSERT INTO plan_allocations(plan_id, asset, weight_bp) VALUES(?,?,?)`, p.ID, a.Asset, a.WeightBP); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// DuePlans — активные копилки, у которых наступило время пополнения.
func (s *Store) DuePlans(ctx context.Context, now int64, limit int) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id FROM plans p JOIN users u ON u.id=p.user_id
		WHERE p.status='active' AND p.frequency!='manual' AND p.next_run_at IS NOT NULL AND p.next_run_at<=? AND u.status='active'
		ORDER BY p.next_run_at LIMIT ?`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DipPlans — активные копилки с включённой докупкой на просадке.
func (s *Store) DipPlans(ctx context.Context) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT p.id FROM plans p JOIN users u ON u.id=p.user_id WHERE p.status='active' AND p.dip_enabled=1 AND u.status='active'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// onlyDigits оставляет не больше max цифр: даже если провайдер пришлёт лишнее, полный номер карты в базу не попадёт.
func onlyDigits(v string, max int) string {
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' && b.Len() < max {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CardMask строит маску вида «4200 00•• •••• 1234»; без первых цифр — «•••• 1234».
func CardMask(bin, last4 string) string {
	if len(bin) < 4 {
		return "•••• " + last4
	}
	if len(bin) >= 6 {
		return bin[:4] + " " + bin[4:6] + "•• •••• " + last4
	}
	return bin[:4] + " •••• •••• " + last4
}
