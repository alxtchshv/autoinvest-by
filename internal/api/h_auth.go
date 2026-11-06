package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"autoinvest/internal/auth"
	"autoinvest/internal/market"
	"autoinvest/internal/notify"
	"autoinvest/internal/store"
)

var phoneRe = regexp.MustCompile(`^\+375(25|29|33|44)\d{7}$`)

func normPhone(p string) string {
	var b strings.Builder
	for _, r := range p {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if d == "" {
		return ""
	}
	if strings.HasPrefix(d, "80") && len(d) == 11 {
		d = "375" + d[2:]
	}
	return "+" + d
}

func validEmail(e string) bool {
	a, err := mail.ParseAddress(e)
	return err == nil && a.Address == e && strings.Contains(e, ".") && len(e) <= 200
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	c := s.Cfg
	writeJSON(w, 200, map[string]any{
		"mode":            c.Mode(),
		"payments":        c.Payments,
		"broker":          c.Broker,
		"market_demo":     s.Hub.IsFixture(),
		"telegram_bot":    s.botUsername(),
		"telegram_test":   s.TG != nil,
		"fee_free_bp":     c.FeeFreeBP,
		"fee_pro_bp":      c.FeeProBP,
		"spread_bp":       c.SpreadBP,
		"pro_month_kop":   c.ProMonthKop,
		"pro_year_kop":    c.ProYearKop,
		"min_contrib_kop": c.MinContribKop,
		"max_contrib_kop": c.MaxContribKop,
		"free_plan_limit": c.FreePlanLimit,
		"pro_plan_limit":  c.ProPlanLimit,
		"require_kyc":     c.RequireKYC,
		"auth":            map[string]string{"google": c.OAuthGoogle, "apple": c.OAuthApple, "phone": c.SMSProvider},
		"assets":          market.Catalog,
		"strategies":      market.Strategies,
		"version":         s.Version,
		"legal_version":   LegalVersion,
	})
}

func (s *Server) botUsername() string {
	if s.Bot == nil {
		return ""
	}
	return s.Bot.Username
}

func (s *Server) setSession(w http.ResponseWriter, r *http.Request, u *store.User, mfaPending bool) error {
	tok, hash := auth.NewToken()
	if err := s.St.CreateSession(r.Context(), hash, u.ID, sessionTTL, clientIP(r), r.UserAgent(), mfaPending); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: tok, Path: "/", HttpOnly: true, Secure: s.Cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL / time.Second)})
	return nil
}

func (s *Server) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.Cfg.SecureCookies, SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		Password string `json:"password"`
		Ref      string `json:"ref"`
		Offer    bool   `json:"accept_offer"`   // публичная оферта и уведомление о рисках
		Privacy  bool   `json:"accept_privacy"` // согласие на обработку персональных данных (Закон № 99-З)
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !s.limiter.Allow("reg:"+clientIP(r), 10, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много регистраций с вашего адреса. Попробуйте позже")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if l := len([]rune(in.Name)); l < 2 || l > 60 {
		writeFieldErr(w, "name", "Укажите имя от 2 до 60 символов")
		return
	}
	if !validEmail(in.Email) {
		writeFieldErr(w, "email", "Проверьте адрес почты")
		return
	}
	phone := normPhone(in.Phone)
	if phone != "" && !phoneRe.MatchString(phone) {
		writeFieldErr(w, "phone", "Номер в формате +375 (29) 123-45-67")
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		writeFieldErr(w, "password", err.Error())
		return
	}
	if !in.Offer {
		writeFieldErr(w, "accept_offer", "Нужно принять оферту и подтвердить, что вы знаете о рисках")
		return
	}
	// Согласие на обработку данных по Закону № 99-З — отдельное и добровольное, его нельзя объединять с офертой.
	if !in.Privacy {
		writeFieldErr(w, "accept_privacy", "Без согласия на обработку персональных данных мы не можем открыть аккаунт")
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	var refBy *int64
	if in.Ref != "" {
		if ref, err := s.St.UserByReferral(r.Context(), in.Ref); err == nil {
			refBy = &ref.ID
		}
	}
	u, err := s.St.CreateUser(r.Context(), store.NewUser{Email: in.Email, Phone: phone, Name: in.Name, PasswordHash: hash, ReferredBy: refBy})
	if errors.Is(err, store.ErrEmailTaken) {
		writeFieldErr(w, "email", err.Error())
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "auth.register", "", clientIP(r))
	for _, kind := range []string{store.ConsentOffer, store.ConsentRisks, store.ConsentPrivacy} {
		if err := s.St.RecordConsent(r.Context(), u.ID, kind, LegalVersion); err != nil {
			s.Log.Error("consent", "user", u.ID, "err", err)
		}
	}
	s.sendVerification(r, u)
	s.N.Send(r.Context(), u.ID, notify.Message{Kind: "welcome", Title: "Добро пожаловать в AutoInvest",
		Body: "Создайте первую копилку: выберите сумму, расписание и стратегию — дальше всё произойдёт само.", Link: "/app/plans/new"})
	if err := s.setSession(w, r, u, false); err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.mePayload(r, u))
}

