package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type Fill struct {
	Asset     string  `json:"asset"`
	Qty       int64   `json:"qty"`
	PriceBYN  float64 `json:"price_byn"`
	PriceUSD  float64 `json:"price_usd"`
	AmountKop int64   `json:"amount_kop"`
}

type Order struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	PlanID     int64   `json:"plan_id"`
	PlanName   string  `json:"plan_name"`
	Side       string  `json:"side"`
	Trigger    string  `json:"trigger"`
	Status     string  `json:"status"`
	AmountKop  int64   `json:"amount_kop"`
	FeeKop     int64   `json:"fee_kop"`
	USDBYN     float64 `json:"usd_byn"`
	Error      string  `json:"error"`
	Broker     string  `json:"broker"`
	CreatedAt  int64   `json:"created_at"`
	FilledAt   *int64  `json:"filled_at"`
	CardLast4  string  `json:"card_last4"`
	CardBrand  string  `json:"card_brand"`
	PaymentUID string  `json:"payment_uid"`
	Provider   string  `json:"provider"`
	Test       bool    `json:"test"`
	UserEmail  string  `json:"user_email,omitempty"`
	Fills      []Fill  `json:"fills"`
}

type OrderFilter struct {
	UserID int64 // 0 = все (админка)
	PlanID int64
	Side   string
	Status string
	From   int64 // unix, включительно; 0 — без ограничения
	To     int64 // unix, не включая
	Limit  int
	Offset int
}

const orderSelect = `SELECT o.id, o.user_id, o.plan_id, p.name, o.side, o.trigger, o.status, o.amount_kop, o.fee_kop, o.usd_byn, o.error, o.broker,
	o.created_at, o.filled_at, COALESCE(c.last4,''), COALESCE(c.brand,''), COALESCE(pm.provider_uid,''), COALESCE(pm.provider,''), COALESCE(pm.test,1), u.email
	FROM orders o JOIN plans p ON p.id=o.plan_id JOIN users u ON u.id=o.user_id
	LEFT JOIN payments pm ON pm.id=o.payment_id LEFT JOIN cards c ON c.id=pm.card_id`

func scanOrder(r scanner) (*Order, error) {
	var o Order
	var filled sql.NullInt64
	err := r.Scan(&o.ID, &o.UserID, &o.PlanID, &o.PlanName, &o.Side, &o.Trigger, &o.Status, &o.AmountKop, &o.FeeKop, &o.USDBYN, &o.Error,
		&o.Broker, &o.CreatedAt, &filled, &o.CardLast4, &o.CardBrand, &o.PaymentUID, &o.Provider, &o.Test, &o.UserEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	o.FilledAt = nullInt(filled)
	o.Fills = []Fill{}
	return &o, err
}

func (s *Store) Orders(ctx context.Context, f OrderFilter) ([]*Order, int, error) {
	var where []string
	var args []any
	if f.UserID > 0 {
		where = append(where, "o.user_id=?")
		args = append(args, f.UserID)
	}
	if f.PlanID > 0 {
		where = append(where, "o.plan_id=?")
		args = append(args, f.PlanID)
	}
	if f.Side != "" {
		where = append(where, "o.side=?")
		args = append(args, f.Side)
	}
	if f.Status != "" {
		where = append(where, "o.status=?")
		args = append(args, f.Status)
	}
	if f.From > 0 {
		where = append(where, "o.created_at>=?")
		args = append(args, f.From)
	}
	if f.To > 0 {
		where = append(where, "o.created_at<?")
		args = append(args, f.To)
	}
	w := ""
	if len(where) > 0 {
		w = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders o`+w, args...).Scan(&total)
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, orderSelect+w+` ORDER BY o.id DESC LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	var out []*Order
	idx := map[int64]*Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, o)
		idx[o.ID] = o
	}
	rows.Close()
	if len(out) > 0 {
		ids := make([]string, 0, len(out))
		fargs := []any{}
		for _, o := range out {
			ids = append(ids, "?")
			fargs = append(fargs, o.ID)
		}
		fr, err := s.DB.QueryContext(ctx, `SELECT order_id, asset, qty, price_byn, price_usd, amount_kop FROM fills WHERE order_id IN (`+strings.Join(ids, ",")+`) ORDER BY id`, fargs...)
		if err == nil {
			for fr.Next() {
				var oid int64
				var x Fill
				fr.Scan(&oid, &x.Asset, &x.Qty, &x.PriceBYN, &x.PriceUSD, &x.AmountKop)
				if o := idx[oid]; o != nil {
					o.Fills = append(o.Fills, x)
				}
			}
			fr.Close()
		}
	}
	return out, total, nil
}

