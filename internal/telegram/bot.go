// Package telegram — бот через официальный Bot API (long polling): привязка аккаунта,
// баланс, список копилок, уведомления о пополнениях.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"autoinvest/internal/engine"
	"autoinvest/internal/money"
	"autoinvest/internal/store"
)

type Bot struct {
	Token     string
	Username  string
	PublicURL string
	API       string
	St        *store.Store
	En        *engine.Engine
	Log       *slog.Logger
	http      *http.Client
}

func New(token, username, publicURL string, st *store.Store, en *engine.Engine, log *slog.Logger) *Bot {
	return &Bot{Token: token, Username: username, PublicURL: publicURL, API: "https://api.telegram.org", St: st, En: en, Log: log,
		http: &http.Client{Timeout: 40 * time.Second}}
}

func (b *Bot) call(ctx context.Context, method string, payload any, out any) error {
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.API+"/bot"+b.Token+"/"+method, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	if !env.OK {
		return errors.New("telegram " + method + ": " + env.Description)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// Send — реализует notify.Messenger.
func (b *Bot) Send(chatID int64, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return b.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true}, nil)
}

// Check проверяет токен и узнаёт имя бота.
func (b *Bot) Check(ctx context.Context) error {
	var me struct {
		Username string `json:"username"`
	}
	if err := b.call(ctx, "getMe", map[string]any{}, &me); err != nil {
		return err
	}
	if b.Username == "" {
		b.Username = me.Username
	}
	b.call(ctx, "setMyCommands", map[string]any{"commands": []map[string]string{
		{"command": "balance", "description": "Баланс и доходность"},
		{"command": "plans", "description": "Мои копилки"},
		{"command": "help", "description": "Помощь"},
	}}, nil)
	return nil
}

type update struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		From struct {
			Username  string `json:"username"`
			FirstName string `json:"first_name"`
		} `json:"from"`
	} `json:"message"`
}

// Run — цикл long polling.
func (b *Bot) Run(ctx context.Context) {
	var offset int64
	backoff := time.Second
	for ctx.Err() == nil {
		var ups []update
		err := b.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message"}}, &ups)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.Log.Warn("telegram getUpdates", "err", err)
			time.Sleep(backoff)
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		for _, u := range ups {
			offset = u.UpdateID + 1
			if u.Message != nil && u.Message.Chat.Type == "private" {
				b.handle(ctx, u.Message.Chat.ID, u.Message.From.Username, strings.TrimSpace(u.Message.Text))
			}
		}
	}
}

func (b *Bot) handle(ctx context.Context, chat int64, username, text string) {
	cmd, arg, _ := strings.Cut(text, " ")
	cmd = strings.ToLower(strings.Split(cmd, "@")[0])
	switch cmd {
	case "/start":
		if arg != "" {
			b.bind(ctx, chat, username, strings.TrimSpace(arg))
			return
		}
		if _, err := b.St.UserByTelegram(ctx, chat); err == nil {
			b.Send(chat, "Аккаунт уже подключён. /balance — баланс, /plans — копилки.")
			return
		}
		b.Send(chat, "Это бот AutoInvest. Чтобы получать уведомления о пополнениях, откройте Профиль → Уведомления на сайте и нажмите «Подключить Telegram».\n\n"+b.PublicURL+"/app/profile/notifications")
	case "/balance":
		u, ok := b.user(ctx, chat)
		if !ok {
			return
		}
		plans, _ := b.St.Plans(ctx, u.ID, false)
		var all []store.Position
		for _, p := range plans {
			all = append(all, p.Positions...)
		}
		v := b.En.Value(all)
		sign := "+"
		if v.PnLKop < 0 {
			sign = ""
		}
		b.Send(chat, fmt.Sprintf("<b>%s</b>\nВложено: %s\nДоход: %s%s (%s%.2f%%)\nЗа сутки: %s%s",
			money.FormatBYN(v.ValueKop), money.FormatBYN(v.InvestedKop), sign, money.FormatBYN(v.PnLKop), sign, v.PnLPct,
			map[bool]string{true: "+", false: ""}[v.DayPnLKop >= 0], money.FormatBYN(v.DayPnLKop)))
	case "/plans":
		u, ok := b.user(ctx, chat)
		if !ok {
			return
		}
		plans, _ := b.St.Plans(ctx, u.ID, false)
		if len(plans) == 0 {
			b.Send(chat, "Копилок пока нет. Создайте первую: "+b.PublicURL+"/app/plans/new")
			return
		}
		var sb strings.Builder
		for _, p := range plans {
			v := b.En.Value(p.Positions)
			st := "работает"
			if p.Status == "paused" {
				st = "на паузе"
			}
			fmt.Fprintf(&sb, "<b>%s</b> — %s, %s\n", escape(p.Name), money.FormatBYN(v.ValueKop), st)
			if p.NextRunAt != nil && p.Status == "active" {
				fmt.Fprintf(&sb, "Следующее пополнение %s на %s\n", time.Unix(*p.NextRunAt, 0).In(engine.Minsk).Format("02.01 в 15:04"), money.FormatBYN(p.AmountKop))
			}
			sb.WriteString("\n")
		}
		b.Send(chat, sb.String())
	case "/stop":
		if u, err := b.St.UserByTelegram(ctx, chat); err == nil {
			b.St.UpdateUserFields(ctx, u.ID, map[string]any{"telegram_chat_id": nil, "telegram_username": ""})
			b.Send(chat, "Telegram отключён от аккаунта. Подключить снова можно в профиле на сайте.")
		}
	default:
		b.Send(chat, "Команды:\n/balance — баланс и доходность\n/plans — мои копилки\n/stop — отключить уведомления\n\nВопросы — в поддержку: "+b.PublicURL+"/app/support")
	}
}

func (b *Bot) user(ctx context.Context, chat int64) (*store.User, bool) {
	u, err := b.St.UserByTelegram(ctx, chat)
	if err != nil {
		b.Send(chat, "Аккаунт не подключён. Откройте Профиль → Уведомления на сайте и нажмите «Подключить Telegram».")
		return nil, false
	}
	return u, true
}

func (b *Bot) bind(ctx context.Context, chat int64, username, code string) {
	uid, err := b.St.UseTelegramLink(ctx, code)
	if err != nil {
		b.Send(chat, "Ссылка устарела. Нажмите «Подключить Telegram» на сайте ещё раз.")
		return
	}
	// один чат — один аккаунт
	b.St.DB.ExecContext(ctx, `UPDATE users SET telegram_chat_id=NULL WHERE telegram_chat_id=? AND id!=?`, chat, uid)
	if len(username) > 64 {
		username = username[:64]
	}
	b.St.UpdateUserFields(ctx, uid, map[string]any{"telegram_chat_id": chat, "telegram_username": username, "notify_telegram": true})
	b.St.Audit(ctx, uid, uid, "telegram.link", "@"+username, "")
	u, _ := b.St.UserByID(ctx, uid)
	name := ""
	if u != nil {
		name = ", " + escape(u.Name)
	}
	b.Send(chat, "Готово"+name+"! Сюда будут приходить чеки о пополнениях, выводах и ответы поддержки.\n\n/balance — баланс\n/plans — копилки")
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