func (s *Server) sendVerification(r *http.Request, u *store.User) {
	tok, err := s.St.CreateEmailToken(r.Context(), u.ID, "verify", 72*time.Hour)
	if err != nil {
		return
	}
	link := s.Cfg.PublicURL + "/verify-email?token=" + tok
	go s.N.Mailer().Send(u.Email, "Подтвердите почту для AutoInvest",
		"Здравствуйте, "+u.Name+"!\n\nПодтвердите адрес почты, чтобы получать чеки и важные уведомления:\n"+link+"\n\nСсылка действует 72 часа. Если вы не регистрировались в AutoInvest, просто проигнорируйте письмо.")
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	// в поле можно ввести почту или подтверждённый номер телефона
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !s.limiter.Allow("login:"+clientIP(r), 20, 15*time.Minute) || !s.limiter.Allow("login:"+email, 8, 15*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много попыток входа. Подождите 15 минут")
		return
	}
	var u *store.User
	var err error
	if !strings.Contains(email, "@") && phoneRe.MatchString(normPhone(email)) {
		u, err = s.St.UserByPhone(r.Context(), normPhone(email))
	} else {
		u, err = s.St.UserByEmail(r.Context(), email)
	}
	if err == nil && !u.HasPassword {
		writeErr(w, http.StatusUnauthorized, "У этого аккаунта нет пароля — войдите через Google, Apple или по коду на телефон")
		return
	}
	if err != nil || !auth.CheckPassword(u.PasswordHash, in.Password) {
		if err == nil {
			s.St.Audit(r.Context(), u.ID, 0, "auth.login_failed", "", clientIP(r))
		}
		writeErr(w, http.StatusUnauthorized, "Неверная почта, телефон или пароль")
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

func (s *Server) afterLogin(r *http.Request, u *store.User) {
	s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"last_login_at": store.Now()})
	s.St.Audit(r.Context(), u.ID, u.ID, "auth.login", shortUA(r.UserAgent()), clientIP(r))
}

func (s *Server) login2FA(w http.ResponseWriter, r *http.Request) {
	sess := currentSession(r)
	if sess == nil || !sess.MFAPending {
		writeErr(w, http.StatusUnauthorized, "Сессия входа истекла, введите пароль ещё раз")
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &in) {
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
	code := strings.TrimSpace(in.Code)
	step, ok := auth.VerifyTOTPStep(u.TOTPSecret, code, time.Now(), u.TOTPLastStep)
	if ok {
		s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"totp_last_step": step})
	}
	if !ok && len(code) > 6 {
		ok = s.useRecoveryCode(r, u, code)
	}
	if !ok {
		writeFieldErr(w, "code", "Неверный код")
		return
	}
	s.St.CompleteMFA(r.Context(), sess.ID)
	s.afterLogin(r, u)
	writeJSON(w, 200, s.mePayload(r, u))
}

func (s *Server) useRecoveryCode(r *http.Request, u *store.User, code string) bool {
	var hashes []string
	json.Unmarshal([]byte(u.RecoveryCodes), &hashes)
	h := auth.HashToken(strings.ToLower(code))
	for i, x := range hashes {
		if x == h {
			hashes = append(hashes[:i], hashes[i+1:]...)
			b, _ := json.Marshal(hashes)
			s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"recovery_codes": string(b)})
			s.St.Audit(r.Context(), u.ID, u.ID, "auth.recovery_code_used", "", clientIP(r))
			return true
		}
	}
	return false
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if sess := currentSession(r); sess != nil {
		s.St.DeleteSession(r.Context(), sess.ID)
		s.St.Audit(r.Context(), sess.UserID, sess.UserID, "auth.logout", shortUA(r.UserAgent()), clientIP(r))
	}
	s.clearSession(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u == nil {
		if sess := currentSession(r); sess != nil && sess.MFAPending {
			writeJSON(w, 200, map[string]any{"user": nil, "mfa_required": true})
			return
		}
		writeJSON(w, 200, map[string]any{"user": nil})
		return
	}
	writeJSON(w, 200, s.mePayload(r, u))
}

