package store

import (
	"context"
	"database/sql"
)

// Passkey — ключ входа на устройстве (Face ID, Touch ID, отпечаток). Data — сериализованный webauthn.Credential.
type Passkey struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	CredID     string `json:"-"`
	Name       string `json:"name"`
	Data       string `json:"-"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt *int64 `json:"last_used_at"`
}

func (s *Store) Passkeys(ctx context.Context, userID int64) ([]Passkey, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, cred_id, name, data, created_at, last_used_at FROM passkeys WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var p Passkey
		var used sql.NullInt64
		if err := rows.Scan(&p.ID, &p.UserID, &p.CredID, &p.Name, &p.Data, &p.CreatedAt, &used); err != nil {
			return nil, err
		}
		p.LastUsedAt = nullInt(used)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) CountPasskeys(ctx context.Context, userID int64) int {
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM passkeys WHERE user_id=?`, userID).Scan(&n)
	return n
}

func (s *Store) AddPasskey(ctx context.Context, userID int64, credID, name, data string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO passkeys(user_id, cred_id, name, data, created_at) VALUES(?,?,?,?,?)`, userID, credID, name, data, Now())
	return err
}

// UpdatePasskeyUse сохраняет новый счётчик подписей (защита от клонированных ключей) и время использования.
func (s *Store) UpdatePasskeyUse(ctx context.Context, userID int64, credID, data string) {
	s.DB.ExecContext(ctx, `UPDATE passkeys SET data=?, last_used_at=? WHERE user_id=? AND cred_id=?`, data, Now(), userID, credID)
}

func (s *Store) DeletePasskey(ctx context.Context, userID, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM passkeys WHERE user_id=? AND id=?`, userID, id)
	return err
}
