package store

import (
	"context"
	"database/sql"
)

// TelegramLogEntry — одно сообщение в переписке с ботом: что написал человек (in) и что ответил бот (out).
type TelegramLogEntry struct {
	ID        int64  `json:"id"`
	ChatID    int64  `json:"chat_id"`
	UserID    *int64 `json:"user_id"`
	Dir       string `json:"dir"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	CreatedAt int64  `json:"created_at"`
}

func (s *Store) LogTelegram(ctx context.Context, chatID int64, userID *int64, dir, kind, text string) {
	if r := []rune(text); len(r) > 4000 {
		text = string(r[:4000]) + "…"
	}
	s.DB.ExecContext(ctx, `INSERT INTO telegram_log(chat_id, user_id, dir, kind, text, created_at) VALUES(?,?,?,?,?,?)`, chatID, userID, dir, kind, text, Now())
}

// TelegramLog — переписка пользователя с ботом, новые сверху.
func (s *Store) TelegramLog(ctx context.Context, userID int64, limit int) ([]TelegramLogEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, chat_id, user_id, dir, kind, text, created_at FROM telegram_log WHERE user_id=? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TelegramLogEntry{}
	for rows.Next() {
		var e TelegramLogEntry
		var uid sql.NullInt64
		if err := rows.Scan(&e.ID, &e.ChatID, &uid, &e.Dir, &e.Kind, &e.Text, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.UserID = nullInt(uid)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PurgeTelegramLog удаляет переписку старше days дней.
func (s *Store) PurgeTelegramLog(ctx context.Context, days int) {
	s.DB.ExecContext(ctx, `DELETE FROM telegram_log WHERE created_at < ?`, Now()-int64(days)*86400)
}
