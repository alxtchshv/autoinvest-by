package api

// Телефон через Telegram (без SMS и без оплаты за сообщения):
//   - номер подтверждается кнопкой «Поделиться номером» в нашем боте: Telegram передаёт номер, к которому привязан аккаунт;
//   - код для входа по номеру бот присылает в этот же чат.
// Поэтому сначала нужно подключить Telegram, потом — телефон.

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"autoinvest/internal/store"
)

const phoneCodeTTL = 10 * 60

func randomDigits(n int) string {
	b := make([]byte, n)
	for i := range b {
		d, _ := rand.Int(rand.Reader, big.NewInt(10))
		b[i] = byte('0' + d.Int64())
	}
	return string(b)
}

// phoneMode — "telegram", если бот работает (настоящий или тестовый), иначе "off".
func (s *Server) phoneMode() string {
	if s.Bot == nil {
		return "off"
	}
	return "telegram"
}

func (s *Server) phoneFromBody(w http.ResponseWriter, raw string) (string, bool) {
	p := normPhone(raw)
	if !phoneRe.MatchString(p) && !intlPhoneRe.MatchString(p) {
		writeFieldErr(w, "phone", "Номер в формате +375 (29) 123-45-67")
		return "", false
	}
	return p, true
}

// phoneLoginCode — POST /auth/phone/code {phone}: код приходит в Telegram, привязанный к аккаунту с этим номером.
func (s *Server) phoneLoginCode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone string `json:"phone"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	phone, ok := s.phoneFromBody(w, in.Phone)
	if !ok {
		return
	}
	if s.Bot == nil {
		writeErr(w, http.StatusServiceUnavailable, "Вход по телефону пока не подключён")
		return
	}
	// проверки номеров с одного адреса — с запасом на опечатки, но без перебора базы
	if !s.limiter.Allow("tglookup-ip:"+clientIP(r), 15, 10*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много попыток. Подождите 10 минут")
		return
	}
	u, err := s.St.UserByPhone(r.Context(), phone)
	if errors.Is(err, store.ErrNotFound) {
		writeFieldErr(w, "phone", "Этот номер ещё не подключён ко входу. Войдите по почте или через Google, затем Профиль → Безопасность → «Телефон и код в Telegram» → «Подтвердить»: бот попросит поделиться номером")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if u.TelegramChatID == nil {
		writeFieldErr(w, "phone", "К аккаунту не подключён Telegram — войдите по почте или через Google")
		return
	}
	// лимит — только на настоящие отправки кода, а не на опечатки в номере
	if !s.limiter.Allow("tgcode:"+phone, 3, 10*time.Minute) || !s.limiter.Allow("tgcode-ip:"+clientIP(r), 10, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Код уже отправлен несколько раз — проверьте чат с ботом в Telegram или подождите 10 минут")
		return
	}
	code := randomDigits(6)
	if err := s.St.CreatePhoneCode(r.Context(), phone, "login", u.ID, code, phoneCodeTTL); err != nil {
		s.internal(w, r, err)
		return
	}
	text := fmt.Sprintf("Код для входа в AutoInvest: <b>%s</b>\nДействует 10 минут. Никому его не сообщайте.\nЕсли это не вы — просто проигнорируйте сообщение.", code)
	if err := s.Bot.Send(*u.TelegramChatID, text); err != nil {
		s.Log.Error("telegram: код входа", "err", err)
		writeErr(w, http.StatusBadGateway, "Не удалось отправить код в Telegram. Попробуйте через минуту")
		return
	}
	resp := map[string]any{"ok": true, "via": "telegram"}
	if s.TG != nil {
		resp["test_code"] = code // тестовый Telegram: дублируем код на экране
	}
	writeJSON(w, 200, resp)
}

// phoneLogin — POST /auth/phone/login {phone, code}
func (s *Server) phoneLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	phone, ok := s.phoneFromBody(w, in.Phone)
	if !ok {
		return
	}
	if !s.limiter.Allow("login:"+clientIP(r), 20, 15*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много попыток входа. Подождите 15 минут")
		return
	}
	uid, err := s.St.UsePhoneCode(r.Context(), phone, "login", in.Code)
	if err != nil {
		writeFieldErr(w, "code", err.Error())
		return
	}
	u, err := s.St.UserByID(r.Context(), uid)
	if err != nil || u.Phone != phone || !u.PhoneVerified {
		writeFieldErr(w, "code", "Номер больше не привязан к аккаунту")
		return
	}
	if u.Status == "blocked" {
		writeErr(w, http.StatusForbidden, blockedMessage(u))
		return
	}
	s.finishLogin(w, r, u)
}

// phoneViaTelegram — POST /me/phone/telegram: бот присылает кнопку «Поделиться номером».
func (s *Server) phoneViaTelegram(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if s.Bot == nil {
		writeErr(w, http.StatusServiceUnavailable, "Telegram-бот ещё не подключён")
		return
	}
	if u.TelegramChatID == nil {
		writeJSON(w, http.StatusConflict, apiErr{Error: "Сначала подключите Telegram: Профиль → Уведомления", Code: "telegram_required"})
		return
	}
	if err := s.Bot.AskPhone(*u.TelegramChatID); err != nil {
		s.Log.Error("telegram: запрос номера", "err", err)
		writeErr(w, http.StatusBadGateway, "Не удалось написать в Telegram. Попробуйте через минуту")
		return
	}
	resp := map[string]any{"ok": true, "bot": s.Bot.Username}
	if s.TG != nil {
		resp["open"] = "/telegram-sandbox"
	} else {
		resp["open"] = "https://t.me/" + s.Bot.Username
	}
	writeJSON(w, 200, resp)
}

// unlinkPhone — DELETE /me/phone: отвязать номер (если остаётся другой способ входа).
func (s *Server) unlinkPhone(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	list, _ := s.St.Identities(r.Context(), u.ID)
	if !u.HasPassword && len(list) == 0 {
		writeErr(w, http.StatusConflict, "Это единственный способ входа. Сначала задайте пароль")
		return
	}
	s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"phone": "", "phone_verified_at": nil})
	s.St.Audit(r.Context(), u.ID, u.ID, "security.phone_removed", "", clientIP(r))
	s.identities(w, r)
}
