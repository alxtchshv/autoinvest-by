package api

// Подтверждение личности через внешние сервисы: МСИ (через интернет-банк) и Sumsub (паспорт + селфи).
// Ручная анкета остаётся только как запасной режим KYC_DOCS=manual. Описание путей — в internal/kyc.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"autoinvest/internal/auth"
	"autoinvest/internal/kyc"
	"autoinvest/internal/notify"
	"autoinvest/internal/store"
)

const msiStateTTL = 15 * 60 // секунд на прохождение страницы МСИ

func (s *Server) sumsub() *kyc.Sumsub {
	return &kyc.Sumsub{Base: s.Cfg.SumsubBase, AppToken: s.Cfg.SumsubToken, Secret: s.Cfg.SumsubSecret, WebhookSecret: s.Cfg.SumsubHook, Level: s.Cfg.SumsubLevel}
}

func (s *Server) msi() *kyc.MSI {
	return &kyc.MSI{AuthURL: s.Cfg.MSIAuthURL, TokenURL: s.Cfg.MSITokenURL, UserInfoURL: s.Cfg.MSIUserURL, ClientID: s.Cfg.MSIClientID,
		ClientSecret: s.Cfg.MSISecret, Scope: s.Cfg.MSIScope, RedirectURL: s.Cfg.PublicURL + "/api/v1/kyc/msi/callback"}
}

// sumsubUserID — externalUserId заявителя в Sumsub; по нему вебхук находит нашего пользователя.
func sumsubUserID(uid int64) string { return "ai-" + strconv.FormatInt(uid, 10) }

// ---------------------------------------------------------------- МСИ

// msiStart — начало входа через МСИ: одноразовый state и адрес страницы (настоящей или тестовой).
func (s *Server) msiStart(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.KYCStatus == "approved" {
		writeErr(w, http.StatusConflict, "Личность уже подтверждена")
		return
	}
	if s.Cfg.MSIMode == "off" {
		writeErr(w, http.StatusConflict, "Вход через МСИ пока не подключён")
		return
	}
	state := strings.ToLower(auth.RandomCode(24, ""))
	s.St.SetKV(r.Context(), "msi_state:"+state, fmt.Sprintf("%d:%d", u.ID, time.Now().Unix()+msiStateTTL))
	url := "/verify/msi-sandbox?state=" + state
	if s.Cfg.MSIMode == "oauth" {
		url = s.msi().AuthCodeURL(state)
	}
	writeJSON(w, 200, map[string]string{"redirect_url": url})
}

// useMSIState проверяет, что state выдан этому пользователю и не истёк; state одноразовый.
func (s *Server) useMSIState(ctx context.Context, state string, uid int64) bool {
	if state == "" || len(state) > 64 {
		return false
	}
	v := s.St.GetKV(ctx, "msi_state:"+state)
	owner, exp, ok := strings.Cut(v, ":")
	if !ok || owner != strconv.FormatInt(uid, 10) {
		return false
	}
	e, _ := strconv.ParseInt(exp, 10, 64)
	s.St.SetKV(ctx, "msi_state:"+state, "used")
	return time.Now().Unix() <= e
}

// msiCallback — возврат со страницы МСИ (боевой режим): меняем код на данные и подтверждаем личность.
func (s *Server) msiCallback(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	back := "/app/profile/verification"
	if !s.useMSIState(r.Context(), r.URL.Query().Get("state"), u.ID) || r.URL.Query().Get("code") == "" {
		http.Redirect(w, r, back+"?error=msi", http.StatusFound)
		return
	}
	p, err := s.msi().Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		s.Log.Error("мси: обмен кода", "err", err)
		http.Redirect(w, r, back+"?error=msi", http.StatusFound)
		return
	}
	if msg := s.approveByMSI(r, u, p, "msi"); msg != "" {
		http.Redirect(w, r, back+"?error=msi", http.StatusFound)
		return
	}
	http.Redirect(w, r, back+"?done=msi", http.StatusFound)
}

