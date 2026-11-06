package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"autoinvest/internal/auth"
	"autoinvest/internal/engine"
	"autoinvest/internal/notify"
	"autoinvest/internal/store"
)

func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Name  *string `json:"name"`
		Phone *string `json:"phone"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	f := map[string]any{}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if l := len([]rune(n)); l < 2 || l > 60 {
			writeFieldErr(w, "name", "Укажите имя от 2 до 60 символов")
			return
		}
		f["name"] = n
	}
	if in.Phone != nil {
		p := normPhone(*in.Phone)
		if p != "" && !phoneRe.MatchString(p) {
			writeFieldErr(w, "phone", "Номер в формате +375 (29) 123-45-67")
			return
		}
		f["phone"] = p
	}
	if err := s.St.UpdateUserFields(r.Context(), u.ID, f); err != nil {
		s.internal(w, r, err)
		return
	}
	u, _ = s.St.UserByID(r.Context(), u.ID)
	writeJSON(w, 200, s.mePayload(r, u))
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !s.limiter.Allow("pwd:"+fmt.Sprint(u.ID), 5, 15*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много попыток. Подождите 15 минут")
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Current) {
		writeFieldErr(w, "current", "Текущий пароль указан неверно")
		return
	}
	if err := auth.ValidatePassword(in.New); err != nil {
		writeFieldErr(w, "new", err.Error())
		return
	}
	hash, _ := auth.HashPassword(in.New)
	s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"password_hash": hash})
	s.St.DeleteOtherSessions(r.Context(), u.ID, currentSession(r).ID)
	s.St.Audit(r.Context(), u.ID, u.ID, "security.password_changed", "", clientIP(r))
	s.N.Send(r.Context(), u.ID, notify.Message{Kind: "security", Title: "Пароль изменён", Body: "Остальные устройства вышли из аккаунта.", Email: true})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	list, err := s.St.ListSessions(r.Context(), u.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	cur := currentSession(r).ID
	for i := range list {
		list[i].Current = list[i].ID == cur
		list[i].UserAgent = shortUA(list[i].UserAgent)
		list[i].ID = list[i].ID[:16] // наружу отдаём только префикс хеша
	}
	writeJSON(w, 200, map[string]any{"sessions": list})
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	prefix := r.PathValue("id")
	list, _ := s.St.ListSessions(r.Context(), u.ID)
	for _, x := range list {
		if strings.HasPrefix(x.ID, prefix) && len(prefix) == 16 && x.ID != currentSession(r).ID {
			s.St.DeleteUserSession(r.Context(), u.ID, x.ID)
			s.St.Audit(r.Context(), u.ID, u.ID, "security.session_revoked", shortUA(x.UserAgent), clientIP(r))
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) revokeOthers(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	s.St.DeleteOtherSessions(r.Context(), u.ID, currentSession(r).ID)
	s.St.Audit(r.Context(), u.ID, u.ID, "security.sessions_revoked", "", clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) setup2FA(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.TOTPEnabled {
		writeErr(w, http.StatusConflict, "Двухфакторная защита уже включена")
		return
	}
	secret := auth.NewTOTPSecret()
	s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"totp_secret": secret})
	writeJSON(w, 200, map[string]string{"secret": secret, "url": auth.TOTPURL("AutoInvest", u.Email, secret)})
}

func (s *Server) enable2FA(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if u.TOTPSecret == "" {
		writeErr(w, http.StatusBadRequest, "Сначала отсканируйте QR-код")
		return
	}
	if !auth.VerifyTOTP(u.TOTPSecret, in.Code, time.Now()) {
		writeFieldErr(w, "code", "Код не подошёл. Проверьте время на телефоне и введите новый код")
		return
	}
	plain, hashes := auth.NewRecoveryCodes()
	b, _ := json.Marshal(hashes)
	s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"totp_enabled": true, "recovery_codes": string(b)})
	s.St.Audit(r.Context(), u.ID, u.ID, "security.2fa_enabled", "", clientIP(r))
	s.N.Send(r.Context(), u.ID, notify.Message{Kind: "security", Title: "Двухфакторная защита включена", Email: true})
	writeJSON(w, 200, map[string]any{"recovery_codes": plain})
}

func (s *Server) disable2FA(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Password) {
		writeFieldErr(w, "password", "Неверный пароль")
		return
	}
	if !auth.VerifyTOTP(u.TOTPSecret, in.Code, time.Now()) && !s.useRecoveryCode(r, u, in.Code) {
		writeFieldErr(w, "code", "Неверный код")
		return
	}
	s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"totp_enabled": false, "totp_secret": "", "recovery_codes": ""})
	s.St.Audit(r.Context(), u.ID, u.ID, "security.2fa_disabled", "", clientIP(r))
	s.N.Send(r.Context(), u.ID, notify.Message{Kind: "security", Title: "Двухфакторная защита отключена", Body: "Если это были не вы — смените пароль и напишите в поддержку.", Email: true})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) updateNotifySettings(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Email    *bool `json:"notify_email"`
		Telegram *bool `json:"notify_telegram"`
		Market   *bool `json:"notify_market"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	f := map[string]any{}
	if in.Email != nil {
		f["notify_email"] = *in.Email
	}
	if in.Telegram != nil {
		f["notify_telegram"] = *in.Telegram
	}
	if in.Market != nil {
		f["notify_market"] = *in.Market
	}
	s.St.UpdateUserFields(r.Context(), u.ID, f)
	u, _ = s.St.UserByID(r.Context(), u.ID)
	writeJSON(w, 200, s.mePayload(r, u))
}

