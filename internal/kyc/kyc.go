// Package kyc — подтверждение личности через внешние сервисы вместо ручной анкеты.
//
// Два пути, как у белорусских криптоплатформ:
//   - МСИ (межбанковская система идентификации, оператор — ОАО «НКФО «ЕРИП»): человек входит через свой
//     интернет-банк, банк передаёт уже проверенные данные. Без фото паспорта и селфи. Подключение — договор с ЕРИП,
//     OAuth 2.0 (адреса и состав полей выдаёт оператор вместе с тестовым стендом).
//   - Sumsub: фото документа + селфи с проверкой «живости», распознавание и сверка делает сервис, итог приходит вебхуком.
//
// В тестовом режиме (sandbox) оба пути работают без договоров: страницы МСИ и проверки документов имитирует сам сервис.
package kyc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Person — данные, которые вернул сервис проверки.
type Person struct {
	LastName       string
	FirstName      string
	MiddleName     string
	BirthDate      string // ГГГГ-ММ-ДД
	PersonalNumber string // идентификационный номер
	DocNumber      string
	DocIssuedAt    string
	Address        string
}

// ---------------------------------------------------------------- Sumsub

// Sumsub — клиент API Sumsub (https://docs.sumsub.com). Запросы подписываются HMAC-SHA256:
// подпись = hex(HMAC(secret, ts + METHOD + path?query + body)), заголовки X-App-Token, X-App-Access-Ts, X-App-Access-Sig.
type Sumsub struct {
	Base          string // https://api.sumsub.com
	AppToken      string
	Secret        string
	WebhookSecret string
	Level         string // имя уровня проверки в кабинете Sumsub, например basic-kyc-level
	Client        *http.Client
	now           func() time.Time
}