// msiSandbox — тестовая страница МСИ: идентификационный номер и код из SMS (подходит любой из 6 цифр).
// Имя берётся из профиля, остальное выводится из номера — как если бы банк вернул данные.
func (s *Server) msiSandbox(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.MSIMode != "sandbox" {
		writeErr(w, http.StatusNotFound, "Тестовый режим МСИ выключен")
		return
	}
	u := currentUser(r)
	var in struct {
		State          string `json:"state"`
		PersonalNumber string `json:"personal_number"`
		Code           string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	pn := strings.ToUpper(strings.TrimSpace(in.PersonalNumber))
	if msg := checkPersonalNumber(pn, time.Time{}); msg != "" {
		writeFieldErr(w, "personal_number", msg)
		return
	}
	if len(in.Code) != 6 || strings.Trim(in.Code, "0123456789") != "" {
		writeFieldErr(w, "code", "Введите 6 цифр из SMS")
		return
	}
	if !s.useMSIState(r.Context(), in.State, u.ID) {
		writeErr(w, http.StatusBadRequest, "Сеанс МСИ устарел — начните заново из профиля")
		return
	}
	last, first := splitName(u.Name)
	p := &kyc.Person{LastName: last, FirstName: first, PersonalNumber: pn, DocNumber: "MP" + pn[1:8], Address: "г. Минск (тестовые данные)"}
	if b, ok := kyc.BirthFromPersonal(pn); ok {
		p.BirthDate = b.Format("2006-01-02")
	}
	if msg := s.approveByMSI(r, u, p, "msi-sandbox"); msg != "" {
		writeFieldErr(w, "personal_number", msg)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// approveByMSI сохраняет данные из банка и подтверждает личность. Возвращает текст ошибки для пользователя.
func (s *Server) approveByMSI(r *http.Request, u *store.User, p *kyc.Person, method string) string {
	if b, err := time.Parse("2006-01-02", p.BirthDate); err == nil && b.AddDate(18, 0, 0).After(time.Now()) {
		return "Сервис доступен с 18 лет"
	}
	k := personKYC(u.ID, p)
	k.Status, k.Method = "approved", method
	if err := s.St.SetKYCResult(r.Context(), k); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return "Этот идентификационный номер уже используется в другом аккаунте"
		}
		s.Log.Error("kyc: сохранение МСИ", "err", err)
		return "Не удалось сохранить данные, попробуйте ещё раз"
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "kyc.msi", method, clientIP(r))
	s.N.Send(r.Context(), u.ID, notify.Message{Kind: "kyc", Title: "Личность подтверждена", Body: "Данные получены через межбанковскую систему идентификации.", Link: "/app/profile/verification", Email: true})
	return ""
}

// ---------------------------------------------------------------- паспорт и селфи

// docsStart — что открыть на странице: WebSDK Sumsub с токеном или тестовую проверку.
func (s *Server) docsStart(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.KYCStatus == "approved" {
		writeErr(w, http.StatusConflict, "Личность уже подтверждена")
		return
	}
	switch s.Cfg.KYCDocs {
	case "sumsub":
		tok, err := s.sumsub().AccessToken(r.Context(), sumsubUserID(u.ID), u.Email)
		if err != nil {
			s.Log.Error("sumsub: токен", "err", err)
			writeErr(w, http.StatusBadGateway, "Сервис проверки документов не отвечает, попробуйте через несколько минут")
			return
		}
		writeJSON(w, 200, map[string]string{"provider": "sumsub", "token": tok})
	default:
		writeJSON(w, 200, map[string]string{"provider": s.Cfg.KYCDocs})
	}
}

// docsSubmitted — WebSDK сообщил, что документы отправлены: показываем «на проверке» до вебхука.
// Только переводит в pending: подтвердить личность может лишь подписанный вебхук.
func (s *Server) docsSubmitted(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if s.Cfg.KYCDocs != "sumsub" || u.KYCStatus == "approved" {
		writeJSON(w, 200, map[string]bool{"ok": true})
		return
	}
	s.St.SetKYCResult(r.Context(), store.KYC{UserID: u.ID, Status: "pending", Method: "docs"})
	s.St.Audit(r.Context(), u.ID, u.ID, "kyc.submitted", "sumsub", clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// docsSandbox — тестовая проверка документов: принимает фото паспорта и селфи, через несколько секунд
// «сервис проверки» одобряет — так же асинхронно, как настоящий вебхук.
func (s *Server) docsSandbox(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.KYCDocs != "sandbox" {
		writeErr(w, http.StatusNotFound, "Тестовая проверка документов выключена")
		return
	}
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
	doc, err := s.saveKYCFile(r, u.ID, "doc_file")
	if err != nil || doc == "" {
		writeFieldErr(w, "doc_file", errText(err, "Сфотографируйте страницу паспорта с фото"))
		return
	}
	selfie, err := s.saveKYCFile(r, u.ID, "selfie_file")
	if err != nil || selfie == "" {
		writeFieldErr(w, "selfie_file", errText(err, "Сделайте селфи"))
		return
	}
	if err := s.St.SetKYCResult(r.Context(), store.KYC{UserID: u.ID, Status: "pending", Method: "docs-sandbox", DocFile: doc, SelfieFile: selfie}); err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "kyc.submitted", "sandbox", clientIP(r))
	uid, name := u.ID, u.Name
	time.AfterFunc(4*time.Second, func() {
		ctx := context.Background()
		cur, err := s.St.KYC(ctx, uid)
		if err != nil || cur.Status != "pending" || cur.Method != "docs-sandbox" {
			return // пока ждали, админ или сам пользователь уже что-то изменили
		}
		last, first := splitName(name)
		k := store.KYC{UserID: uid, Status: "approved", Method: "docs-sandbox", LastName: last, FirstName: first, BirthDate: "1990-01-01",
			DocNumber: fmt.Sprintf("MP%07d", uid%10000000), Address: "г. Минск (тестовые данные)"}
		if err := s.St.SetKYCResult(ctx, k); err != nil {
			s.Log.Error("kyc: тестовое одобрение", "err", err)
			return
		}
		s.St.Audit(ctx, uid, 0, "kyc.provider", "sandbox: approved", "")
		s.N.Send(ctx, uid, notify.Message{Kind: "kyc", Title: "Личность подтверждена", Body: "Документы и селфи проверены.", Link: "/app/profile/verification", Email: true})
	})
	s.getKYC(w, r)
}

// sumsubWebhook — итог проверки от Sumsub. Без верной подписи не принимаем ничего.
func (s *Server) sumsubWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Cfg.KYCDocs != "sumsub" {
		writeErr(w, http.StatusNotFound, "Sumsub не подключён")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Не удалось прочитать запрос")
		return
	}
	ss := s.sumsub()
	if !ss.VerifyWebhook(body, r.Header.Get("X-Payload-Digest"), r.Header.Get("X-Payload-Digest-Alg")) {
		writeErr(w, http.StatusUnauthorized, "Неверная подпись")
		return
	}
	var ev kyc.SumsubEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		writeErr(w, http.StatusBadRequest, "Неверный формат")
		return
	}
	uid, err := strconv.ParseInt(strings.TrimPrefix(ev.ExternalUserID, "ai-"), 10, 64)
	if err != nil || !strings.HasPrefix(ev.ExternalUserID, "ai-") {
		writeJSON(w, 200, map[string]bool{"ok": true}) // чужой заявитель (например, тестовый из кабинета) — просто подтверждаем получение
		return
	}
	ctx := r.Context()
	switch ev.Type {
	case "applicantPending":
		s.St.SetKYCResult(ctx, store.KYC{UserID: uid, Status: "pending", Method: "docs", ProviderRef: ev.ApplicantID})
	case "applicantReviewed":
		if ev.ReviewResult.ReviewAnswer == "GREEN" {
			p, err := ss.Applicant(ctx, ev.ApplicantID)
			if err != nil {
				s.Log.Error("sumsub: данные заявителя", "err", err)
				writeErr(w, http.StatusBadGateway, "Повторите позже") // Sumsub пришлёт вебхук ещё раз
				return
			}
			k := personKYC(uid, p)
			k.Status, k.Method, k.ProviderRef = "approved", "docs", ev.ApplicantID
			if err := s.St.SetKYCResult(ctx, k); err != nil {
				if strings.Contains(err.Error(), "UNIQUE") {
					k = store.KYC{UserID: uid, Status: "rejected", Method: "docs", ProviderRef: ev.ApplicantID, RejectReason: "Этот идентификационный номер уже используется в другом аккаунте"}
					s.St.SetKYCResult(ctx, k)
					s.N.Send(ctx, uid, notify.Message{Kind: "kyc", Title: "Верификация не пройдена", Body: k.RejectReason, Link: "/app/profile/verification", Email: true})
					break
				}
				s.internal(w, r, err)
				return
			}
			s.St.Audit(ctx, uid, 0, "kyc.provider", "sumsub: approved", "")
			s.N.Send(ctx, uid, notify.Message{Kind: "kyc", Title: "Личность подтверждена", Body: "Документы и селфи проверены.", Link: "/app/profile/verification", Email: true})
		} else {
			reason := strings.TrimSpace(ev.ReviewResult.ClientComment)
			if reason == "" {
				reason = "Фото документа или селфи не прошли проверку"
			}
			if ev.ReviewResult.ReviewRejectType == "RETRY" {
				reason += ". Можно пройти проверку ещё раз"
			}
			s.St.SetKYCResult(ctx, store.KYC{UserID: uid, Status: "rejected", Method: "docs", ProviderRef: ev.ApplicantID, RejectReason: reason})
			s.St.Audit(ctx, uid, 0, "kyc.provider", "sumsub: rejected", "")
			s.N.Send(ctx, uid, notify.Message{Kind: "kyc", Title: "Верификация не пройдена", Body: reason, Link: "/app/profile/verification", Email: true})
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- общее

func personKYC(uid int64, p *kyc.Person) store.KYC {
	return store.KYC{UserID: uid, LastName: p.LastName, FirstName: p.FirstName, MiddleName: p.MiddleName, BirthDate: p.BirthDate,
		PersonalNumber: strings.ToUpper(p.PersonalNumber), DocNumber: strings.ToUpper(p.DocNumber), DocIssuedAt: p.DocIssuedAt, Address: p.Address}
}

// splitName — «Имя Фамилия» из профиля в (фамилия, имя) для тестовых данных.
func splitName(name string) (last, first string) {
	parts := strings.Fields(name)
	switch len(parts) {
	case 0:
		return "Тестовый", "Пользователь"
	case 1:
		return parts[0], parts[0]
	default:
		return parts[len(parts)-1], parts[0]
	}
}

func errText(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

// saveKYCFile сохраняет загруженный файл (фото или PDF до 8 МБ) в DATA_DIR/kyc; пустая строка — файла нет.
func (s *Server) saveKYCFile(r *http.Request, uid int64, field string) (string, error) {
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
	dir := filepath.Join(s.Cfg.DataDir, "kyc")
	os.MkdirAll(dir, 0o700)
	name := fmt.Sprintf("%d_%s_%s%s", uid, field, strings.ToLower(auth.RandomCode(12, "")), ext)
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
