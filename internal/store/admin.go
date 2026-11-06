package store

import (
	"context"
	"strings"
)

type Overview struct {
	Users          int   `json:"users"`
	UsersToday     int   `json:"users_today"`
	Users7d        int   `json:"users_7d"`
	ActivePlans    int   `json:"active_plans"`
	PausedPlans    int   `json:"paused_plans"`
	Investors      int   `json:"investors"`
	Pro            int   `json:"pro"`
	VolumeKop      int64 `json:"volume_kop"`
	Volume30dKop   int64 `json:"volume_30d_kop"`
	FeesKop        int64 `json:"fees_kop"`
	Orders30d      int   `json:"orders_30d"`
	FailedOrders7d int   `json:"failed_orders_7d"`
	OpenTickets    int   `json:"open_tickets"`
	PendingKYC     int   `json:"pending_kyc"`
	PendingPayouts int   `json:"pending_payouts"`
	MonthlyRunKop  int64 `json:"monthly_run_kop"` // ожидаемый месячный объём пополнений по активным копилкам
}

type DayPoint struct {
	Day       string `json:"day"`
	Signups   int    `json:"signups"`
	VolumeKop int64  `json:"volume_kop"`
}

func (s *Store) Overview(ctx context.Context) (Overview, error) {
	var o Overview
	now := Now()
	day := now - 86400
	q := func(dst any, sql string, args ...any) { s.DB.QueryRowContext(ctx, sql, args...).Scan(dst) }
	q(&o.Users, `SELECT COUNT(*) FROM users WHERE status!='deleted' AND role='user'`)
	q(&o.UsersToday, `SELECT COUNT(*) FROM users WHERE created_at>? AND role='user'`, day)
	q(&o.Users7d, `SELECT COUNT(*) FROM users WHERE created_at>? AND role='user'`, now-7*86400)
	q(&o.ActivePlans, `SELECT COUNT(*) FROM plans WHERE status='active'`)
	q(&o.PausedPlans, `SELECT COUNT(*) FROM plans WHERE status='paused'`)
	q(&o.Investors, `SELECT COUNT(DISTINCT user_id) FROM orders WHERE status='filled' AND side='buy'`)
	q(&o.Pro, `SELECT COUNT(*) FROM users WHERE tier='pro' AND pro_until>?`, now)
	q(&o.VolumeKop, `SELECT COALESCE(SUM(amount_kop),0) FROM orders WHERE status='filled' AND side='buy'`)
	q(&o.Volume30dKop, `SELECT COALESCE(SUM(amount_kop),0) FROM orders WHERE status='filled' AND side='buy' AND created_at>?`, now-30*86400)
	q(&o.FeesKop, `SELECT COALESCE(SUM(fee_kop),0) FROM orders WHERE status='filled'`)
	q(&o.Orders30d, `SELECT COUNT(*) FROM orders WHERE created_at>?`, now-30*86400)
	q(&o.FailedOrders7d, `SELECT COUNT(*) FROM orders WHERE status='failed' AND created_at>?`, now-7*86400)
	q(&o.OpenTickets, `SELECT COUNT(*) FROM tickets WHERE status='open'`)
	q(&o.PendingKYC, `SELECT COUNT(*) FROM kyc WHERE status='pending'`)
	q(&o.PendingPayouts, `SELECT COUNT(*) FROM orders WHERE status='payout_pending'`)
	q(&o.MonthlyRunKop, `SELECT COALESCE(SUM(CASE frequency WHEN 'daily' THEN amount_kop*30 WHEN 'weekly' THEN amount_kop*52/12
		WHEN 'biweekly' THEN amount_kop*26/12 WHEN 'monthly' THEN amount_kop ELSE 0 END),0) FROM plans WHERE status='active'`)
	return o, nil
}