func (s *Store) Order(ctx context.Context, userID, id int64) (*Order, error) {
	q := orderSelect + ` WHERE o.id=?`
	args := []any{id}
	if userID > 0 {
		q += ` AND o.user_id=?`
		args = append(args, userID)
	}
	o, err := scanOrder(s.DB.QueryRowContext(ctx, q, args...))
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT asset, qty, price_byn, price_usd, amount_kop FROM fills WHERE order_id=? ORDER BY id`, id)
	if err != nil {
		return o, nil
	}
	defer rows.Close()
	for rows.Next() {
		var x Fill
		rows.Scan(&x.Asset, &x.Qty, &x.PriceBYN, &x.PriceUSD, &x.AmountKop)
		o.Fills = append(o.Fills, x)
	}
	return o, nil
}

// Payment — запись о движении денег через провайдера.
type Payment struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id"`
	CardID      *int64 `json:"card_id"`
	Provider    string `json:"provider"`
	Kind        string `json:"kind"`
	TrackingID  string `json:"tracking_id"`
	ProviderUID string `json:"provider_uid"`
	AmountKop   int64  `json:"amount_kop"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Test        bool   `json:"test"`
	CreatedAt   int64  `json:"created_at"`
	CardLast4   string `json:"card_last4"`
}

func (s *Store) Payments(ctx context.Context, userID int64, kind string, limit int) ([]Payment, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT pm.id, pm.user_id, pm.card_id, pm.provider, pm.kind, pm.tracking_id, pm.provider_uid, pm.amount_kop, pm.status, pm.message, pm.test, pm.created_at, COALESCE(c.last4,'')
		FROM payments pm LEFT JOIN cards c ON c.id=pm.card_id WHERE pm.user_id=? AND (?='' OR pm.kind=?) ORDER BY pm.id DESC LIMIT ?`, userID, kind, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Payment
	for rows.Next() {
		var p Payment
		var cid sql.NullInt64
		rows.Scan(&p.ID, &p.UserID, &cid, &p.Provider, &p.Kind, &p.TrackingID, &p.ProviderUID, &p.AmountKop, &p.Status, &p.Message, &p.Test, &p.CreatedAt, &p.CardLast4)
		p.CardID = nullInt(cid)
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- snapshots

type Snapshot struct {
	TS          int64 `json:"t"`
	ValueKop    int64 `json:"value_kop"`
	InvestedKop int64 `json:"invested_kop"`
}

func (s *Store) PutSnapshot(ctx context.Context, userID, planID, ts, value, invested int64) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO snapshots(user_id, plan_id, ts, value_kop, invested_kop) VALUES(?,?,?,?,?)
		ON CONFLICT(user_id, plan_id, ts) DO UPDATE SET value_kop=excluded.value_kop, invested_kop=excluded.invested_kop`, userID, planID, ts, value, invested)
	return err
}

func (s *Store) Snapshots(ctx context.Context, userID, planID, from int64) ([]Snapshot, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT ts, value_kop, invested_kop FROM snapshots WHERE user_id=? AND plan_id=? AND ts>=? ORDER BY ts`, userID, planID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Snapshot{}
	for rows.Next() {
		var x Snapshot
		rows.Scan(&x.TS, &x.ValueKop, &x.InvestedKop)
		out = append(out, x)
	}
	return out, rows.Err()
}

// UsersWithPositions — для почасовых снимков стоимости.
func (s *Store) UsersWithPositions(ctx context.Context) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT p.user_id FROM positions x JOIN plans p ON p.id=x.plan_id WHERE x.qty>0 OR x.cost_kop>0`)
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
