// Package store — доступ к данным (SQLite). Только SQL и модели, без бизнес-логики.
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"autoinvest/internal/auth"
)

var ErrNotFound = errors.New("не найдено")

type Store struct{ DB *sql.DB }

func New(d *sql.DB) *Store { return &Store{DB: d} }

func Now() int64 { return time.Now().Unix() }

func nullInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	x := v.Int64
	return &x
}

func nullStr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	x := v.String
	return &x
}

type User struct {
	ID               int64  `json:"id"`
	Email            string `json:"email"`
	EmailVerified    bool   `json:"email_verified"`
	Phone            string `json:"phone"`
	PhoneVerified    bool   `json:"phone_verified"`
	HasPassword      bool   `json:"has_password"`
	Name             string `json:"name"`
	PasswordHash     string `json:"-"`
	Role             string `json:"role"`
	Status           string `json:"status"`
	TOTPSecret       string `json:"-"`
	TOTPEnabled      bool   `json:"totp_enabled"`
	TOTPLastStep     int64  `json:"-"`
	RecoveryCodes    string `json:"-"`
	Tier             string `json:"tier"`
	ProUntil         *int64 `json:"pro_until"`
	ProAutorenew     bool   `json:"pro_autorenew"`
	ProPeriod        string `json:"pro_period"`
	ReferralCode     string `json:"referral_code"`
	ReferredBy       *int64 `json:"-"`
	ReferralRewarded bool   `json:"-"`
	RiskProfile      string `json:"risk_profile"`
	KYCStatus        string `json:"kyc_status"`
	TelegramChatID   *int64 `json:"-"`
	TelegramUsername string `json:"telegram_username"`
	NotifyEmail      bool   `json:"notify_email"`
	NotifyTelegram   bool   `json:"notify_telegram"`
	NotifyMarket     bool   `json:"notify_market"`
	CreatedAt        int64  `json:"created_at"`
	LastLoginAt      *int64 `json:"last_login_at"`
	OnboardedAt      *int64 `json:"onboarded_at"`
}

// IsPro — действует ли подписка.
func (u *User) IsPro() bool {
	return u.Tier == "pro" && u.ProUntil != nil && *u.ProUntil > Now()
}

func (u *User) IsStaff() bool { return u.Role == "admin" || u.Role == "support" }

const userCols = `id, email, email_verified_at, phone, name, password_hash, role, status, totp_secret, totp_enabled,
recovery_codes, tier, pro_until, pro_autorenew, pro_period, referral_code, referred_by, referral_rewarded, risk_profile,
kyc_status, telegram_chat_id, telegram_username, notify_email, notify_telegram, notify_market, created_at, last_login_at, totp_last_step, onboarded_at, phone_verified_at`

// userColsU — те же колонки с префиксом таблицы u (для JOIN).
var userColsU = "u." + strings.ReplaceAll(strings.Join(strings.Fields(userCols), " "), ", ", ", u.")

type scanner interface{ Scan(...any) error }

func scanUser(r scanner) (*User, error) {
	var u User
	var verified, proUntil, refBy, tg, lastLogin, onboarded, phoneVerified sql.NullInt64
	err := r.Scan(&u.ID, &u.Email, &verified, &u.Phone, &u.Name, &u.PasswordHash, &u.Role, &u.Status, &u.TOTPSecret,
		&u.TOTPEnabled, &u.RecoveryCodes, &u.Tier, &proUntil, &u.ProAutorenew, &u.ProPeriod, &u.ReferralCode, &refBy,
		&u.ReferralRewarded, &u.RiskProfile, &u.KYCStatus, &tg, &u.TelegramUsername, &u.NotifyEmail, &u.NotifyTelegram,
		&u.NotifyMarket, &u.CreatedAt, &lastLogin, &u.TOTPLastStep, &onboarded, &phoneVerified)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.EmailVerified = verified.Valid
	u.ProUntil, u.ReferredBy, u.TelegramChatID, u.LastLoginAt = nullInt(proUntil), nullInt(refBy), nullInt(tg), nullInt(lastLogin)
	u.OnboardedAt = nullInt(onboarded)
	u.PhoneVerified = phoneVerified.Valid && u.Phone != ""
	u.HasPassword = u.PasswordHash != ""
	return &u, nil
}

func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=?`, id))
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email=? AND status!='deleted'`, strings.TrimSpace(email)))
}

func (s *Store) UserByReferral(ctx context.Context, code string) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE referral_code=? AND status='active'`, strings.ToUpper(strings.TrimSpace(code))))
}

func (s *Store) UserByTelegram(ctx context.Context, chatID int64) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE telegram_chat_id=? AND status='active'`, chatID))
}

type NewUser struct {
	Email, Phone, Name, PasswordHash, Role string
	ReferredBy                             *int64
	EmailVerified                          bool
}

