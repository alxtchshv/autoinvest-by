package api

// Вход по номеру телефона: одноразовый код (SMS, Telegram Gateway или тестовый режим) и подтверждение номера в профиле.
// Войти по номеру можно только если номер подтверждён кодом: неподтверждённый номер при регистрации мог указать кто угодно.

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"autoinvest/internal/notify"
	"autoinvest/internal/sms"
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

// smsSender — провайдер из настроек (создаётся один раз).
func (s *Server) smsSender() sms.Sender {
	s.smsOnce.Do(func() {
		if s.SMS != nil {
			return
		}
		switch s.Cfg.SMSProvider {
		case "telegram":
			s.SMS = &sms.TelegramGateway{Token: s.Cfg.TGGatewayKey}
		case "http":
			s.SMS = &sms.HTTPSender{URL: s.Cfg.SMSHTTPURL, Token: s.Cfg.SMSHTTPToken}
		case "sandbox":
			s.SMS = sms.NewSandbox(s.Log)
		}
	})
	return s.SMS
}

// sendPhoneCode — общая часть: лимиты, код, отправка. testCode непустой только в тестовом режиме.
func (s *Server) sendPhoneCode(w http.ResponseWriter, r *http.Request, phone, purpose string, uid int64) (testCode string, ok bool) {
	snd := s.smsSender()
	if snd == nil || s.Cfg.SMSProvider == "off" {
		writeErr(w, http.StatusServiceUnavailable, "Вход по телефону пока не подключён")
		return "", false
	}
	if !s.limiter.Allow("sms:"+phone, 3, 10*time.Minute) || !s.limiter.Allow("sms-ip:"+clientIP(r), 10, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много кодов. Подождите 10 минут")
		return "", false
	}
	code := randomDigits(6)
	if err := s.St.CreatePhoneCode(r.Context(), phone, purpose, uid, code, phoneCodeTTL); err != nil {
		s.internal(w, r, err)
		return "", false
	}
	if err := snd.SendCode(r.Context(), phone, code, fmt.Sprintf("AutoInvest: код %s. Никому его не сообщайте.", code)); err != nil {
		s.Log.Error("sms", "provider", snd.Name(), "err", err)
		writeErr(w, http.StatusBadGateway, "Не удалось отправить код. Попробуйте через минуту")
		return "", false
	}
	if snd.Name() == "sandbox" {
		testCode = code
	}
	return testCode, true
}

func (s *Server) phoneFromBody(w http.ResponseWriter, raw string) (string, bool) {
	p := normPhone(raw)
	if !phoneRe.MatchString(p) {
		writeFieldErr(w, "phone", "Номер в формате +375 (29) 123-45-67")
		return "", false
	}
	return p, true
}

// phoneLoginCode — POST /auth/phone/code {phone}
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
	u, err := s.St.UserByPhone(r.Context(), phone)
	if errors.Is(err, store.ErrNotFound) {
		writeFieldErr(w, "phone", "Этот номер не подтверждён ни в одном аккаунте. Войдите по почте и подтвердите номер в профиле")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	test, ok := s.sendPhoneCode(w, r, phone, "login", u.ID)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "via": s.Cfg.SMSProvider, "test_code": test})
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
		writeErr(w, http.StatusForbidden, "Аккаунт заблокирован. Напишите в поддержку: support@autoinvest.by")
		return
	}
	if err := s.setSession(w, r, u, u.TOTPEnabled); err != nil {
		s.internal(w, r, err)
		return
	}
	if u.TOTPEnabled {
		writeJSON(w, 200, map[string]any{"mfa_required": true})
		return
	}
	s.afterLogin(r, u)
	writeJSON(w, 200, s.mePayload(r, u))
}

// phoneVerifyCode — POST /me/phone/code {phone}: код для подтверждения номера в профиле.
func (s *Server) phoneVerifyCode(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
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
	if other, err := s.St.UserByPhone(r.Context(), phone); err == nil && other.ID != u.ID {
		writeFieldErr(w, "phone", store.ErrPhoneTaken.Error())
		return
	}
	test, ok := s.sendPhoneCode(w, r, phone, "verify", u.ID)
	if !ok {
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "via": s.Cfg.SMSProvider, "test_code": test})
}

// phoneVerify — POST /me/phone/verify {phone, code}
func (s *Server) phoneVerify(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
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
	uid, err := s.St.UsePhoneCode(r.Context(), phone, "verify", in.Code)
	if err != nil || uid != u.ID {
		writeFieldErr(w, "code", store.ErrCodeInvalid.Error())
		return
	}
	if err := s.St.SetVerifiedPhone(r.Context(), u.ID, phone); err != nil {
		if errors.Is(err, store.ErrPhoneTaken) {
			writeFieldErr(w, "phone", err.Error())
			return
		}
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "security.phone_verified", phone[:7]+"•••"+phone[len(phone)-2:], clientIP(r))
	s.N.Send(r.Context(), u.ID, notify.Message{Kind: "security", Title: "Номер телефона подтверждён", Body: "Теперь можно входить по номеру и коду."})
	s.identities(w, r)
}