// Анкета риск-профиля: 6 вопросов, у каждого ответа 1–4 балла.
func (s *Server) submitRisk(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Answers []int `json:"answers"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Answers) != 6 {
		writeErr(w, http.StatusBadRequest, "Ответьте на все вопросы")
		return
	}
	sum := 0
	for _, a := range in.Answers {
		if a < 1 || a > 4 {
			writeErr(w, http.StatusBadRequest, "Некорректный ответ")
			return
		}
		sum += a
	}
	profile := "conservative"
	switch {
	case sum >= 19:
		profile = "aggressive"
	case sum >= 14:
		profile = "moderate"
	}
	s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"risk_profile": profile})
	s.St.Audit(r.Context(), u.ID, u.ID, "profile.risk", profile, clientIP(r))
	writeJSON(w, 200, map[string]any{"risk_profile": profile, "score": sum, "max_risk": maxRisk(profile)})
}

func maxRisk(profile string) int {
	switch profile {
	case "conservative":
		return 2
	case "moderate":
		return 3
	case "aggressive":
		return 5
	}
	return 5
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	list, err := s.St.AuditFor(r.Context(), currentUser(r).ID, 50)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"events": list})
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Password) {
		writeFieldErr(w, "password", "Неверный пароль")
		return
	}
	plans, _ := s.St.Plans(r.Context(), u.ID, true)
	for _, p := range plans {
		if v := s.En.Value(p.Positions); v.ValueKop > 0 || len(p.Positions) > 0 && v.InvestedKop > 0 {
			writeErr(w, http.StatusConflict, "Сначала выведите деньги из копилки «"+p.Name+"»")
			return
		}
	}
	var pending int
	s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM orders WHERE user_id=? AND status IN ('pending','payout_pending','manual')`, u.ID).Scan(&pending)
	if pending > 0 {
		writeErr(w, http.StatusConflict, "Дождитесь завершения операций в обработке")
		return
	}
	ctx := r.Context()
	s.DB.ExecContext(ctx, `UPDATE plans SET status='closed', closed_at=?, next_run_at=NULL WHERE user_id=? AND status!='closed'`, store.Now(), u.ID)
	s.DB.ExecContext(ctx, `UPDATE cards SET status='removed', token='' WHERE user_id=?`, u.ID)
	s.DB.ExecContext(ctx, `UPDATE users SET status='deleted', email=?, phone='', name='Удалённый пользователь', telegram_chat_id=NULL, totp_secret='', recovery_codes='', updated_at=? WHERE id=?`,
		fmt.Sprintf("deleted+%d@deleted.local", u.ID), store.Now(), u.ID)
	s.DB.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, u.ID)
	s.St.RevokeConsents(ctx, u.ID)
	s.St.Audit(ctx, u.ID, u.ID, "account.deleted", "", clientIP(r))
	s.clearSession(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- KYC

var (
	personalRe = regexp.MustCompile(`^[0-9]{7}[A-Z][0-9]{3}[A-Z]{2}[0-9]$`)
	docRe      = regexp.MustCompile(`^[A-Z]{2}[0-9]{7}$`)
)

func (s *Server) getKYC(w http.ResponseWriter, r *http.Request) {
	k, err := s.St.KYC(r.Context(), currentUser(r).ID)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, 200, map[string]any{"kyc": nil})
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"kyc": k})
}

