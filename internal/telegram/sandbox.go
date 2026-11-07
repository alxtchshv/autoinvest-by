package telegram

// Sandbox — встроенная имитация Telegram Bot API для стенда и разработки (TELEGRAM_SANDBOX, без токена бота).
// Бот работает с ней ровно так же, как с api.telegram.org (getUpdates, sendMessage, editMessageText…),
// а окно чата в кабинете (/telegram-sandbox) пишет сообщения и нажимает кнопки через UserSays/UserPress.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

type SandboxButton struct {
	Text    string `json:"text"`
	Data    string `json:"data,omitempty"`
	URL     string `json:"url,omitempty"`
	Contact bool   `json:"contact,omitempty"` // кнопка нижней клавиатуры «Поделиться номером»
}

type SandboxMsg struct {
	ID      int64             `json:"id"`
	Rev     int64             `json:"rev"`
	FromBot bool              `json:"from_bot"`
	Text    string            `json:"text"`
	HTML    bool              `json:"html"`
	Buttons [][]SandboxButton `json:"buttons,omitempty"`
	At      int64             `json:"at"`
}

type Sandbox struct {
	Username string

	mu      sync.Mutex
	queue   []json.RawMessage // необработанные апдейты
	updID   int64
	msgID   int64
	rev     int64
	chats   map[int64][]*SandboxMsg
	replyKB map[int64][]SandboxButton // нижняя клавиатура чата (reply keyboard)
	changed chan struct{}             // закрывается и пересоздаётся при каждом новом апдейте
}

func NewSandbox(username string) *Sandbox {
	return &Sandbox{Username: username, chats: map[int64][]*SandboxMsg{}, replyKB: map[int64][]SandboxButton{}, changed: make(chan struct{})}
}

// Transport — http.RoundTripper, через который бот «ходит» в песочницу, не выходя в сеть.
func (s *Sandbox) Transport() http.RoundTripper { return sbTransport{s} }

type sbTransport struct{ s *Sandbox }

func (t sbTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.s.ServeHTTP(rec, r)
	return rec.Result(), nil
}

func (s *Sandbox) notify() {
	close(s.changed)
	s.changed = make(chan struct{})
}

func (s *Sandbox) addMsg(chat int64, fromBot bool, text string, html bool, kb [][]SandboxButton) *SandboxMsg {
	s.msgID++
	s.rev++
	m := &SandboxMsg{ID: s.msgID, Rev: s.rev, FromBot: fromBot, Text: text, HTML: html, Buttons: kb, At: time.Now().Unix()}
	s.chats[chat] = append(s.chats[chat], m)
	if len(s.chats[chat]) > 300 {
		s.chats[chat] = s.chats[chat][len(s.chats[chat])-300:]
	}
	return m
}

// UserSays — пользователь написал боту.
func (s *Sandbox) UserSays(chat int64, username, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.addMsg(chat, false, text, false, nil)
	s.updID++
	upd, _ := json.Marshal(map[string]any{"update_id": s.updID, "message": map[string]any{
		"message_id": m.ID, "text": text, "chat": map[string]any{"id": chat, "type": "private"},
		"from": map[string]any{"id": chat, "username": username, "first_name": username}}})
	s.queue = append(s.queue, upd)
	s.notify()
}

// UserReplies — пользователь ответил (reply) на сообщение бота с номером replyTo.
func (s *Sandbox) UserReplies(chat int64, username, text string, replyTo int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var orig *SandboxMsg
	for _, m := range s.chats[chat] {
		if m.ID == replyTo {
			orig = m
		}
	}
	if orig == nil {
		return false
	}
	m := s.addMsg(chat, false, text, false, nil)
	s.updID++
	upd, _ := json.Marshal(map[string]any{"update_id": s.updID, "message": map[string]any{
		"message_id": m.ID, "text": text, "chat": map[string]any{"id": chat, "type": "private"},
		"from":             map[string]any{"id": chat, "username": username},
		"reply_to_message": map[string]any{"message_id": orig.ID, "text": plainText(orig.Text), "chat": map[string]any{"id": chat, "type": "private"}}}})
	s.queue = append(s.queue, upd)
	s.notify()
	return true
}

// UserPress — пользователь нажал кнопку под сообщением бота.
func (s *Sandbox) UserPress(chat, msgID int64, data, username string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found *SandboxMsg
	for _, m := range s.chats[chat] {
		if m.ID == msgID && m.FromBot {
			found = m
		}
	}
	if found == nil {
		return false
	}
	ok := false
	for _, row := range found.Buttons {
		for _, b := range row {
			ok = ok || (b.Data == data && data != "")
		}
	}
	if !ok {
		return false
	}
	s.updID++
	upd, _ := json.Marshal(map[string]any{"update_id": s.updID, "callback_query": map[string]any{
		"id": "cb" + itoa(s.updID), "data": data, "from": map[string]any{"id": chat, "username": username},
		"message": map[string]any{"message_id": msgID, "chat": map[string]any{"id": chat, "type": "private"}}}})
	s.queue = append(s.queue, upd)
	s.notify()
	return true
}

// Keyboard — текущая нижняя клавиатура чата.
func (s *Sandbox) Keyboard(chat int64) []SandboxButton {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.replyKB[chat]
}

