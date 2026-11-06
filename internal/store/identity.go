package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
)

// ---------------------------------------------------------------- вход через Google / Apple

// Identity — привязанный внешний способ входа.
type Identity struct {
	Provider  string `json:"provider"` // google | apple
	Email     string `json:"email"`
	CreatedAt int64  `json:"created_at"`
}

// UserByIdentity — пользователь, у которого привязан этот аккаунт провайдера.
func (s *Store) UserByIdentity(ctx context.Context, provider, subject string) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userColsU+` FROM user_identities i JOIN users u ON u.id=i.user_id
		WHERE i.provider=? AND i.subject=? AND u.status!='deleted'`, provider, subject))
}

var ErrIdentityTaken = errors.New("Этот аккаунт уже привязан к другому пользователю AutoInvest")

// LinkIdentity привязывает аккаунт провайдера к пользователю (один аккаунт провайдера — один пользователь).
func (s *Store) LinkIdentity(ctx context.Context, userID int64, provider, subject, email string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO user_identities(provider, subject, user_id, email, created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(provider, subject) DO UPDATE SET email=CASE WHEN excluded.email!='' THEN excluded.email ELSE user_identities.email END
		WHERE user_identities.user_id=excluded.user_id`, provider, subject, userID, email, Now())
	if err != nil {
		return err
	}
	var owner int64
	s.DB.QueryRowContext(ctx, `SELECT user_id FROM user_identities WHERE provider=? AND subject=?`, provider, subject).Scan(&owner)
	if owner != userID {
		return ErrIdentityTaken
	}
	return nil
}

func (s *Store) Identities(ctx context.Context, userID int64) ([]Identity, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT provider, email, created_at FROM user_identities WHERE user_id=? ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Identity{}
	for rows.Next() {
		var i Identity
		if err := rows.Scan(&i.Provider, &i.Email, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Store) UnlinkIdentity(ctx context.Context, userID int64, provider string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM user_identities WHERE user_id=? AND provider=?`, userID, provider)
	return err
}

// ---------------------------------------------------------------- телефон

// UserByPhone — только по подтверждённому номеру: неподтверждённый номер мог указать кто угодно.
func (s *Store) UserByPhone(ctx context.Context, phone string) (*User, error) {
	return scanUser(s.DB.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE phone=? AND phone_verified_at IS NOT NULL AND status!='deleted'`, phone))
}

var ErrPhoneTaken = errors.New("Этот номер уже подтверждён в другом аккаунте")

// SetVerifiedPhone записывает номер как подтверждённый.
func (s *Store) SetVerifiedPhone(ctx context.Context, userID int64, phone string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET phone=?, phone_verified_at=?, updated_at=? WHERE id=?`, phone, Now(), Now(), userID)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrPhoneTaken
	}
	return err
}

func phoneCodeHash(phone, purpose, code string) string {
	h := sha256.Sum256([]byte(phone + "|" + purpose + "|" + code))
	return hex.EncodeToString(h[:])
}

// CreatePhoneCode сохраняет одноразовый код (храним только хеш); прежние коды того же назначения гасятся.
func (s *Store) CreatePhoneCode(ctx context.Context, phone, purpose string, userID int64, code string, ttl int64) error {
	now := Now()
	s.DB.ExecContext(ctx, `UPDATE phone_codes SET used=1 WHERE phone=? AND purpose=? AND used=0`, phone, purpose)
	s.DB.ExecContext(ctx, `DELETE FROM phone_codes WHERE expires_at<?`, now-86400)
	var uid any
	if userID > 0 {
		uid = userID
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO phone_codes(phone, purpose, user_id, code_hash, expires_at, created_at) VALUES(?,?,?,?,?,?)`,
		phone, purpose, uid, phoneCodeHash(phone, purpose, code), now+ttl, now)
	return err
}

var ErrCodeInvalid = errors.New("Неверный или устаревший код")

// UsePhoneCode проверяет код: не больше 5 попыток на код, код одноразовый. Возвращает user_id, сохранённый при выдаче.
func (s *Store) UsePhoneCode(ctx context.Context, phone, purpose, code string) (int64, error) {
	var id int64
	var hash string
	var attempts int
	var uid sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT id, code_hash, attempts, user_id FROM phone_codes WHERE phone=? AND purpose=? AND used=0 AND expires_at>? ORDER BY id DESC LIMIT 1`,
		phone, purpose, Now()).Scan(&id, &hash, &attempts, &uid)
	if err != nil {
		return 0, ErrCodeInvalid
	}
	if attempts >= 5 {
		s.DB.ExecContext(ctx, `UPDATE phone_codes SET used=1 WHERE id=?`, id)
		return 0, ErrCodeInvalid
	}
	if hash != phoneCodeHash(phone, purpose, code) {
		s.DB.ExecContext(ctx, `UPDATE phone_codes SET attempts=attempts+1 WHERE id=?`, id)
		return 0, ErrCodeInvalid
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE phone_codes SET used=1 WHERE id=? AND used=0`, id)
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrCodeInvalid
	}
	return uid.Int64, nil
}
