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