func (s *Server) submitKYC(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.KYCStatus == "approved" {
		writeErr(w, http.StatusConflict, "Личность уже подтверждена")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "Файлы слишком большие (до 8 МБ каждый)")
		return
	}
	v := func(k string) string { return strings.TrimSpace(r.FormValue(k)) }
	k := store.KYC{UserID: u.ID, LastName: v("last_name"), FirstName: v("first_name"), MiddleName: v("middle_name"),
		BirthDate: v("birth_date"), PersonalNumber: strings.ToUpper(v("personal_number")), DocNumber: strings.ToUpper(strings.ReplaceAll(v("doc_number"), " ", "")),
		DocIssuedAt: v("doc_issued_at"), Citizenship: strings.ToUpper(v("citizenship")), Address: v("address")}
	if k.Citizenship == "" {
		k.Citizenship = "BY"
	}
	for f, val := range map[string]string{"last_name": k.LastName, "first_name": k.FirstName} {
		if l := len([]rune(val)); l < 2 || l > 60 {
			writeFieldErr(w, f, "Заполните как в паспорте")
			return
		}
	}
	bd, err := time.Parse("2006-01-02", k.BirthDate)
	if err != nil {
		writeFieldErr(w, "birth_date", "Укажите дату рождения")
		return
	}
	if bd.AddDate(18, 0, 0).After(time.Now()) {
		writeFieldErr(w, "birth_date", "Сервис доступен с 18 лет")
		return
	}
	if msg := checkPersonalNumber(k.PersonalNumber, bd); msg != "" {
		writeFieldErr(w, "personal_number", msg)
		return
	}
	if !docRe.MatchString(k.DocNumber) {
		writeFieldErr(w, "doc_number", "Серия и номер паспорта, например MP1234567")
		return
	}
	existing, _ := s.St.KYC(r.Context(), u.ID)
	dir := filepath.Join(s.Cfg.DataDir, "kyc")
	os.MkdirAll(dir, 0o700)
	save := func(field string) (string, error) {
		f, hdr, err := r.FormFile(field)
		if err != nil {
			return "", nil
		}
		defer f.Close()
		if hdr.Size > 8<<20 {
			return "", errors.New("Файл больше 8 МБ")
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		ct := http.DetectContentType(head[:n])
		ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "application/pdf": ".pdf", "image/webp": ".webp"}[ct]
		if ext == "" {
			return "", errors.New("Загрузите фото (JPG, PNG) или PDF")
		}
		name := fmt.Sprintf("%d_%s_%s%s", u.ID, field, strings.ToLower(auth.RandomCode(12, "")), ext)
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if err != nil {
			return "", err
		}
		defer out.Close()
		out.Write(head[:n])
		if _, err := io.Copy(out, f); err != nil {
			return "", err
		}
		return name, nil
	}
	if k.DocFile, err = save("doc_file"); err != nil {
		writeFieldErr(w, "doc_file", err.Error())
		return
	}
	if k.SelfieFile, err = save("selfie_file"); err != nil {
		writeFieldErr(w, "selfie_file", err.Error())
		return
	}
	if k.DocFile == "" && (existing == nil || existing.DocFile == "") {
		writeFieldErr(w, "doc_file", "Загрузите фото разворота паспорта")
		return
	}
	if err := s.St.SubmitKYC(r.Context(), k); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeFieldErr(w, "personal_number", "Этот идентификационный номер уже используется")
			return
		}
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "kyc.submitted", "", clientIP(r))
	s.N.Send(r.Context(), u.ID, notify.Message{Kind: "kyc", Title: "Документы отправлены на проверку", Body: "Обычно проверка занимает до одного рабочего дня.", Link: "/app/profile/verification"})
	s.getKYC(w, r)
}

