// Package oauth — вход через Google и Apple (OAuth 2.0 / OpenID Connect, authorization code).
//
// Google: код меняется на токен, данные берутся из userinfo по TLS напрямую у Google.
// Apple: код меняется на id_token в https://appleid.apple.com/auth/token (client_secret — JWT, подписанный ключом .p8);
// id_token получен напрямую от Apple по TLS, поэтому достаточно проверить iss, aud и срок (OIDC Core, п. 3.1.3.7).
package oauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Profile — кто вошёл.
type Profile struct {
	Subject       string // постоянный идентификатор у провайдера
	Email         string
	EmailVerified bool
	Name          string
}

func client(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func doJSON(c *http.Client, req *http.Request, out any) error {
	resp, err := client(c).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: %d %s", req.URL.Host, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return json.Unmarshal(raw, out)
}

// ---------------------------------------------------------------- Google

type Google struct {
	ClientID, ClientSecret, RedirectURL string
	AuthBase, TokenURL, UserInfoURL     string // пусто — боевые адреса Google
	Client                              *http.Client
}

func (g *Google) AuthURL(state string) string {
	base := g.AuthBase
	if base == "" {
		base = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	q := url.Values{"client_id": {g.ClientID}, "redirect_uri": {g.RedirectURL}, "response_type": {"code"},
		"scope": {"openid email profile"}, "state": {state}, "prompt": {"select_account"}}
	return base + "?" + q.Encode()
}

func (g *Google) Exchange(ctx context.Context, code string) (*Profile, error) {
	tokenURL, infoURL := g.TokenURL, g.UserInfoURL
	if tokenURL == "" {
		tokenURL = "https://oauth2.googleapis.com/token"
	}
	if infoURL == "" {
		infoURL = "https://openidconnect.googleapis.com/v1/userinfo"
	}
	form := url.Values{"code": {code}, "client_id": {g.ClientID}, "client_secret": {g.ClientSecret}, "redirect_uri": {g.RedirectURL}, "grant_type": {"authorization_code"}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := doJSON(g.Client, req, &tok); err != nil {
		return nil, fmt.Errorf("google token: %w", err)
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	var info struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := doJSON(g.Client, req, &info); err != nil {
		return nil, fmt.Errorf("google userinfo: %w", err)
	}
	if info.Sub == "" {
		return nil, errors.New("google: нет идентификатора пользователя")
	}
	return &Profile{Subject: info.Sub, Email: strings.ToLower(info.Email), EmailVerified: info.EmailVerified, Name: info.Name}, nil
}

// ---------------------------------------------------------------- Apple

type Apple struct {
	ClientID    string // Services ID, например by.autoinvest.web
	TeamID      string
	KeyID       string
	PrivateKey  string // содержимое .p8 (PEM, PKCS#8)
	RedirectURL string
	AuthBase    string
	TokenURL    string
	Client      *http.Client
	now         func() time.Time
}

func (a *Apple) AuthURL(state string) string {
	base := a.AuthBase
	if base == "" {
		base = "https://appleid.apple.com/auth/authorize"
	}
	// имя и почту Apple отдаёт только при response_mode=form_post — ответ придёт POST-запросом на RedirectURL
	q := url.Values{"client_id": {a.ClientID}, "redirect_uri": {a.RedirectURL}, "response_type": {"code"},
		"scope": {"name email"}, "response_mode": {"form_post"}, "state": {state}}
	return base + "?" + q.Encode()
}

func (a *Apple) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

// ClientSecret — JWT ES256, которым Apple проверяет, что запрос от нас.
func (a *Apple) ClientSecret() (string, error) {
	block, _ := pem.Decode([]byte(a.PrivateKey))
	if block == nil {
		return "", errors.New("apple: ключ .p8 не похож на PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("apple: ключ .p8: %w", err)
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return "", errors.New("apple: нужен EC-ключ P-256")
	}
	now := a.clock()
	head, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": a.KeyID})
	claims, _ := json.Marshal(map[string]any{"iss": a.TeamID, "iat": now.Unix(), "exp": now.Add(10 * time.Minute).Unix(), "aud": "https://appleid.apple.com", "sub": a.ClientID})
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(head) + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + enc.EncodeToString(sig), nil
}

// Exchange меняет код на id_token. firstName/lastName Apple присылает только при первом входе (поле user формы).
func (a *Apple) Exchange(ctx context.Context, code, userJSON string) (*Profile, error) {
	secret, err := a.ClientSecret()
	if err != nil {
		return nil, err
	}
	tokenURL := a.TokenURL
	if tokenURL == "" {
		tokenURL = "https://appleid.apple.com/auth/token"
	}
	form := url.Values{"client_id": {a.ClientID}, "client_secret": {secret}, "code": {code}, "grant_type": {"authorization_code"}, "redirect_uri": {a.RedirectURL}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := doJSON(a.Client, req, &tok); err != nil {
		return nil, fmt.Errorf("apple token: %w", err)
	}
	claims, err := decodeJWT(tok.IDToken)
	if err != nil {
		return nil, err
	}
	if claims.Iss != "https://appleid.apple.com" || !claims.hasAud(a.ClientID) || claims.Exp < a.clock().Unix() || claims.Sub == "" {
		return nil, errors.New("apple: id_token не прошёл проверку")
	}
	p := &Profile{Subject: claims.Sub, Email: strings.ToLower(claims.Email), EmailVerified: claims.verified()}
	var u struct {
		Name struct {
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
		} `json:"name"`
	}
	if userJSON != "" && json.Unmarshal([]byte(userJSON), &u) == nil {
		p.Name = strings.TrimSpace(u.Name.FirstName + " " + u.Name.LastName)
	}
	return p, nil
}

type idClaims struct {
	Iss           string          `json:"iss"`
	Aud           json.RawMessage `json:"aud"`
	Exp           int64           `json:"exp"`
	Sub           string          `json:"sub"`
	Email         string          `json:"email"`
	EmailVerified any             `json:"email_verified"` // Apple присылает и "true", и true
}

func (c idClaims) hasAud(id string) bool {
	var one string
	if json.Unmarshal(c.Aud, &one) == nil {
		return one == id
	}
	var many []string
	if json.Unmarshal(c.Aud, &many) == nil {
		for _, a := range many {
			if a == id {
				return true
			}
		}
	}
	return false
}

func (c idClaims) verified() bool {
	switch v := c.EmailVerified.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}

func decodeJWT(t string) (idClaims, error) {
	var c idClaims
	parts := strings.Split(t, ".")
	if len(parts) != 3 {
		return c, errors.New("apple: неверный id_token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(raw, &c)
}

// verifyES256 — для тестов: проверка подписи client_secret.
func verifyES256(pub *ecdsa.PublicKey, token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		return false
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	return ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
}
