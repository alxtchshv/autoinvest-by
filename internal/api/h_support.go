package api

import (
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autoinvest/internal/notify"
	"autoinvest/internal/store"
)

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	list, unread, err := s.St.Notifications(r.Context(), currentUser(r).ID, min(max(qInt(r, "limit", 30), 1), 100))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"notifications": list, "unread": unread})
}

func (s *Server) readNotifications(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID int64 `json:"id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	s.St.MarkNotificationsRead(r.Context(), currentUser(r).ID, in.ID)
	s.listNotifications(w, r)
}

var ticketCategories = map[string]string{
	"payments": "Платежи и карты", "plans": "Копилки и пополнения", "withdraw": "Вывод денег",
	"account": "Аккаунт и вход", "kyc": "Верификация", "other": "Другое",
}

func (s *Server) listTickets(w http.ResponseWriter, r *http.Request) {
	list, err := s.St.Tickets(r.Context(), currentUser(r).ID, "", 100)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"tickets": list, "categories": ticketCategories})
}

func (s *Server) createTicket(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Subject  string `json:"subject"`
		Category string `json:"category"`
		Body     string `json:"body"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Subject, in.Body = strings.TrimSpace(in.Subject), strings.TrimSpace(in.Body)
	if l := len([]rune(in.Subject)); l < 3 || l > 120 {
		writeFieldErr(w, "subject", "Тема от 3 до 120 символов")
		return
	}
	if l := len([]rune(in.Body)); l < 10 || l > 5000 {
		writeFieldErr(w, "body", "Опишите вопрос подробнее (от 10 символов)")
		return
	}
	if _, ok := ticketCategories[in.Category]; !ok {
		in.Category = "other"
	}
	if !s.limiter.Allow("ticket:"+strconv.FormatInt(u.ID, 10), 10, 24*time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Вы создали много обращений за сутки. Дополните уже открытое")
		return
	}
	id, err := s.St.CreateTicket(r.Context(), u.ID, in.Subject, in.Category, in.Body)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.alertSupport(fmt.Sprintf("Новое обращение #%d от %s\n<b>%s</b>\n%s\n\nОтветьте на это сообщение (reply), чтобы ответить клиенту.", id, html.EscapeString(u.Email), html.EscapeString(in.Subject), html.EscapeString(truncate(in.Body, 500))))
	t, _ := s.St.Ticket(r.Context(), u.ID, id)
	writeJSON(w, http.StatusCreated, map[string]any{"ticket": t})
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// alertSupport пересылает событие в чат поддержки в Telegram (если настроен).
func (s *Server) alertSupport(text string) {
	if s.Bot != nil && s.Cfg.SupportChatID != 0 {
		go s.Bot.Send(s.Cfg.SupportChatID, text)
	}
}

func (s *Server) getTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.St.Ticket(r.Context(), currentUser(r).ID, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ticket": t})
}

func (s *Server) replyTicket(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, err := s.St.Ticket(r.Context(), u.ID, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	body := strings.TrimSpace(in.Body)
	if l := len([]rune(body)); l < 1 || l > 5000 {
		writeFieldErr(w, "body", "Напишите сообщение")
		return
	}
	if !s.limiter.Allow("tmsg:"+strconv.FormatInt(u.ID, 10), 60, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много сообщений")
		return
	}
	if err := s.St.AddTicketMessage(r.Context(), t.ID, u.ID, false, body); err != nil {
		s.internal(w, r, err)
		return
	}
	s.alertSupport(fmt.Sprintf("Ответ в обращении #%d от %s\n%s", t.ID, html.EscapeString(u.Email), html.EscapeString(truncate(body, 500))))
	s.getTicket(w, r)
}

func (s *Server) closeTicket(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	t, err := s.St.Ticket(r.Context(), u.ID, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.SetTicketStatus(r.Context(), t.ID, "closed")
	s.getTicket(w, r)
}

// ---------------------------------------------------------------- поддержка: ответы сотрудников

func (s *Server) adminTickets(w http.ResponseWriter, r *http.Request) {
	list, err := s.St.Tickets(r.Context(), 0, r.URL.Query().Get("status"), 200)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"tickets": list, "categories": ticketCategories})
}

func (s *Server) adminTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.St.Ticket(r.Context(), 0, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	u, _ := s.St.UserByID(r.Context(), t.UserID)
	writeJSON(w, 200, map[string]any{"ticket": t, "user": u})
}

func (s *Server) adminTicketReply(w http.ResponseWriter, r *http.Request) {
	staff := currentUser(r)
	t, err := s.St.Ticket(r.Context(), 0, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	var in struct {
		Body string `json:"body"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		writeFieldErr(w, "body", "Напишите ответ")
		return
	}
	if err := s.St.AddTicketMessage(r.Context(), t.ID, staff.ID, true, body); err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), t.UserID, staff.ID, "support.reply", fmt.Sprintf("ticket %d", t.ID), clientIP(r))
	s.N.Send(r.Context(), t.UserID, notify.Message{Kind: "support", Title: "Ответ поддержки: " + t.Subject, Body: truncate(body, 300),
		Link: fmt.Sprintf("/app/support/%d", t.ID), Email: true})
	s.adminTicket(w, r)
}

func (s *Server) adminTicketStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Status != "open" && in.Status != "answered" && in.Status != "closed" {
		writeErr(w, http.StatusBadRequest, "Некорректный статус")
		return
	}
	if _, err := s.St.Ticket(r.Context(), 0, pathID(r)); err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.SetTicketStatus(r.Context(), pathID(r), in.Status)
	s.adminTicket(w, r)
}

var _ = store.ErrNotFound