func (s *Store) CreateUser(ctx context.Context, n NewUser) (*User, error) {
	if n.Role == "" {
		n.Role = "user"
	}
	now := Now()
	var verified any
	if n.EmailVerified {
		verified = now
	}
	for i := 0; i < 5; i++ {
		code := auth.RandomCode(8, "")
		res, err := s.DB.ExecContext(ctx, `INSERT INTO users(email, email_verified_at, phone, name, password_hash, role, referral_code, referred_by, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, strings.TrimSpace(n.Email), verified, n.Phone, n.Name, n.PasswordHash, n.Role, code, n.ReferredBy, now, now)
		if err != nil {
			if strings.Contains(err.Error(), "users.referral_code") {
				continue
			}
			if strings.Contains(err.Error(), "UNIQUE") && strings.Contains(err.Error(), "email") {
				return nil, ErrEmailTaken
			}
			return nil, err
		}
		id, _ := res.LastInsertId()
		return s.UserByID(ctx, id)
	}
	return nil, errors.New("не удалось создать пользователя")
}

var ErrEmailTaken = errors.New("Пользователь с таким email уже зарегистрирован")

// UpdateUserFields — точечное обновление разрешённых колонок.
func (s *Store) UpdateUserFields(ctx context.Context, id int64, fields map[string]any) error {
	allowed := map[string]bool{"name": true, "phone": true, "password_hash": true, "totp_secret": true, "totp_enabled": true,
		"recovery_codes": true, "tier": true, "pro_until": true, "pro_autorenew": true, "pro_period": true, "risk_profile": true,
		"kyc_status": true, "telegram_chat_id": true, "telegram_username": true, "notify_email": true, "notify_telegram": true,
		"notify_market": true, "status": true, "role": true, "email_verified_at": true, "last_login_at": true, "referral_rewarded": true, "email": true, "totp_last_step": true, "onboarded_at": true, "phone_verified_at": true}
	var sets []string
	var args []any
	for k, v := range fields {
		if !allowed[k] {
			return errors.New("недопустимое поле " + k)
		}
		sets = append(sets, k+"=?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at=?")
	args = append(args, Now(), id)
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...)
	return err
}

// ---------------------------------------------------------------- sessions

type Session struct {
	ID         string `json:"id"`
	UserID     int64  `json:"-"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt int64  `json:"last_seen_at"`
	ExpiresAt  int64  `json:"expires_at"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
	MFAPending bool   `json:"-"`
	Current    bool   `json:"current"`
}

func (s *Store) CreateSession(ctx context.Context, hash string, userID int64, ttl time.Duration, ip, ua string, mfaPending bool) error {
	now := Now()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO sessions(id, user_id, created_at, last_seen_at, expires_at, ip, user_agent, mfa_pending) VALUES(?,?,?,?,?,?,?,?)`,
		hash, userID, now, now, now+int64(ttl/time.Second), ip, ua, mfaPending)
	return err
}

func (s *Store) SessionByHash(ctx context.Context, hash string) (*Session, error) {
	var x Session
	err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, created_at, last_seen_at, expires_at, ip, user_agent, mfa_pending FROM sessions WHERE id=? AND expires_at>?`, hash, Now()).
		Scan(&x.ID, &x.UserID, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt, &x.IP, &x.UserAgent, &x.MFAPending)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

func (s *Store) TouchSession(ctx context.Context, hash string, ip string) {
	s.DB.ExecContext(ctx, `UPDATE sessions SET last_seen_at=?, ip=? WHERE id=?`, Now(), ip, hash)
}

func (s *Store) CompleteMFA(ctx context.Context, hash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE sessions SET mfa_pending=0 WHERE id=?`, hash)
	return err
}

func (s *Store) DeleteSession(ctx context.Context, hash string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, hash)
	return err
}

func (s *Store) DeleteUserSession(ctx context.Context, userID int64, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE id=? AND user_id=?`, id, userID)
	return err
}

func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, keep string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=? AND id!=?`, userID, keep)
	return err
}

func (s *Store) ListSessions(ctx context.Context, userID int64) ([]Session, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, created_at, last_seen_at, expires_at, ip, user_agent FROM sessions WHERE user_id=? AND expires_at>? AND mfa_pending=0 ORDER BY last_seen_at DESC`, userID, Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var x Session
		if err := rows.Scan(&x.ID, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt, &x.IP, &x.UserAgent); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- email tokens

func (s *Store) CreateEmailToken(ctx context.Context, userID int64, kind string, ttl time.Duration) (string, error) {
	tok, hash := auth.NewToken()
	s.DB.ExecContext(ctx, `UPDATE email_tokens SET used_at=? WHERE user_id=? AND kind=? AND used_at IS NULL`, Now(), userID, kind)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO email_tokens(token_hash, user_id, kind, expires_at) VALUES(?,?,?,?)`, hash, userID, kind, Now()+int64(ttl/time.Second))
	return tok, err
}

