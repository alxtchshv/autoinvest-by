package api

// Вторая защита: код из Google Authenticator (TOTP) и ключи входа — Face ID, Touch ID, отпечаток, Windows Hello (WebAuthn).
// WebAuthn — встроенная функция браузеров и телефонов, платить за неё не нужно; биометрия не покидает устройство,
// сервер хранит только открытый ключ.
//
// Где спрашиваем: при входе по паролю или коду из Telegram (если защита включена) и перед опасными действиями
// (вывод денег, закрытие копилки, смена пароля, удаление аккаунта). Вход через Google второй защиты не требует:
// у Google своя проверка, а опасные действия всё равно подтверждаются.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"autoinvest/internal/auth"
	"autoinvest/internal/store"
)

const stepUpTTL = 5 * 60 // подтверждение ключом действует 5 минут

type waUser struct {
	u     *store.User
	creds []webauthn.Credential
}

func (w waUser) WebAuthnID() []byte {
	return []byte("autoinvest-user-" + strconv.FormatInt(w.u.ID, 10))
}
func (w waUser) WebAuthnName() string                       { return w.u.Email }
func (w waUser) WebAuthnDisplayName() string                { return w.u.Name }
func (w waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }

func (s *Server) webAuthn() (*webauthn.WebAuthn, error) {
	pu, err := url.Parse(s.Cfg.PublicURL)
	if err != nil {
		return nil, err
	}
	return webauthn.New(&webauthn.Config{RPID: pu.Hostname(), RPDisplayName: "AutoInvest", RPOrigins: []string{strings.TrimRight(s.Cfg.PublicURL, "/")}})
}

func (s *Server) loadWAUser(r *http.Request, u *store.User) (waUser, []store.Passkey) {
	list, _ := s.St.Passkeys(r.Context(), u.ID)
	wu := waUser{u: u}
	for _, p := range list {
		var c webauthn.Credential
		if json.Unmarshal([]byte(p.Data), &c) == nil {
			wu.creds = append(wu.creds, c)
		}
	}
	return wu, list
}

func (s *Server) needsSecondFactor(r *http.Request, u *store.User) bool {
	return u.TOTPEnabled || s.St.CountPasskeys(r.Context(), u.ID) > 0
}

// mfaMethods — чем можно подтвердить вход. Face ID / отпечаток заменяет код: если на этом устройстве ключа нет,
// а Google Authenticator не включён, код приходит в Telegram или на почту.
func (s *Server) mfaMethods(r *http.Request, u *store.User) map[string]bool {
	pk := s.St.CountPasskeys(r.Context(), u.ID) > 0
	fallback := pk && !u.TOTPEnabled
	return map[string]bool{"totp": u.TOTPEnabled, "passkey": pk,
		"telegram": fallback && s.Bot != nil && u.TelegramChatID != nil, "email": fallback}
}