func (s *Sumsub) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (s *Sumsub) do(ctx context.Context, method, path string, body []byte, out any) error {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	ts := strconv.FormatInt(now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(s.Secret))
	mac.Write([]byte(ts + strings.ToUpper(method) + path))
	mac.Write(body)
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.Base, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("X-App-Token", s.AppToken)
	req.Header.Set("X-App-Access-Ts", ts)
	req.Header.Set("X-App-Access-Sig", hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("sumsub %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return json.Unmarshal(raw, out)
}

// AccessToken — короткоживущий токен для WebSDK на странице пользователя. userID — наш идентификатор
// (в Sumsub он становится externalUserId заявителя).
func (s *Sumsub) AccessToken(ctx context.Context, userID, email string) (string, error) {
	in := map[string]any{"userId": userID, "levelName": s.Level, "ttlInSecs": 1200}
	if email != "" {
		in["applicantIdentifiers"] = map[string]string{"email": email}
	}
	body, _ := json.Marshal(in)
	var out struct {
		Token string `json:"token"`
	}
	if err := s.do(ctx, http.MethodPost, "/resources/accessTokens/sdk", body, &out); err != nil {
		return "", err
	}
	if out.Token == "" {
		return "", errors.New("sumsub: пустой токен")
	}
	return out.Token, nil
}

// Applicant — распознанные данные заявителя (после зелёного результата проверки).
func (s *Sumsub) Applicant(ctx context.Context, applicantID string) (*Person, error) {
	var out struct {
		Info struct {
			FirstName  string `json:"firstName"`
			LastName   string `json:"lastName"`
			MiddleName string `json:"middleName"`
			Dob        string `json:"dob"`
			IDDocs     []struct {
				Number         string `json:"number"`
				PersonalNumber string `json:"personalNumber"`
				IssuedDate     string `json:"issuedDate"`
			} `json:"idDocs"`
			Addresses []struct {
				FormattedAddress string `json:"formattedAddress"`
			} `json:"addresses"`
		} `json:"info"`
	}
	if err := s.do(ctx, http.MethodGet, "/resources/applicants/"+url.PathEscape(applicantID)+"/one", nil, &out); err != nil {
		return nil, err
	}
	p := &Person{FirstName: out.Info.FirstName, LastName: out.Info.LastName, MiddleName: out.Info.MiddleName, BirthDate: out.Info.Dob}
	for _, d := range out.Info.IDDocs {
		if p.DocNumber == "" {
			p.DocNumber, p.DocIssuedAt = d.Number, d.IssuedDate
		}
		if p.PersonalNumber == "" {
			p.PersonalNumber = d.PersonalNumber
		}
	}
	if len(out.Info.Addresses) > 0 {
		p.Address = out.Info.Addresses[0].FormattedAddress
	}
	return p, nil
}

// SumsubEvent — вебхук Sumsub. Нас интересует applicantReviewed: GREEN — личность подтверждена, RED — нет;
// reviewRejectType RETRY означает «можно исправить и отправить снова», FINAL — окончательный отказ.
type SumsubEvent struct {
	Type           string `json:"type"`
	ApplicantID    string `json:"applicantId"`
	ExternalUserID string `json:"externalUserId"`
	ReviewStatus   string `json:"reviewStatus"`
	ReviewResult   struct {
		ReviewAnswer      string   `json:"reviewAnswer"`
		ReviewRejectType  string   `json:"reviewRejectType"`
		ModerationComment string   `json:"moderationComment"`
		ClientComment     string   `json:"clientComment"`
		RejectLabels      []string `json:"rejectLabels"`
	} `json:"reviewResult"`
}

// VerifyWebhook проверяет X-Payload-Digest: HMAC тела секретом вебхука, алгоритм — из X-Payload-Digest-Alg.
func (s *Sumsub) VerifyWebhook(body []byte, digest, alg string) bool {
	if s.WebhookSecret == "" || digest == "" {
		return false
	}
	var h func() hash.Hash
	switch alg {
	case "", "HMAC_SHA256_HEX":
		h = sha256.New
	case "HMAC_SHA512_HEX":
		h = sha512.New
	case "HMAC_SHA1_HEX":
		h = sha1.New
	default:
		return false
	}
	mac := hmac.New(h, []byte(s.WebhookSecret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(want), []byte(strings.ToLower(digest)))
}

// ---------------------------------------------------------------- МСИ

// MSI — вход через межбанковскую систему идентификации по OAuth 2.0 (authorization code).
// Адреса, scope и названия полей ответа выдаёт ЕРИП при подключении; здесь — типовая схема,
// поля сопоставляются в fromClaims и при необходимости уточняются под спецификацию оператора.
type MSI struct {
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	ClientID     string
	ClientSecret string
	Scope        string
	RedirectURL  string
	Client       *http.Client
}

func (m *MSI) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// AuthCodeURL — куда отправить пользователя: страница МСИ с выбором банка.
func (m *MSI) AuthCodeURL(state string) string {
	q := url.Values{"response_type": {"code"}, "client_id": {m.ClientID}, "redirect_uri": {m.RedirectURL}, "state": {state}}
	if m.Scope != "" {
		q.Set("scope", m.Scope)
	}
	sep := "?"
	if strings.Contains(m.AuthURL, "?") {
		sep = "&"
	}
	return m.AuthURL + sep + q.Encode()
}

// Exchange меняет код на токен и забирает данные человека.
func (m *MSI) Exchange(ctx context.Context, code string) (*Person, error) {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {m.RedirectURL}, "client_id": {m.ClientID}, "client_secret": {m.ClientSecret}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, m.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := m.getJSON(req, &tok); err != nil {
		return nil, fmt.Errorf("мси: токен: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, errors.New("мси: пустой токен")
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, m.UserInfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", "application/json")
	claims := map[string]any{}
	if err := m.getJSON(req, &claims); err != nil {
		return nil, fmt.Errorf("мси: данные: %w", err)
	}
	return fromClaims(claims), nil
}

func (m *MSI) getJSON(req *http.Request, out any) error {
	resp, err := m.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%d %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return json.Unmarshal(raw, out)
}

// fromClaims берёт первое непустое из возможных названий поля (OIDC-стиль и типовые русские варианты).
func fromClaims(c map[string]any) *Person {
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := c[k].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v)
			}
		}
		return ""
	}
	return &Person{
		LastName:       pick("family_name", "last_name", "surname"),
		FirstName:      pick("given_name", "first_name", "name"),
		MiddleName:     pick("middle_name", "patronymic"),
		BirthDate:      pick("birthdate", "birth_date"),
		PersonalNumber: strings.ToUpper(pick("personal_number", "identification_number", "id_number")),
		DocNumber:      strings.ToUpper(pick("document_number", "passport_number", "doc_number")),
		DocIssuedAt:    pick("document_issue_date", "doc_issued_at"),
		Address:        pick("address", "registration_address"),
	}
}

// BirthFromPersonal извлекает дату рождения из идентификационного номера РБ: 1-я цифра — пол и век
// (1–2: XIX, 3–4: XX, 5–6: XXI), затем ДДММГГ.
func BirthFromPersonal(pn string) (time.Time, bool) {
	if len(pn) < 7 {
		return time.Time{}, false
	}
	century := map[byte]int{'1': 1800, '2': 1800, '3': 1900, '4': 1900, '5': 2000, '6': 2000}[pn[0]]
	if century == 0 {
		return time.Time{}, false
	}
	d, e1 := strconv.Atoi(pn[1:3])
	m, e2 := strconv.Atoi(pn[3:5])
	y, e3 := strconv.Atoi(pn[5:7])
	if e1 != nil || e2 != nil || e3 != nil {
		return time.Time{}, false
	}
	t := time.Date(century+y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d || int(t.Month()) != m {
		return time.Time{}, false
	}
	return t, true
}