func (s *Server) mePayload(r *http.Request, u *store.User) map[string]any {
	_, unread, _ := s.St.Notifications(r.Context(), u.ID, 1)
	cards, _ := s.St.Cards(r.Context(), u.ID)
	plans, _ := s.St.CountOpenPlans(r.Context(), u.ID)
	return map[string]any{
		"user": u, "is_pro": u.IsPro(), "unread": unread, "cards": len(cards), "plans": plans,
		"telegram_linked": u.TelegramChatID != nil,
	}
}

func (s *Server) verifyEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	uid, err := s.St.UseEmailToken(r.Context(), in.Token, "verify")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Ссылка недействительна или устарела. Запросите новую в профиле")
		return
	}
	s.St.UpdateUserFields(r.Context(), uid, map[string]any{"email_verified_at": store.Now()})
	s.St.Audit(r.Context(), uid, uid, "auth.email_verified", "", clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) resendVerification(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.EmailVerified {
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	if !s.limiter.Allow("verify:"+u.Email, 3, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Письмо уже отправлено. Проверьте папку «Спам» или повторите через час")
		return
	}
	s.sendVerification(r, u)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) forgot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !s.limiter.Allow("forgot:"+clientIP(r), 10, time.Hour) || !s.limiter.Allow("forgot:"+email, 3, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много запросов. Попробуйте через час")
		return
	}
	// Ответ одинаковый, есть аккаунт или нет — чтобы нельзя было перебирать почты.
	if u, err := s.St.UserByEmail(r.Context(), email); err == nil && u.Status == "active" {
		tok, err := s.St.CreateEmailToken(r.Context(), u.ID, "reset", time.Hour)
		if err == nil {
			link := s.Cfg.PublicURL + "/reset-password?token=" + tok
			go s.N.Mailer().Send(u.Email, "Восстановление пароля AutoInvest",
				"Здравствуйте, "+u.Name+"!\n\nЧтобы задать новый пароль, перейдите по ссылке:\n"+link+"\n\nСсылка действует 1 час. Если вы не запрашивали восстановление, ничего не делайте — пароль останется прежним.")
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) reset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		writeFieldErr(w, "password", err.Error())
		return
	}
	uid, err := s.St.UseEmailToken(r.Context(), in.Token, "reset")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Ссылка недействительна или устарела. Запросите восстановление ещё раз")
		return
	}
	hash, _ := auth.HashPassword(in.Password)
	s.St.UpdateUserFields(r.Context(), uid, map[string]any{"password_hash": hash, "email_verified_at": store.Now()})
	s.St.DeleteOtherSessions(r.Context(), uid, "")
	s.St.Audit(r.Context(), uid, uid, "auth.password_reset", "", clientIP(r))
	s.N.Send(r.Context(), uid, notify.Message{Kind: "security", Title: "Пароль изменён", Body: "Все устройства вышли из аккаунта. Если это были не вы — срочно напишите в поддержку.", Email: true})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// shortUA — «Chrome, Windows» из User-Agent.
func shortUA(ua string) string {
	browser := "Браузер"
	switch {
	case strings.Contains(ua, "Edg/"):
		browser = "Edge"
	case strings.Contains(ua, "OPR/") || strings.Contains(ua, "YaBrowser"):
		browser = map[bool]string{true: "Яндекс Браузер", false: "Opera"}[strings.Contains(ua, "YaBrowser")]
	case strings.Contains(ua, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(ua, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(ua, "Safari/"):
		browser = "Safari"
	}
	osName := ""
	switch {
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad"):
		osName = "iOS"
	case strings.Contains(ua, "Android"):
		osName = "Android"
	case strings.Contains(ua, "Windows"):
		osName = "Windows"
	case strings.Contains(ua, "Mac OS"):
		osName = "macOS"
	case strings.Contains(ua, "Linux"):
		osName = "Linux"
	}
	if osName == "" {
		return browser
	}
	return browser + ", " + osName
}