// loginSendCode — POST /auth/login/code {via: telegram|email}: запасной код второго шага, если Face ID на устройстве нет.
func (s *Server) loginSendCode(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	if sess == nil || !sess.MFAPending {
		writeErr(w, http.StatusUnauthorized, "Сессия входа истекла, войдите ещё раз")
		return
	}
	var in struct {
		Via string `json:"via"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	u, err := s.St.UserByID(r.Context(), sess.UserID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	m := s.mfaMethods(r, u)
	if !m[in.Via] || (in.Via != "telegram" && in.Via != "email") {
		writeErr(w, http.StatusBadRequest, "Этот способ недоступен")
		return
	}
	if !s.limiter.Allow("mfa-code:"+strconv.FormatInt(u.ID, 10), 3, 10*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много кодов. Подождите 10 минут")
		return
	}
	code := randomDigits(6)
	if err := s.St.CreatePhoneCode(r.Context(), "mfa:"+strconv.FormatInt(u.ID, 10), "login2fa", u.ID, code, phoneCodeTTL); err != nil {
		s.internal(w, r, err)
		return
	}
	resp := map[string]any{"ok": true, "via": in.Via}
	if in.Via == "telegram" {
		if err := s.Bot.Send(*u.TelegramChatID, "Код для входа в AutoInvest: <b>"+code+"</b>\nДействует 10 минут. Никому его не сообщайте."); err != nil {
			writeErr(w, http.StatusBadGateway, "Не удалось отправить код в Telegram")
			return
		}
		if s.TG != nil {
			resp["test_code"] = code
		}
	} else {
		mailer := s.N.Mailer()
		if mailer.Enabled() {
			go mailer.Send(u.Email, "Код для входа в AutoInvest: "+code, "Код для входа в AutoInvest: "+code+"\n\nДействует 10 минут. Если это не вы — смените пароль.")
		} else if s.Cfg.Env == "production" {
			writeErr(w, http.StatusServiceUnavailable, "Почта не настроена — используйте Telegram")
			return
		} else {
			resp["test_code"] = code
		}
	}
	writeJSON(w, 200, resp)
}

func (s *Server) saveWASession(r *http.Request, kind string, sd *webauthn.SessionData) {
	raw, _ := json.Marshal(sd)
	s.St.SetKV(r.Context(), "wa:"+kind+":"+currentSession(r).ID, string(raw))
}

func (s *Server) takeWASession(r *http.Request, kind string) (*webauthn.SessionData, bool) {
	key := "wa:" + kind + ":" + currentSession(r).ID
	raw := s.St.GetKV(r.Context(), key)
	s.St.SetKV(r.Context(), key, "")
	var sd webauthn.SessionData
	if raw == "" || json.Unmarshal([]byte(raw), &sd) != nil || time.Now().After(sd.Expires) && !sd.Expires.IsZero() {
		return nil, false
	}
	return &sd, true
}

// ---------------------------------------------------------------- регистрация ключа

// POST /me/passkeys/begin — параметры для navigator.credentials.create
func (s *Server) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	wa, err := s.webAuthn()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	wu, _ := s.loadWAUser(r, currentUser(r))
	var excl []protocol.CredentialDescriptor
	for _, c := range wu.creds {
		excl = append(excl, c.Descriptor())
	}
	opts, sd, err := wa.BeginRegistration(wu,
		webauthn.WithExclusions(excl),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementPreferred, UserVerification: protocol.VerificationRequired}))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.saveWASession(r, "reg", sd)
	writeJSON(w, 200, opts)
}

// POST /me/passkeys/finish?name=… — ответ браузера после Face ID / отпечатка
func (s *Server) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	sd, ok := s.takeWASession(r, "reg")
	if !ok {
		writeErr(w, http.StatusBadRequest, "Сеанс добавления ключа истёк, попробуйте ещё раз")
		return
	}
	wa, err := s.webAuthn()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	wu, list := s.loadWAUser(r, u)
	if len(list) >= 10 {
		writeErr(w, http.StatusConflict, "Можно добавить не больше 10 устройств")
		return
	}
	cred, err := wa.FinishRegistration(wu, *sd, r)
	if err != nil {
		s.Log.Warn("passkey register", "err", err)
		writeErr(w, http.StatusBadRequest, "Устройство не подтвердило ключ. Попробуйте ещё раз")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" || len([]rune(name)) > 60 {
		name = shortUA(r.UserAgent())
	}
	raw, _ := json.Marshal(cred)
	if err := s.St.AddPasskey(r.Context(), u.ID, base64.RawURLEncoding.EncodeToString(cred.ID), name, string(raw)); err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "security.passkey_added", name, clientIP(r))
	s.passkeyList(w, r)
}

func (s *Server) passkeyList(w http.ResponseWriter, r *http.Request) {
	list, err := s.St.Passkeys(r.Context(), currentUser(r).ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"passkeys": list})
}

func (s *Server) passkeyDelete(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	s.St.DeletePasskey(r.Context(), u.ID, pathID(r))
	s.St.Audit(r.Context(), u.ID, u.ID, "security.passkey_removed", "", clientIP(r))
	s.passkeyList(w, r)
}

// ---------------------------------------------------------------- проверка ключом

// assertionBegin — параметры для navigator.credentials.get
func (s *Server) assertionBegin(w http.ResponseWriter, r *http.Request, u *store.User, kind string) {
	wa, err := s.webAuthn()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	wu, _ := s.loadWAUser(r, u)
	if len(wu.creds) == 0 {
		writeErr(w, http.StatusConflict, "На аккаунте нет ключей входа")
		return
	}
	opts, sd, err := wa.BeginLogin(wu, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.saveWASession(r, kind, sd)
	writeJSON(w, 200, opts)
}

func (s *Server) assertionFinish(r *http.Request, u *store.User, kind string) error {
	sd, ok := s.takeWASession(r, kind)
	if !ok {
		return errors.New("Сеанс проверки истёк, попробуйте ещё раз")
	}
	wa, err := s.webAuthn()
	if err != nil {
		return err
	}
	// тело читаем один раз и отдаём библиотеке копией
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	r.Body = io.NopCloser(bytes.NewReader(body))
	wu, _ := s.loadWAUser(r, u)
	cred, err := wa.FinishLogin(wu, *sd, r)
	if err != nil {
		s.Log.Warn("passkey login", "err", err)
		return errors.New("Ключ не подошёл. Попробуйте ещё раз или введите код")
	}
	raw, _ := json.Marshal(cred)
	s.St.UpdatePasskeyUse(r.Context(), u.ID, base64.RawURLEncoding.EncodeToString(cred.ID), string(raw))
	return nil
}

// Вход: второй шаг ключом.
func (s *Server) loginPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	if sess == nil || !sess.MFAPending {
		writeErr(w, http.StatusUnauthorized, "Сессия входа истекла, войдите ещё раз")
		return
	}
	u, err := s.St.UserByID(r.Context(), sess.UserID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.assertionBegin(w, r, u, "login")
}

func (s *Server) loginPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	if sess == nil || !sess.MFAPending {
		writeErr(w, http.StatusUnauthorized, "Сессия входа истекла, войдите ещё раз")
		return
	}
	if !s.limiter.Allow("2fa:"+sess.ID, 6, 10*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много попыток. Войдите заново через 10 минут")
		return
	}
	u, err := s.St.UserByID(r.Context(), sess.UserID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if err := s.assertionFinish(r, u, "login"); err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.St.CompleteMFA(r.Context(), sess.ID)
	s.afterLogin(r, u)
	writeJSON(w, 200, s.mePayload(r, u))
}

// Подтверждение опасного действия ключом: на 5 минут вместо пароля и кода.
func (s *Server) stepUpBegin(w http.ResponseWriter, r *http.Request) {
	s.assertionBegin(w, r, currentUser(r), "stepup")
}

func (s *Server) stepUpFinish(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if err := s.assertionFinish(r, u, "stepup"); err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	s.St.SetKV(r.Context(), "stepup:"+currentSession(r).ID, strconv.FormatInt(time.Now().Unix()+stepUpTTL, 10))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) stepUpValid(r *http.Request) bool {
	sess := currentSession(r)
	if sess == nil {
		return false
	}
	exp, _ := strconv.ParseInt(s.St.GetKV(r.Context(), "stepup:"+sess.ID), 10, 64)
	return exp > time.Now().Unix()
}

// confirmAction — подтверждение опасного действия: недавняя проверка ключом, либо пароль и (если включена защита) код из приложения.
func (s *Server) confirmAction(w http.ResponseWriter, r *http.Request, password, code string) bool {
	u := currentUser(r)
	if !s.limiter.Allow("confirm:"+strconv.FormatInt(u.ID, 10), 10, 15*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много попыток. Подождите 15 минут")
		return false
	}
	if s.stepUpValid(r) {
		return true
	}
	if !u.HasPassword && !u.TOTPEnabled {
		writeFieldErr(w, "password", "У аккаунта ещё нет пароля — задайте его в Профиль → Безопасность")
		return false
	}
	if u.HasPassword && !auth.CheckPassword(u.PasswordHash, password) {
		writeFieldErr(w, "password", "Неверный пароль")
		return false
	}
	if u.TOTPEnabled {
		code = strings.TrimSpace(code)
		step, ok := auth.VerifyTOTPStep(u.TOTPSecret, code, time.Now(), u.TOTPLastStep)
		if ok {
			s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"totp_last_step": step})
		}
		if !ok && len(code) > 6 {
			ok = s.useRecoveryCode(r, u, code)
		}
		if !ok {
			writeFieldErr(w, "code", "Введите код из Google Authenticator")
			return false
		}
	}
	return true
}