// ---------------------------------------------------------------- Telegram

func (s *Server) telegramLink(w http.ResponseWriter, r *http.Request) {
	if s.Bot == nil || s.Bot.Username == "" {
		writeErr(w, http.StatusServiceUnavailable, "Telegram-бот ещё не подключён администратором")
		return
	}
	code := auth.RandomCode(24, "abcdefghijkmnopqrstuvwxyz23456789")
	if err := s.St.CreateTelegramLink(r.Context(), currentUser(r).ID, code, 15*60); err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]string{"url": "https://t.me/" + s.Bot.Username + "?start=" + code})
}

func (s *Server) telegramUnlink(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.TelegramChatID != nil && s.Bot != nil {
		go s.Bot.Send(*u.TelegramChatID, "Telegram отключён от аккаунта AutoInvest.")
	}
	s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"telegram_chat_id": nil, "telegram_username": ""})
	s.St.Audit(r.Context(), u.ID, u.ID, "telegram.unlink", "", clientIP(r))
	u, _ = s.St.UserByID(r.Context(), u.ID)
	writeJSON(w, 200, s.mePayload(r, u))
}

// ---------------------------------------------------------------- рефералка и подписка

func (s *Server) referral(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	st, friends, err := s.St.Referrals(r.Context(), u.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"code": u.ReferralCode, "link": s.Cfg.PublicURL + "/r/" + u.ReferralCode, "stats": st, "friends": friends})
}

func (s *Server) billing(w http.ResponseWriter, r *http.Request) {
	u, err := s.St.UserByID(r.Context(), currentUser(r).ID) // свежие данные после оплаты
	if err != nil {
		s.internal(w, r, err)
		return
	}
	pays, _ := s.St.Payments(r.Context(), u.ID, "subscription", 24)
	if pays == nil {
		pays = []store.Payment{}
	}
	writeJSON(w, 200, map[string]any{"tier": u.Tier, "is_pro": u.IsPro(), "pro_until": u.ProUntil, "autorenew": u.ProAutorenew, "period": u.ProPeriod,
		"month_kop": s.Cfg.ProMonthKop, "year_kop": s.Cfg.ProYearKop, "payments": pays})
}

func (s *Server) buyPro(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Period string `json:"period"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Period != "year" {
		in.Period = "month"
	}
	if !s.limiter.Allow("pro:"+fmt.Sprint(u.ID), 5, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много попыток оплаты. Попробуйте позже")
		return
	}
	if err := s.En.ChargePro(r.Context(), u.ID, in.Period, false); err != nil {
		if errors.Is(err, engine.ErrPaymentPending) {
			writeJSON(w, http.StatusAccepted, apiErr{Error: err.Error(), Code: "pending"})
			return
		}
		writeErr(w, http.StatusPaymentRequired, err.Error())
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "billing.pro", in.Period, clientIP(r))
	s.billing(w, r.WithContext(r.Context()))
}

func (s *Server) setAutorenew(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		On bool `json:"on"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"pro_autorenew": in.On})
	s.St.Audit(r.Context(), u.ID, u.ID, "billing.autorenew", fmt.Sprint(in.On), clientIP(r))
	s.billing(w, r)
}
