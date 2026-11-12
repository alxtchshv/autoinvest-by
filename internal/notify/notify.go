// Package notify — уведомления: центр уведомлений в приложении, живые события (SSE), Telegram и e-mail.
package notify

import (
	"context"
	"crypto/tls"
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
	TG    string // свой текст для Telegram (HTML); пусто — заголовок и текст
	Mail  string // свой текст письма; пусто — Body
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
		text := m.TG
		if text == "" {
			text = "<b>" + escapeHTML(m.Title) + "</b>"
			if m.Body != "" {
				text += "\n" + escapeHTML(m.Body)
			}
		}
		// Telegram делает ссылку кликабельной только для настоящего адреса сайта (https), localhost он не открывает
		if m.Link != "" && strings.HasPrefix(n.publicURL, "https://") {
			text += "\n\n<a href=\"" + n.publicURL + m.Link + "\">Открыть в AutoInvest →</a>"
		}
		go func(chat int64) {
			if err := tg.Send(chat, text); err != nil {
				n.log.Warn("telegram send", "err", err)
			}
		}(*u.TelegramChatID)
	}
	if m.Email && u.NotifyEmail && n.mail != nil {
		body := m.Body
		if m.Mail != "" {
			body = m.Mail
		}
		if m.Link != "" {
			body += "\n\n" + n.publicURL + m.Link
		}
		go n.mail.Send(u.Email, m.Title, body)
	}
}

// SupportReply — уведомление об ответе поддержки: номер обращения, тема, вопрос клиента и ответ.
// В Telegram на него можно ответить (reply) — текст добавится в это же обращение.
func SupportReply(t *store.Ticket, answer string) Message {
	question := ""
	for i := len(t.Messages) - 1; i >= 0; i-- {
		if !t.Messages[i].IsStaff {
			question = t.Messages[i].Body
			break
		}
	}
	question = cut(question, 400)
	answer = strings.TrimSpace(answer)
	tg := fmt.Sprintf("<b>Ответ поддержки · обращение №%d</b>\nТема: %s\n", t.ID, escapeHTML(t.Subject))
	if question != "" {
		tg += "\n<i>Ваш вопрос:</i>\n" + escapeHTML(question) + "\n"
	}
	tg += "\n<i>Ответ:</i>\n" + escapeHTML(cut(answer, 3000)) +
		"\n\nЧтобы ответить, нажмите на это сообщение → «Ответить» и напишите текст — он попадёт в это же обращение."
	mail := fmt.Sprintf("Обращение №%d: %s\n\n", t.ID, t.Subject)
	if question != "" {
		mail += "Ваш вопрос:\n" + question + "\n\n"
	}
	mail += "Ответ поддержки:\n" + answer
	return Message{Kind: "support", Title: fmt.Sprintf("Ответ поддержки по обращению №%d", t.ID), Body: cut(answer, 300),
		Link: fmt.Sprintf("/app/support/%d", t.ID), Email: true, TG: tg, Mail: mail}
}

func cut(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return string(r)
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
	var err error
	if m.Port == 465 {
		err = sendImplicitTLS(m.Host, m.Port, a, fromAddr, to, []byte(msg)) // Яндекс, Mail.ru: TLS сразу
	} else {
		err = smtp.SendMail(net.JoinHostPort(m.Host, fmt.Sprint(m.Port)), a, fromAddr, []string{to}, []byte(msg)) // 587: STARTTLS (Gmail)
	}
	if err != nil {
		m.log.Error("smtp", "to", to, "err", err)
	}
	return err
}

// sendImplicitTLS — SMTP поверх TLS с первого байта (порт 465).
func sendImplicitTLS(host string, port int, a smtp.Auth, from, to string, msg []byte) error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 15 * time.Second}, "tcp", net.JoinHostPort(host, fmt.Sprint(port)), &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if a != nil {
		if err := c.Auth(a); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(msg); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func encodeFrom(from string) string {
	i := strings.LastIndex(from, "<")
	if i <= 0 {
		return from
	}
	name := strings.TrimSpace(from[:i])
	return mime.QEncoding.Encode("utf-8", name) + " " + from[i:]
}
