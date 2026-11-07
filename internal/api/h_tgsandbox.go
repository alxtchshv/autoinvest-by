package api

// Окно тестового Telegram в кабинете: пользователь «пишет» боту и нажимает кнопки, бот отвечает как в настоящем Telegram.
// Работает только с встроенной имитацией (TELEGRAM_SANDBOX), у каждого пользователя свой чат.

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const tgSandboxChatBase = 9_000_000_000

func (s *Server) tgChat(r *http.Request) (int64, string) {
	u := currentUser(r)
	name := strings.ToLower(strings.Fields(u.Name + " user")[0])
	return tgSandboxChatBase + u.ID, "test_" + strconv.FormatInt(u.ID, 10) + "_" + name
}

func (s *Server) tgSandboxOff(w http.ResponseWriter) bool {
	if s.TG == nil {
		writeErr(w, http.StatusNotFound, "Тестовый Telegram выключен — подключён настоящий бот")
		return true
	}
	return false
}

// GET /telegram/sandbox?after=<rev>&wait=1 — сообщения чата; с wait ждёт новых до 20 секунд.
func (s *Server) tgSandboxMessages(w http.ResponseWriter, r *http.Request) {
	if s.tgSandboxOff(w) {
		return
	}
	chat, _ := s.tgChat(r)
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	msgs, rev := s.TG.Messages(chat, after)
	if len(msgs) == 0 && r.URL.Query().Get("wait") == "1" {
		s.TG.Wait(r.Context(), 20*time.Second)
		msgs, rev = s.TG.Messages(chat, after)
	}
	writeJSON(w, 200, map[string]any{"bot": s.TG.Username, "messages": msgs, "rev": rev, "keyboard": s.TG.Keyboard(chat)})
}

func (s *Server) tgSandboxSend(w http.ResponseWriter, r *http.Request) {
	if s.tgSandboxOff(w) {
		return
	}
	var in struct {
		Text string `json:"text"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	text := strings.TrimSpace(in.Text)
	if text == "" || len([]rune(text)) > 4000 {
		writeFieldErr(w, "text", "Сообщение от 1 до 4000 символов")
		return
	}
	chat, name := s.tgChat(r)
	s.TG.UserSays(chat, name, text)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) tgSandboxPress(w http.ResponseWriter, r *http.Request) {
	if s.tgSandboxOff(w) {
		return
	}
	var in struct {
		MessageID int64  `json:"message_id"`
		Data      string `json:"data"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	chat, name := s.tgChat(r)
	if !s.TG.UserPress(chat, in.MessageID, in.Data, name) {
		writeErr(w, http.StatusBadRequest, "Такой кнопки нет")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// POST /telegram/sandbox/contact {phone} — «Поделиться номером» в тестовом Telegram.
func (s *Server) tgSandboxContact(w http.ResponseWriter, r *http.Request) {
	if s.tgSandboxOff(w) {
		return
	}
	var in struct {
		Phone string `json:"phone"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	p := normPhone(in.Phone)
	if !intlPhoneRe.MatchString(p) {
		writeFieldErr(w, "phone", "Номер в формате +375 29 123-45-67")
		return
	}
	chat, name := s.tgChat(r)
	if !s.TG.UserSharesContact(chat, name, p) {
		writeErr(w, http.StatusBadRequest, "Бот не просил номер — отправьте /phone")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
