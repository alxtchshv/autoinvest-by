package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"autoinvest/internal/market"
)

// ---------------------------------------------------------------- notifications

type Notification struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Link      string `json:"link"`
	Read      bool   `json:"read"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) AddNotification(ctx context.Context, userID int64, n Notification) (int64, error) {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO notifications(user_id, kind, title, body, link, created_at) VALUES(?,?,?,?,?,?)`,
		userID, n.Kind, n.Title, n.Body, n.Link, Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) Notifications(ctx context.Context, userID int64, limit int) ([]Notification, int, error) {
	var unread int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id=? AND read_at IS NULL`, userID).Scan(&unread)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, kind, title, body, link, read_at IS NOT NULL, created_at FROM notifications WHERE user_id=? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.Read, &n.CreatedAt)
		out = append(out, n)
	}
	return out, unread, rows.Err()
}

func (s *Store) MarkNotificationsRead(ctx context.Context, userID int64, id int64) error {
	if id > 0 {
		_, err := s.DB.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE user_id=? AND id=? AND read_at IS NULL`, Now(), userID, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE notifications SET read_at=? WHERE user_id=? AND read_at IS NULL`, Now(), userID)
	return err
}

// ---------------------------------------------------------------- support

type Ticket struct {
	ID          int64           `json:"id"`
	UserID      int64           `json:"user_id"`
	UserEmail   string          `json:"user_email,omitempty"`
	UserName    string          `json:"user_name,omitempty"`
	Subject     string          `json:"subject"`
	Category    string          `json:"category"`
	Status      string          `json:"status"`
	CreatedAt   int64           `json:"created_at"`
	UpdatedAt   int64           `json:"updated_at"`
	LastMessage string          `json:"last_message"`
	Messages    []TicketMessage `json:"messages,omitempty"`
}

type TicketMessage struct {
	ID         int64  `json:"id"`
	AuthorName string `json:"author_name"`
	IsStaff    bool   `json:"is_staff"`
	Body       string `json:"body"`
	CreatedAt  int64  `json:"created_at"`
}

func (s *Store) CreateTicket(ctx context.Context, userID int64, subject, category, body string) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := Now()
	res, err := tx.ExecContext(ctx, `INSERT INTO tickets(user_id, subject, category, status, created_at, updated_at) VALUES(?,?,?,?,?,?)`, userID, subject, category, "open", now, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ticket_messages(ticket_id, author_id, is_staff, body, created_at) VALUES(?,?,?,?,?)`, id, userID, false, body, now); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *Store) AddTicketMessage(ctx context.Context, ticketID, authorID int64, staff bool, body string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := Now()
	if _, err := tx.ExecContext(ctx, `INSERT INTO ticket_messages(ticket_id, author_id, is_staff, body, created_at) VALUES(?,?,?,?,?)`, ticketID, authorID, staff, body, now); err != nil {
		return err
	}
	status := "open"
	if staff {
		status = "answered"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tickets SET status=?, updated_at=? WHERE id=?`, status, now, ticketID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetTicketStatus(ctx context.Context, id int64, status string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE tickets SET status=?, updated_at=? WHERE id=?`, status, Now(), id)
	return err
}

const ticketSelect = `SELECT t.id, t.user_id, u.email, u.name, t.subject, t.category, t.status, t.created_at, t.updated_at,
	COALESCE((SELECT body FROM ticket_messages m WHERE m.ticket_id=t.id ORDER BY m.id DESC LIMIT 1), '')
	FROM tickets t JOIN users u ON u.id=t.user_id`

