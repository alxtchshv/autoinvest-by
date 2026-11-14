package api

import (
	"context"
	"fmt"
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

var ticketCategories = func() map[string]string {
	m := map[string]string{}
	for _, c := range store.TicketCategories {
		m[c.Key] = c.Label
	}
	return m
}()

// supportUser — обращения всегда от основного аккаунта: с демо-счёта пишет тот же человек.
func (s *Server) supportUser(r *http.Request) *store.User {
	u := currentUser(r)
	if u != nil && u.DemoOf != nil {
		if real, err := s.St.UserByID(r.Context(), *u.DemoOf); err == nil {
			return real
		}
	}
	return u
}

func (s *Server) listTickets(w http.ResponseWriter, r *http.Request) {
	list, err := s.St.Tickets(r.Context(), s.supportUser(r).ID, "", 100)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"tickets": list, "categories": ticketCategories})
}

func (s *Server) createTicket(w http.ResponseWriter, r *http.Request) {
	u := s.supportUser(r)
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
	s.alertSupport(r.Context(), id, "new", fmt.Sprintf("Новое обращение #%d: %s", id, in.Subject), u.Email+": "+truncate(in.Body, 160), in.Body)
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

// alertSupport — событие по обращению: подробная карточка в группу поддержки в Telegram (тема, клиент, история, кнопки)
// и уведомление в колокольчик всем сотрудникам (админ и поддержка) со ссылкой на обращение в админке.
func (s *Server) alertSupport(ctx context.Context, ticketID int64, event, title, short, full string) {
	if s.Bot != nil {
		go s.Bot.SupportAlert(context.Background(), ticketID, event, full, "")
	}
	s.NotifyStaff(ctx, ticketID, title, short)
}

// GET /admin/users/{id}/support — вся переписка клиента с поддержкой (для карточки пользователя в админке).
func (s *Server) adminUserSupport(w http.ResponseWriter, r *http.Request) {
	list, err := s.St.SupportHistory(r.Context(), pathID(r), 100)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"tickets": list})
}

// NotifyStaff — уведомление сотрудникам об обращении (его же вызывает бот, когда клиент пишет в поддержку из Telegram).
func (s *Server) NotifyStaff(ctx context.Context, ticketID int64, title, body string) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM users WHERE role IN ('admin','support') AND status='active'`)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		s.N.Send(ctx, id, notify.Message{Kind: "support", Title: title, Body: body, Link: fmt.Sprintf("/admin/tickets/%d", ticketID)})
	}
}

func (s *Server) getTicket(w http.ResponseWriter, r *http.Request) {
	t, err := s.St.Ticket(r.Context(), s.supportUser(r).ID, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ticket": t})
}

func (s *Server) replyTicket(w http.ResponseWriter, r *http.Request) {
	u := s.supportUser(r)
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
	s.alertSupport(r.Context(), t.ID, "reply", fmt.Sprintf("Ответ клиента по обращению #%d", t.ID), u.Email+": "+truncate(body, 160), body)
	s.getTicket(w, r)
}

func (s *Server) closeTicket(w http.ResponseWriter, r *http.Request) {
	u := s.supportUser(r)
	t, err := s.St.Ticket(r.Context(), u.ID, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if t.Status != "closed" {
		s.St.CloseTicket(r.Context(), t.ID, "client")
		s.alertSupport(r.Context(), t.ID, "closed_client", fmt.Sprintf("Клиент закрыл обращение #%d", t.ID), u.Email+": "+t.Subject, "")
	}
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
	// вся переписка этого клиента с поддержкой: остальные обращения со всеми сообщениями
	history, _ := s.St.SupportHistory(r.Context(), t.UserID, 50)
	plans, _ := s.St.CountOpenPlans(r.Context(), t.UserID)
	writeJSON(w, 200, map[string]any{"ticket": t, "user": u, "history": history, "plans": plans})
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
	if full, err := s.St.Ticket(r.Context(), 0, t.ID); err == nil {
		t = full
	}
	s.N.Send(r.Context(), t.UserID, notify.SupportReply(t, body))
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
	t, err := s.St.Ticket(r.Context(), 0, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if in.Status == "closed" && t.Status != "closed" {
		// закрыл сотрудник — человек узнаёт (кабинет, Telegram с кнопкой «Ответить»), в группе — отметка
		staff := currentUser(r)
		s.St.CloseTicket(r.Context(), t.ID, "staff")
		s.N.Send(r.Context(), t.UserID, notify.SupportClosed(t))
		if s.Bot != nil {
			go s.Bot.SupportAlert(context.Background(), t.ID, "closed_staff", "", staff.Name)
		}
	} else {
		s.St.SetTicketStatus(r.Context(), t.ID, in.Status)
	}
	s.adminTicket(w, r)
}

var _ = store.ErrNotFound