// UseEmailToken помечает токен использованным и возвращает пользователя.
func (s *Store) UseEmailToken(ctx context.Context, token, kind string) (int64, error) {
	var uid int64
	err := s.DB.QueryRowContext(ctx, `UPDATE email_tokens SET used_at=? WHERE token_hash=? AND kind=? AND used_at IS NULL AND expires_at>? RETURNING user_id`,
		Now(), auth.HashToken(token), kind, Now()).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return uid, err
}

// ---------------------------------------------------------------- audit

type clientKey struct{}

// ClientInfo — откуда пришёл запрос. API кладёт его в контекст, журнал действий берёт оттуда
// IP и User-Agent, даже если вызывающий код их явно не передал.
type ClientInfo struct {
	IP        string
	UserAgent string
}

func WithClient(ctx context.Context, c ClientInfo) context.Context {
	return context.WithValue(ctx, clientKey{}, c)
}

func clientFrom(ctx context.Context) ClientInfo {
	c, _ := ctx.Value(clientKey{}).(ClientInfo)
	return c
}

func (s *Store) Audit(ctx context.Context, userID, actorID int64, action, details, ip string) {
	c := clientFrom(ctx)
	if ip == "" {
		ip = c.IP
	}
	ua := c.UserAgent
	if len(ua) > 300 {
		ua = ua[:300]
	}
	s.DB.ExecContext(ctx, `INSERT INTO audit_log(user_id, actor_id, action, details, ip, user_agent, created_at) VALUES(?,?,?,?,?,?,?)`,
		userID, actorID, action, details, ip, ua, Now())
}

type AuditEntry struct {
	Action    string `json:"action"`
	Details   string `json:"details"`
	IP        string `json:"ip"`
	UserAgent string `json:"user_agent"`
	CreatedAt int64  `json:"created_at"`
}

// AuditFor — последние события пользователя; prefix (например "auth.") сужает выборку.
func (s *Store) AuditFor(ctx context.Context, userID int64, limit int, prefix ...string) ([]AuditEntry, error) {
	like := "%"
	if len(prefix) > 0 && prefix[0] != "" {
		like = prefix[0] + "%"
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT action, details, ip, user_agent, created_at FROM audit_log WHERE user_id=? AND action LIKE ? ORDER BY id DESC LIMIT ?`, userID, like, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		rows.Scan(&a.Action, &a.Details, &a.IP, &a.UserAgent, &a.CreatedAt)
		out = append(out, a)
	}
	return out, rows.Err()
}

// AuditRange — события пользователя за период [from, to) по возрастанию времени; 0 — без границы.
func (s *Store) AuditRange(ctx context.Context, userID, from, to int64, limit int) ([]AuditEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT action, details, ip, user_agent, created_at FROM audit_log
		WHERE user_id=? AND (?=0 OR created_at>=?) AND (?=0 OR created_at<?) ORDER BY id DESC LIMIT ?`, userID, from, from, to, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.Action, &a.Details, &a.IP, &a.UserAgent, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Версии юридических документов. При изменении текста документа версия повышается,
// и пользователю нужно принять новую редакцию.
const (
	ConsentOffer   = "offer"
	ConsentRisks   = "risks"
	ConsentPrivacy = "privacy"
)

// RecordConsent сохраняет факт согласия: когда, с какого адреса и браузера, на какую редакцию документа.
func (s *Store) RecordConsent(ctx context.Context, userID int64, kind, version string) error {
	c := clientFrom(ctx)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO consents(user_id, kind, version, accepted_at, ip, user_agent) VALUES(?,?,?,?,?,?)`,
		userID, kind, version, Now(), c.IP, c.UserAgent)
	return err
}

type Consent struct {
	Kind       string `json:"kind"`
	Version    string `json:"version"`
	AcceptedAt int64  `json:"accepted_at"`
	IP         string `json:"ip"`
	RevokedAt  *int64 `json:"revoked_at"`
}

// Consents — история согласий пользователя.
func (s *Store) Consents(ctx context.Context, userID int64) ([]Consent, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT kind, version, accepted_at, ip, revoked_at FROM consents WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Consent{}
	for rows.Next() {
		var c Consent
		if err := rows.Scan(&c.Kind, &c.Version, &c.AcceptedAt, &c.IP, &c.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RevokeConsents отмечает отзыв согласий (при удалении аккаунта).
func (s *Store) RevokeConsents(ctx context.Context, userID int64) {
	s.DB.ExecContext(ctx, `UPDATE consents SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, Now(), userID)
}
