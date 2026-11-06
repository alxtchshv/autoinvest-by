// Package notify — уведомления: центр уведомлений в приложении, живые события (SSE), Telegram и e-mail.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"autoinvest/internal/store"
)

// Messenger — отправка сообщений в Telegram (реализует пакет telegram).
type Messenger interface {
	Send(chatID int64, text string) error
}

// UserEvent — событие для открытых вкладок пользователя.
type UserEvent struct {
	Type string `json:"type"` // notification | plans
	Data any    `json:"data"`
}

type Notifier struct {
	st        *store.Store
	log       *slog.Logger
	mail      *Mailer
	publicURL string

	mu   sync.RWMutex
	tg   Messenger
	subs map[int64]map[chan UserEvent]struct{}
}

func New(st *store.Store, mail *Mailer, publicURL string, log *slog.Logger) *Notifier {
	return &Notifier{st: st, mail: mail, log: log, publicURL: publicURL, subs: map[int64]map[chan UserEvent]struct{}{}}
}

func (n *Notifier) SetMessenger(m Messenger) {
	n.mu.Lock()
	n.tg = m
	n.mu.Unlock()
}

func (n *Notifier) Mailer() *Mailer { return n.mail }

// Message — что отправить.
type Message struct {
	Kind  string // purchase | purchase_failed | payout | kyc | support | security | billing | referral | market
	Title string
	Body  string
	Link  string // путь в приложении, например /app/plans/3
	Email bool   // дублировать письмом
}

// Send сохраняет уведомление и рассылает его по включённым каналам. Ошибки каналов не прерывают работу.
func (n *Notifier) Send(ctx context.Context, userID int64, m Message) {
	id, err := n.st.AddNotification(ctx, userID, store.Notification{Kind: m.Kind, Title: m.Title, Body: m.Body, Link: m.Link})
	if err != nil {
		n.log.Error("notification save", "err", err)
	}
	n.Publish(userID, UserEvent{Type: "notification", Data: store.Notification{ID: id, Kind: m.Kind, Title: m.Title, Body: m.Body, Link: m.Link, CreatedAt: store.Now()}})
	u, err := n.st.UserByID(ctx, userID)
	if err != nil {
		return
	}
	if m.Kind == "market" && !u.NotifyMarket {
		return
	}
	n.mu.RLock()
	tg := n.tg
	n.mu.RUnlock()
	if tg != nil && u.TelegramChatID != nil && u.NotifyTelegram {
		text := "<b>" + escapeHTML(m.Title) + "</b>"
		if m.Body != "" {
			text += "\n" + escapeHTML(m.Body)
		}
		if m.Link != "" {
			text += "\n<a href=\"" + n.publicURL + m.Link + "\">Открыть в AutoInvest</a>"
		}
		go func(chat int64) {
			if err := tg.Send(chat, text); err != nil {
				n.log.Warn("telegram send", "err", err)
			}
		}(*u.TelegramChatID)
	}
	if m.Email && u.NotifyEmail && n.mail != nil {
		body := m.Body
		if m.Link != "" {
			body += "\n\n" + n.publicURL + m.Link
		}
		go n.mail.Send(u.Email, m.Title, body)
	}
}

func escapeHTML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// Subscribe — поток событий пользователя для SSE.
func (n *Notifier) Subscribe(userID int64) (chan UserEvent, func()) {
	ch := make(chan UserEvent, 8)
	n.mu.Lock()
	if n.subs[userID] == nil {
		n.subs[userID] = map[chan UserEvent]struct{}{}
	}
	n.subs[userID][ch] = struct{}{}
	n.mu.Unlock()
	return ch, func() {
		n.mu.Lock()
		delete(n.subs[userID], ch)
		if len(n.subs[userID]) == 0 {
			delete(n.subs, userID)
		}
		n.mu.Unlock()
	}
}

func (n *Notifier) Publish(userID int64, ev UserEvent) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for ch := range n.subs[userID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// ---------------------------------------------------------------- e-mail

type Mailer struct {
	Host, User, Pass, From string
	Port                   int
	log                    *slog.Logger
}

func NewMailer(host string, port int, user, pass, from string, log *slog.Logger) *Mailer {
	return &Mailer{Host: host, Port: port, User: user, Pass: pass, From: from, log: log}
}

func (m *Mailer) Enabled() bool { return m != nil && m.Host != "" }

// Send отправляет письмо. Без SMTP письмо пишется в лог — удобно при локальном запуске.
func (m *Mailer) Send(to, subject, body string) error {
	if !m.Enabled() {
		m.log.Info("e-mail (SMTP не настроен, письмо выведено в лог)", "to", to, "subject", subject, "body", body)
		return nil
	}
	fromAddr := m.From
	if i := strings.LastIndex(fromAddr, "<"); i >= 0 {
		fromAddr = strings.Trim(fromAddr[i:], "<>")
	}
	headers := []string{
		"From: " + encodeFrom(m.From),
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=utf-8",
		"Content-Transfer-Encoding: 8bit",
		"Date: " + time.Now().Format(time.RFC1123Z),
	}
	msg := strings.Join(headers, "\r\n") + "\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n")
	var a smtp.Auth
	if m.User != "" {
		a = smtp.PlainAuth("", m.User, m.Pass, m.Host)
	}
	err := smtp.SendMail(net.JoinHostPort(m.Host, fmt.Sprint(m.Port)), a, fromAddr, []string{to}, []byte(msg))
	if err != nil {
		m.log.Error("smtp", "to", to, "err", err)
	}
	return err
}

func encodeFrom(from string) string {
	i := strings.LastIndex(from, "<")
	if i <= 0 {
		return from
	}
	name := strings.TrimSpace(from[:i])
	return mime.QEncoding.Encode("utf-8", name) + " " + from[i:]
}