func scanTicket(r scanner) (*Ticket, error) {
	var t Ticket
	err := r.Scan(&t.ID, &t.UserID, &t.UserEmail, &t.UserName, &t.Subject, &t.Category, &t.Status, &t.CreatedAt, &t.UpdatedAt, &t.LastMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if len([]rune(t.LastMessage)) > 140 {
		t.LastMessage = string([]rune(t.LastMessage)[:140]) + "…"
	}
	return &t, err
}

// Tickets: userID=0 — все (для поддержки), status="" — любые.
func (s *Store) Tickets(ctx context.Context, userID int64, status string, limit int) ([]Ticket, error) {
	var where []string
	var args []any
	if userID > 0 {
		where = append(where, "t.user_id=?")
		args = append(args, userID)
	}
	if status != "" {
		where = append(where, "t.status=?")
		args = append(args, status)
	}
	q := ticketSelect
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += ` ORDER BY CASE t.status WHEN 'open' THEN 0 WHEN 'answered' THEN 1 ELSE 2 END, t.updated_at DESC LIMIT ?`
	rows, err := s.DB.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (s *Store) Ticket(ctx context.Context, userID, id int64) (*Ticket, error) {
	q := ticketSelect + ` WHERE t.id=?`
	args := []any{id}
	if userID > 0 {
		q += ` AND t.user_id=?`
		args = append(args, userID)
	}
	t, err := scanTicket(s.DB.QueryRowContext(ctx, q, args...))
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT m.id, CASE WHEN m.is_staff=1 THEN 'Поддержка AutoInvest' ELSE u.name END, m.is_staff, m.body, m.created_at
		FROM ticket_messages m JOIN users u ON u.id=m.author_id WHERE m.ticket_id=? ORDER BY m.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	t.Messages = []TicketMessage{}
	for rows.Next() {
		var m TicketMessage
		rows.Scan(&m.ID, &m.AuthorName, &m.IsStaff, &m.Body, &m.CreatedAt)
		t.Messages = append(t.Messages, m)
	}
	return t, rows.Err()
}

// ---------------------------------------------------------------- KYC

type KYC struct {
	UserID         int64  `json:"user_id"`
	UserEmail      string `json:"user_email,omitempty"`
	LastName       string `json:"last_name"`
	FirstName      string `json:"first_name"`
	MiddleName     string `json:"middle_name"`
	BirthDate      string `json:"birth_date"`
	PersonalNumber string `json:"personal_number"`
	DocNumber      string `json:"doc_number"`
	DocIssuedAt    string `json:"doc_issued_at"`
	Citizenship    string `json:"citizenship"`
	Address        string `json:"address"`
	DocFile        string `json:"-"`
	SelfieFile     string `json:"-"`
	HasDoc         bool   `json:"has_doc"`
	HasSelfie      bool   `json:"has_selfie"`
	Status         string `json:"status"`
	RejectReason   string `json:"reject_reason"`
	SubmittedAt    int64  `json:"submitted_at"`
	ReviewedAt     *int64 `json:"reviewed_at"`
	Method         string `json:"method"` // manual | msi | docs (Sumsub) | msi-sandbox | docs-sandbox
	ProviderRef    string `json:"-"`
}

const kycSelect = `SELECT k.user_id, u.email, k.last_name, k.first_name, k.middle_name, k.birth_date, k.personal_number, k.doc_number, k.doc_issued_at,
	k.citizenship, k.address, k.doc_file, k.selfie_file, k.status, k.reject_reason, k.submitted_at, k.reviewed_at, k.method, k.provider_ref FROM kyc k JOIN users u ON u.id=k.user_id`

func scanKYC(r scanner) (*KYC, error) {
	var k KYC
	var rev sql.NullInt64
	err := r.Scan(&k.UserID, &k.UserEmail, &k.LastName, &k.FirstName, &k.MiddleName, &k.BirthDate, &k.PersonalNumber, &k.DocNumber, &k.DocIssuedAt,
		&k.Citizenship, &k.Address, &k.DocFile, &k.SelfieFile, &k.Status, &k.RejectReason, &k.SubmittedAt, &rev, &k.Method, &k.ProviderRef)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	k.ReviewedAt = nullInt(rev)
	k.HasDoc, k.HasSelfie = k.DocFile != "", k.SelfieFile != ""
	return &k, err
}

func (s *Store) KYC(ctx context.Context, userID int64) (*KYC, error) {
	return scanKYC(s.DB.QueryRowContext(ctx, kycSelect+` WHERE k.user_id=?`, userID))
}

func (s *Store) KYCList(ctx context.Context, status string, limit int) ([]KYC, error) {
	rows, err := s.DB.QueryContext(ctx, kycSelect+` WHERE (?='' OR k.status=?) ORDER BY k.submitted_at DESC LIMIT ?`, status, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KYC{}
	for rows.Next() {
		k, err := scanKYC(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *k)
	}
	return out, rows.Err()
}

func (s *Store) SubmitKYC(ctx context.Context, k KYC) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO kyc(user_id, last_name, first_name, middle_name, birth_date, personal_number, doc_number, doc_issued_at, citizenship, address, doc_file, selfie_file, status, reject_reason, submitted_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'pending','',?)
		ON CONFLICT(user_id) DO UPDATE SET last_name=excluded.last_name, first_name=excluded.first_name, middle_name=excluded.middle_name,
		birth_date=excluded.birth_date, personal_number=excluded.personal_number, doc_number=excluded.doc_number, doc_issued_at=excluded.doc_issued_at,
		citizenship=excluded.citizenship, address=excluded.address,
		doc_file=CASE WHEN excluded.doc_file!='' THEN excluded.doc_file ELSE kyc.doc_file END,
		selfie_file=CASE WHEN excluded.selfie_file!='' THEN excluded.selfie_file ELSE kyc.selfie_file END,
		status='pending', reject_reason='', submitted_at=excluded.submitted_at, reviewed_at=NULL`,
		k.UserID, k.LastName, k.FirstName, k.MiddleName, k.BirthDate, k.PersonalNumber, k.DocNumber, k.DocIssuedAt, k.Citizenship, k.Address, k.DocFile, k.SelfieFile, Now())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET kyc_status='pending', updated_at=? WHERE id=?`, Now(), k.UserID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetKYCResult записывает итог внешней проверки (МСИ, Sumsub или их тестовые копии): status — pending, approved или rejected.
// Пока провайдер не вернул данные, вместо идентификационного номера хранится заглушка (номер уникален по таблице).
// Непустые поля из k перезаписывают сохранённые, пустые — не трогают.
func (s *Store) SetKYCResult(ctx context.Context, k KYC) error {
	if k.PersonalNumber == "" {
		k.PersonalNumber = fmt.Sprintf("pending-%d", k.UserID)
	}
	if k.Citizenship == "" {
		k.Citizenship = "BY"
	}
	now := Now()
	var reviewed any
	if k.Status != "pending" {
		reviewed = now
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO kyc(user_id, last_name, first_name, middle_name, birth_date, personal_number, doc_number, doc_issued_at, citizenship, address, doc_file, selfie_file, status, reject_reason, submitted_at, reviewed_at, method, provider_ref)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET
		last_name=CASE WHEN excluded.last_name!='' THEN excluded.last_name ELSE kyc.last_name END,
		first_name=CASE WHEN excluded.first_name!='' THEN excluded.first_name ELSE kyc.first_name END,
		middle_name=CASE WHEN excluded.middle_name!='' THEN excluded.middle_name ELSE kyc.middle_name END,
		birth_date=CASE WHEN excluded.birth_date!='' THEN excluded.birth_date ELSE kyc.birth_date END,
		personal_number=CASE WHEN excluded.personal_number NOT LIKE 'pending-%' OR kyc.personal_number LIKE 'pending-%' THEN excluded.personal_number ELSE kyc.personal_number END,
		doc_number=CASE WHEN excluded.doc_number!='' THEN excluded.doc_number ELSE kyc.doc_number END,
		doc_issued_at=CASE WHEN excluded.doc_issued_at!='' THEN excluded.doc_issued_at ELSE kyc.doc_issued_at END,
		address=CASE WHEN excluded.address!='' THEN excluded.address ELSE kyc.address END,
		doc_file=CASE WHEN excluded.doc_file!='' THEN excluded.doc_file ELSE kyc.doc_file END,
		selfie_file=CASE WHEN excluded.selfie_file!='' THEN excluded.selfie_file ELSE kyc.selfie_file END,
		status=excluded.status, reject_reason=excluded.reject_reason,
		submitted_at=CASE WHEN excluded.status='pending' OR kyc.method!=excluded.method THEN excluded.submitted_at ELSE kyc.submitted_at END,
		reviewed_at=excluded.reviewed_at, method=excluded.method,
		provider_ref=CASE WHEN excluded.provider_ref!='' THEN excluded.provider_ref ELSE kyc.provider_ref END`,
		k.UserID, k.LastName, k.FirstName, k.MiddleName, k.BirthDate, k.PersonalNumber, k.DocNumber, k.DocIssuedAt, k.Citizenship, k.Address,
		k.DocFile, k.SelfieFile, k.Status, k.RejectReason, now, reviewed, k.Method, k.ProviderRef)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET kyc_status=?, updated_at=? WHERE id=?`, k.Status, now, k.UserID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ReviewKYC(ctx context.Context, userID, reviewer int64, approve bool, reason string, submittedAt int64) error {
	status := "rejected"
	if approve {
		status = "approved"
		reason = ""
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE kyc SET status=?, reject_reason=?, reviewed_at=?, reviewed_by=? WHERE user_id=? AND status='pending' AND (?=0 OR submitted_at=?)`, status, reason, Now(), reviewer, userID, submittedAt, submittedAt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	tx.ExecContext(ctx, `UPDATE users SET kyc_status=?, updated_at=? WHERE id=?`, status, Now(), userID)
	return tx.Commit()
}

// ---------------------------------------------------------------- telegram

func (s *Store) CreateTelegramLink(ctx context.Context, userID int64, code string, ttl int64) error {
	s.DB.ExecContext(ctx, `DELETE FROM telegram_links WHERE user_id=? OR expires_at<?`, userID, Now())
	_, err := s.DB.ExecContext(ctx, `INSERT INTO telegram_links(code, user_id, expires_at) VALUES(?,?,?)`, code, userID, Now()+ttl)
	return err
}

func (s *Store) UseTelegramLink(ctx context.Context, code string) (int64, error) {
	var uid int64
	err := s.DB.QueryRowContext(ctx, `DELETE FROM telegram_links WHERE code=? AND expires_at>? RETURNING user_id`, code, Now()).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return uid, err
}

// ---------------------------------------------------------------- kv

func (s *Store) GetKV(ctx context.Context, key string) string {
	var v string
	s.DB.QueryRowContext(ctx, `SELECT value FROM kv WHERE key=?`, key).Scan(&v)
	return v
}

func (s *Store) SetKV(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO kv(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ---------------------------------------------------------------- referral

type ReferralStats struct {
	Invited  int `json:"invited"`
	Active   int `json:"active"`
	Rewarded int `json:"rewarded"`
}

type ReferralFriend struct {
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	Active    bool   `json:"active"`
}

func (s *Store) Referrals(ctx context.Context, userID int64) (ReferralStats, []ReferralFriend, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT u.name, u.created_at, u.referral_rewarded,
		EXISTS(SELECT 1 FROM orders o WHERE o.user_id=u.id AND o.status='filled' AND o.side='buy')
		FROM users u WHERE u.referred_by=? AND u.status!='deleted' ORDER BY u.id DESC LIMIT 200`, userID)
	if err != nil {
		return ReferralStats{}, nil, err
	}
	defer rows.Close()
	var st ReferralStats
	out := []ReferralFriend{}
	for rows.Next() {
		var f ReferralFriend
		var rewarded bool
		rows.Scan(&f.Name, &f.CreatedAt, &rewarded, &f.Active)
		// показываем только имя и первую букву фамилии
		if parts := strings.Fields(f.Name); len(parts) > 1 {
			f.Name = parts[0] + " " + string([]rune(parts[1])[0]) + "."
		}
		st.Invited++
		if f.Active {
			st.Active++
		}
		if rewarded {
			st.Rewarded++
		}
		out = append(out, f)
	}
	return st, out, rows.Err()
}

// LoadRates и SaveRates хранят последний настоящий курс НБРБ в таблице kv (реализуют market.RatePersister).
func (s *Store) LoadRates(ctx context.Context) (market.Rates, bool) {
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM kv WHERE key='nbrb_rates'`).Scan(&raw); err != nil {
		return market.Rates{}, false
	}
	var r market.Rates
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return market.Rates{}, false
	}
	return r, true
}

func (s *Store) SaveRates(ctx context.Context, r market.Rates) {
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	s.DB.ExecContext(ctx, `INSERT INTO kv(key, value) VALUES('nbrb_rates', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b))
}