// UserSharesContact — пользователь нажал «Поделиться номером» (работает, только если бот показал такую кнопку).
func (s *Sandbox) UserSharesContact(chat int64, username, phone string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ok := false
	for _, b := range s.replyKB[chat] {
		ok = ok || b.Contact
	}
	if !ok {
		return false
	}
	m := s.addMsg(chat, false, "📱 "+phone, false, nil)
	delete(s.replyKB, chat)
	s.updID++
	upd, _ := json.Marshal(map[string]any{"update_id": s.updID, "message": map[string]any{
		"message_id": m.ID, "chat": map[string]any{"id": chat, "type": "private"},
		"from":    map[string]any{"id": chat, "username": username},
		"contact": map[string]any{"phone_number": phone, "user_id": chat}}})
	s.queue = append(s.queue, upd)
	s.notify()
	return true
}

// Messages — сообщения чата, изменившиеся после ревизии after (новые и отредактированные).
func (s *Sandbox) Messages(chat, after int64) ([]SandboxMsg, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []SandboxMsg{}
	for _, m := range s.chats[chat] {
		if m.Rev > after {
			out = append(out, *m)
		}
	}
	return out, s.rev
}

// Wait ждёт изменений (для длинного опроса из окна чата) не дольше d.
func (s *Sandbox) Wait(ctx context.Context, d time.Duration) {
	s.mu.Lock()
	ch := s.changed
	s.mu.Unlock()
	select {
	case <-ch:
	case <-time.After(d):
	case <-ctx.Done():
	}
}

// ServeHTTP — сам Bot API: /bot<token>/<method>.
func (s *Sandbox) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var in struct {
		Offset      int64           `json:"offset"`
		Timeout     int             `json:"timeout"`
		ChatID      int64           `json:"chat_id"`
		MessageID   int64           `json:"message_id"`
		Text        string          `json:"text"`
		ParseMode   string          `json:"parse_mode"`
		ReplyMarkup json.RawMessage `json:"reply_markup"`
	}
	json.Unmarshal(body, &in)
	reply := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": v})
	}
	switch method {
	case "getMe":
		reply(map[string]any{"id": 1, "is_bot": true, "username": s.Username, "first_name": "AutoInvest"})
	case "setMyCommands", "answerCallbackQuery", "setChatMenuButton":
		reply(true)
	case "getUpdates":
		deadline := time.Now().Add(time.Duration(min(max(in.Timeout, 0), 25)) * time.Second)
		for {
			s.mu.Lock()
			var out []json.RawMessage
			keep := s.queue[:0]
			for _, u := range s.queue {
				var id struct {
					UpdateID int64 `json:"update_id"`
				}
				json.Unmarshal(u, &id)
				if id.UpdateID >= in.Offset {
					keep = append(keep, u)
					out = append(out, u)
				}
			}
			s.queue = keep
			ch := s.changed
			s.mu.Unlock()
			if len(out) > 0 || time.Now().After(deadline) {
				if out == nil {
					out = []json.RawMessage{}
				}
				reply(out)
				return
			}
			select {
			case <-ch:
			case <-time.After(time.Until(deadline)):
			case <-r.Context().Done():
				reply([]json.RawMessage{})
				return
			}
		}
	case "sendMessage":
		s.mu.Lock()
		m := s.addMsg(in.ChatID, true, in.Text, in.ParseMode == "HTML", parseKeyboard(in.ReplyMarkup))
		if kb, remove := parseReplyKeyboard(in.ReplyMarkup); remove {
			delete(s.replyKB, in.ChatID)
		} else if kb != nil {
			s.replyKB[in.ChatID] = kb
		}
		s.notify()
		s.mu.Unlock()
		reply(map[string]any{"message_id": m.ID, "chat": map[string]any{"id": in.ChatID}, "text": in.Text})
	case "editMessageText":
		s.mu.Lock()
		for _, m := range s.chats[in.ChatID] {
			if m.ID == in.MessageID && m.FromBot {
				s.rev++
				m.Rev, m.Text, m.HTML, m.Buttons = s.rev, in.Text, in.ParseMode == "HTML", parseKeyboard(in.ReplyMarkup)
			}
		}
		s.notify()
		s.mu.Unlock()
		reply(map[string]any{"message_id": in.MessageID})
	default:
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":false,"description":"метод не поддерживается тестовым Telegram"}`))
	}
}

func parseKeyboard(raw json.RawMessage) [][]SandboxButton {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	var kb struct {
		Inline [][]struct {
			Text string `json:"text"`
			Data string `json:"callback_data"`
			URL  string `json:"url"`
		} `json:"inline_keyboard"`
	}
	if json.Unmarshal(raw, &kb) != nil {
		return nil
	}
	var out [][]SandboxButton
	for _, row := range kb.Inline {
		var r []SandboxButton
		for _, b := range row {
			r = append(r, SandboxButton{Text: b.Text, Data: b.Data, URL: b.URL})
		}
		out = append(out, r)
	}
	return out
}

// parseReplyKeyboard — нижняя клавиатура (keyboard) или команда её убрать (remove_keyboard).
func parseReplyKeyboard(raw json.RawMessage) ([]SandboxButton, bool) {
	var kb struct {
		Keyboard [][]struct {
			Text    string `json:"text"`
			Contact bool   `json:"request_contact"`
		} `json:"keyboard"`
		Remove bool `json:"remove_keyboard"`
	}
	if len(bytes.TrimSpace(raw)) == 0 || json.Unmarshal(raw, &kb) != nil {
		return nil, false
	}
	if kb.Remove {
		return nil, true
	}
	var out []SandboxButton
	for _, row := range kb.Keyboard {
		for _, b := range row {
			out = append(out, SandboxButton{Text: b.Text, Contact: b.Contact})
		}
	}
	return out, false
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