func (s *Store) DailySeries(ctx context.Context, days int) ([]DayPoint, error) {
	from := Now() - int64(days)*86400
	m := map[string]*DayPoint{}
	rows, err := s.DB.QueryContext(ctx, `SELECT date(created_at,'unixepoch'), COUNT(*) FROM users WHERE created_at>? AND role='user' GROUP BY 1`, from)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d string
		var n int
		rows.Scan(&d, &n)
		m[d] = &DayPoint{Day: d, Signups: n}
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT date(created_at,'unixepoch'), SUM(amount_kop) FROM orders WHERE created_at>? AND status='filled' AND side='buy' GROUP BY 1`, from)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d string
		var v int64
		rows.Scan(&d, &v)
		if m[d] == nil {
			m[d] = &DayPoint{Day: d}
		}
		m[d].VolumeKop = v
	}
	rows.Close()
	var out []DayPoint
	for i := days - 1; i >= 0; i-- {
		var d string
		s.DB.QueryRowContext(ctx, `SELECT date(?,'unixepoch')`, Now()-int64(i)*86400).Scan(&d)
		if p := m[d]; p != nil {
			out = append(out, *p)
		} else {
			out = append(out, DayPoint{Day: d})
		}
	}
	return out, nil
}

type AdminUser struct {
	User
	Plans       int   `json:"plans"`
	InvestedKop int64 `json:"invested_kop"`
	Cards       int   `json:"cards"`
}

func (s *Store) AdminUsers(ctx context.Context, query string, limit, offset int) ([]AdminUser, int, error) {
	where := ` WHERE status!='deleted'`
	var args []any
	if q := strings.TrimSpace(query); q != "" {
		where += ` AND (email LIKE ? OR name LIKE ? OR phone LIKE ? OR CAST(id AS TEXT)=?)`
		like := "%" + q + "%"
		args = append(args, like, like, like, q)
	}
	var total int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`+where, args...).Scan(&total)
	rows, err := s.DB.QueryContext(ctx, `SELECT `+userCols+`,
		(SELECT COUNT(*) FROM plans p WHERE p.user_id=users.id AND p.status!='closed'),
		(SELECT COALESCE(SUM(x.cost_kop),0) FROM positions x JOIN plans p ON p.id=x.plan_id WHERE p.user_id=users.id),
		(SELECT COUNT(*) FROM cards c WHERE c.user_id=users.id AND c.status='active')
		FROM users`+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []AdminUser
	for rows.Next() {
		var a AdminUser
		var extra = []any{&a.Plans, &a.InvestedKop, &a.Cards}
		u, err := scanUser(multiScanner{rows, extra})
		if err != nil {
			return nil, 0, err
		}
		a.User = *u
		out = append(out, a)
	}
	return out, total, rows.Err()
}

// multiScanner добавляет дополнительные колонки к стандартному набору пользователя.
type multiScanner struct {
	r     scanner
	extra []any
}

func (m multiScanner) Scan(dst ...any) error { return m.r.Scan(append(dst, m.extra...)...) }

// PartnerMetrics — показатели, которые спрашивают партнёры и инвесторы: воронка, удержание,
// средний взнос, объём по активам и когорты регистраций.
type PartnerMetrics struct {
	Funnel struct {
		Registered    int `json:"registered"`
		EmailVerified int `json:"email_verified"`
		WithCard      int `json:"with_card"`
		WithPlan      int `json:"with_plan"`
		Invested      int `json:"invested"`
	} `json:"funnel"`
	// Retention30d — доля инвесторов, сделавших первую покупку больше 30 дней назад
	// и пополнявших копилки за последние 30 дней.
	Retention30d    float64       `json:"retention_30d"`
	RetentionBase   int           `json:"retention_base"`
	AvgTicketKop    int64         `json:"avg_ticket_kop"`   // средняя сумма пополнения за 30 дней
	ActiveInvestors int           `json:"active_investors"` // пополняли за 30 дней
	AssetVolume30d  []AssetVolume `json:"asset_volume_30d"`
	Holdings        []AssetQty    `json:"holdings"` // суммарные остатки по активам (для оценки AUM)
	Cohorts         []Cohort      `json:"cohorts"`
}

type AssetVolume struct {
	Asset     string `json:"asset"`
	AmountKop int64  `json:"amount_kop"`
}

type AssetQty struct {
	Asset   string `json:"asset"`
	Qty     int64  `json:"qty"` // 1e-8
	CostKop int64  `json:"cost_kop"`
}

type Cohort struct {
	Month      string `json:"month"`
	Registered int    `json:"registered"`
	Invested   int    `json:"invested"`
	Active30d  int    `json:"active_30d"`
}

