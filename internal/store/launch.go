package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"autoinvest/internal/auth"
)

// ---------------------------------------------------------------- лист ожидания

// JoinWaitlist отмечает, что человек дошёл до оплаты и ждёт запуска; возвращает его место в очереди.
func (s *Store) JoinWaitlist(ctx context.Context, userID int64) (int, error) {
	pos, _, err := s.JoinWaitlistFirst(ctx, userID)
	return pos, err
}

// JoinWaitlistFirst — то же, плюс first=true, если человек попал в список только что (для приветственных уведомлений).
func (s *Store) JoinWaitlistFirst(ctx context.Context, userID int64) (pos int, first bool, err error) {
	if res, e := s.DB.ExecContext(ctx, `UPDATE users SET waitlist_at=? WHERE id=? AND waitlist_at IS NULL`, Now(), userID); e == nil {
		n, _ := res.RowsAffected()
		first = n > 0
	}
	pos, err = s.WaitlistPosition(ctx, userID)
	return pos, first, err
}

func (s *Store) WaitlistPosition(ctx context.Context, userID int64) (int, error) {
	var at sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT waitlist_at FROM users WHERE id=?`, userID).Scan(&at); err != nil || !at.Valid {
		return 0, err
	}
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE waitlist_at IS NOT NULL AND role='user' AND (waitlist_at<? OR (waitlist_at=? AND id<=?))`,
		at.Int64, at.Int64, userID).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------- демо-счёт

// DemoUser — теневой пользователь для демо-счёта: отдельные копилки, карты и операции с виртуальными деньгами.
// Роль demo: не попадает в статистику, не может войти сам (нет пароля и настоящей почты).
func (s *Store) DemoUser(ctx context.Context, real *User) (*User, bool, error) {
	u, err := scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE demo_of=?`, real.ID))
	if err == nil {
		return u, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	now := Now()
	email := fmt.Sprintf("demo-%d@demo.autoinvest.local", real.ID)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO users(email, email_verified_at, phone, name, password_hash, role, referral_code, created_at, updated_at,
		demo_of, risk_profile, onboarded_at, notify_email, notify_telegram) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,0,0)`,
		email, now, "", real.Name, "", "demo", "D"+strings.ToUpper(auth.RandomCode(9, "")), now, now, real.ID, real.RiskProfile, now)
	if err != nil {
		return nil, false, err
	}
	id, _ := res.LastInsertId()
	u, err = s.UserByID(ctx, id)
	return u, true, err
}

// ---------------------------------------------------------------- посещения (для рекламы)

// TrackVisit — +1 посещение за день по источнику рекламы (utm_source/utm_campaign). Без cookie и персональных данных.
func (s *Store) TrackVisit(ctx context.Context, source, campaign string) {
	day := time.Now().Format("2006-01-02")
	s.DB.ExecContext(ctx, `INSERT INTO visits(day, source, campaign, count) VALUES(?,?,?,1)
		ON CONFLICT(day, source, campaign) DO UPDATE SET count=count+1`, day, source, campaign)
}

// Launch — воронка запуска для админки: посещения → регистрации → анкета → копилка → лист ожидания → демо → отзывы.
type LaunchFunnel struct {
	Visits30d     int            `json:"visits_30d"`
	VisitsToday   int            `json:"visits_today"`
	Registered    int            `json:"registered"`
	Onboarded     int            `json:"onboarded"`
	PlanCreated   int            `json:"plan_created"`
	Waitlist      int            `json:"waitlist"`
	DemoUsers     int            `json:"demo_users"`
	DemoInvested  int            `json:"demo_invested"`
	Feedback      int            `json:"feedback"`
	TelegramLinks int            `json:"telegram_links"`
	Sources       []LaunchSource `json:"sources"`
}

type LaunchSource struct {
	Source     string `json:"source"`
	Campaign   string `json:"campaign"`
	Visits     int    `json:"visits"`
	Registered int    `json:"registered"`
	Waitlist   int    `json:"waitlist"`
}

func (s *Store) LaunchFunnel(ctx context.Context) (LaunchFunnel, error) {
	var f LaunchFunnel
	q := func(dst *int, query string, args ...any) { s.DB.QueryRowContext(ctx, query, args...).Scan(dst) }
	from := time.Now().AddDate(0, 0, -30).Format("2006-01-02")
	today := time.Now().Format("2006-01-02")
	base := ` FROM users u WHERE u.role='user' AND u.status!='deleted'`
	q(&f.Visits30d, `SELECT COALESCE(SUM(count),0) FROM visits WHERE day>=?`, from)
	q(&f.VisitsToday, `SELECT COALESCE(SUM(count),0) FROM visits WHERE day=?`, today)
	q(&f.Registered, `SELECT COUNT(*)`+base)
	q(&f.Onboarded, `SELECT COUNT(*)`+base+` AND u.onboarded_at IS NOT NULL`)
	q(&f.PlanCreated, `SELECT COUNT(*)`+base+` AND EXISTS(SELECT 1 FROM plans p WHERE p.user_id=u.id)`)
	q(&f.Waitlist, `SELECT COUNT(*)`+base+` AND u.waitlist_at IS NOT NULL`)
	q(&f.DemoUsers, `SELECT COUNT(*) FROM users WHERE role='demo' AND status!='deleted'`)
	q(&f.DemoInvested, `SELECT COUNT(DISTINCT o.user_id) FROM orders o JOIN users d ON d.id=o.user_id WHERE d.role='demo' AND d.status!='deleted' AND o.side='buy' AND o.status='filled'`)
	q(&f.Feedback, `SELECT COUNT(*) FROM tickets WHERE category IN ('idea','bug')`)
	q(&f.TelegramLinks, `SELECT COUNT(*)`+base+` AND u.telegram_chat_id IS NOT NULL`)
	rows, err := s.DB.QueryContext(ctx, `
		WITH v AS (SELECT source, campaign, SUM(count) n FROM visits WHERE day>=? GROUP BY source, campaign),
		     r AS (SELECT utm_source source, utm_campaign campaign, COUNT(*) reg, SUM(waitlist_at IS NOT NULL) wl FROM users WHERE role='user' AND status!='deleted' GROUP BY 1, 2)
		SELECT v.source, v.campaign, v.n, COALESCE(r.reg,0), COALESCE(r.wl,0) FROM v LEFT JOIN r ON r.source=v.source AND r.campaign=v.campaign
		ORDER BY v.n DESC LIMIT 30`, from)
	if err != nil {
		return f, err
	}
	defer rows.Close()
	for rows.Next() {
		var x LaunchSource
		if err := rows.Scan(&x.Source, &x.Campaign, &x.Visits, &x.Registered, &x.Waitlist); err != nil {
			return f, err
		}
		f.Sources = append(f.Sources, x)
	}
	return f, rows.Err()
}

// AddBonusProDays — подарок к запуску (дни Pro начислятся, когда откроются пополнения).
func (s *Store) AddBonusProDays(ctx context.Context, userID int64, days int) (int, error) {
	if _, err := s.DB.ExecContext(ctx, `UPDATE users SET bonus_pro_days=MIN(bonus_pro_days+?, 365), updated_at=? WHERE id=?`, days, Now(), userID); err != nil {
		return 0, err
	}
	var total int
	err := s.DB.QueryRowContext(ctx, `SELECT bonus_pro_days FROM users WHERE id=?`, userID).Scan(&total)
	return total, err
}