func (s *Store) PartnerMetrics(ctx context.Context) (PartnerMetrics, error) {
	var m PartnerMetrics
	now := Now()
	month := now - 30*86400
	q := func(dst any, sql string, args ...any) error { return s.DB.QueryRowContext(ctx, sql, args...).Scan(dst) }
	base := `FROM users u WHERE u.status!='deleted' AND u.role='user'`
	if err := q(&m.Funnel.Registered, `SELECT COUNT(*) `+base); err != nil {
		return m, err
	}
	q(&m.Funnel.EmailVerified, `SELECT COUNT(*) `+base+` AND u.email_verified_at IS NOT NULL`)
	q(&m.Funnel.WithCard, `SELECT COUNT(*) `+base+` AND EXISTS(SELECT 1 FROM cards c WHERE c.user_id=u.id)`)
	q(&m.Funnel.WithPlan, `SELECT COUNT(*) `+base+` AND EXISTS(SELECT 1 FROM plans p WHERE p.user_id=u.id)`)
	q(&m.Funnel.Invested, `SELECT COUNT(*) `+base+` AND EXISTS(SELECT 1 FROM orders o WHERE o.user_id=u.id AND o.side='buy' AND o.status='filled')`)
	q(&m.ActiveInvestors, `SELECT COUNT(DISTINCT user_id) FROM orders WHERE side='buy' AND status='filled' AND created_at>?`, month)
	q(&m.AvgTicketKop, `SELECT COALESCE(AVG(amount_kop),0) FROM orders WHERE side='buy' AND status='filled' AND created_at>?`, month)

	var retained int
	q(&m.RetentionBase, `SELECT COUNT(*) FROM (SELECT user_id FROM orders WHERE side='buy' AND status='filled' GROUP BY user_id HAVING MIN(created_at)<?)`, month)
	q(&retained, `SELECT COUNT(*) FROM (SELECT user_id FROM orders WHERE side='buy' AND status='filled' GROUP BY user_id
		HAVING MIN(created_at)<? AND MAX(created_at)>?)`, month, month)
	if m.RetentionBase > 0 {
		m.Retention30d = float64(retained) / float64(m.RetentionBase)
	}

	rows, err := s.DB.QueryContext(ctx, `SELECT f.asset, SUM(f.amount_kop) FROM fills f JOIN orders o ON o.id=f.order_id
		WHERE o.side='buy' AND o.status='filled' AND o.created_at>? GROUP BY f.asset ORDER BY 2 DESC`, month)
	if err != nil {
		return m, err
	}
	for rows.Next() {
		var a AssetVolume
		if err := rows.Scan(&a.Asset, &a.AmountKop); err != nil {
			rows.Close()
			return m, err
		}
		m.AssetVolume30d = append(m.AssetVolume30d, a)
	}
	rows.Close()

	rows, err = s.DB.QueryContext(ctx, `SELECT x.asset, SUM(x.qty), SUM(x.cost_kop) FROM positions x JOIN plans p ON p.id=x.plan_id
		WHERE x.qty>0 GROUP BY x.asset ORDER BY 3 DESC`)
	if err != nil {
		return m, err
	}
	for rows.Next() {
		var a AssetQty
		if err := rows.Scan(&a.Asset, &a.Qty, &a.CostKop); err != nil {
			rows.Close()
			return m, err
		}
		m.Holdings = append(m.Holdings, a)
	}
	rows.Close()

	rows, err = s.DB.QueryContext(ctx, `SELECT strftime('%Y-%m', u.created_at, 'unixepoch') AS mon, COUNT(*),
		SUM(EXISTS(SELECT 1 FROM orders o WHERE o.user_id=u.id AND o.side='buy' AND o.status='filled')),
		SUM(EXISTS(SELECT 1 FROM orders o WHERE o.user_id=u.id AND o.side='buy' AND o.status='filled' AND o.created_at>?))
		`+base+` AND u.created_at>? GROUP BY mon ORDER BY mon DESC`, month, now-186*86400)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var c Cohort
		if err := rows.Scan(&c.Month, &c.Registered, &c.Invested, &c.Active30d); err != nil {
			return m, err
		}
		m.Cohorts = append(m.Cohorts, c)
	}
	if m.AssetVolume30d == nil {
		m.AssetVolume30d = []AssetVolume{}
	}
	if m.Holdings == nil {
		m.Holdings = []AssetQty{}
	}
	if m.Cohorts == nil {
		m.Cohorts = []Cohort{}
	}
	return m, rows.Err()
}
