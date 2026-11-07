package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"autoinvest/internal/auth"
	"autoinvest/internal/store"
)

func (c *client) postForm(path string, form url.Values) *http.Response {
	req, _ := http.NewRequest("POST", c.base+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func (c *client) get(path string) *http.Response {
	resp, err := c.hc.Get(c.base + path)
	if err != nil {
		c.t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestOAuthSandboxLoginAndLink(t *testing.T) {
	srv, s := newServer(t)
	s.Cfg.OAuthGoogle, s.Cfg.OAuthApple = "sandbox", "off"
	c := newClient(t, srv.URL)
	if loc := c.get("/oauth/apple/start").Header.Get("Location"); !strings.Contains(loc, "oauth_error=off") {
		t.Fatalf("выключенный провайдер: %s", loc)
	}
	loc := c.get("/oauth/google/start?next=/app/plans").Header.Get("Location")
	if !strings.HasPrefix(loc, "/oauth-sandbox?provider=google&state=") {
		t.Fatalf("start: %s", loc)
	}
	state := strings.TrimPrefix(loc, "/oauth-sandbox?provider=google&state=")

	// чужой браузер без cookie с этим state не войдёт (защита от подстановки чужого входа)
	evil := newClient(t, srv.URL)
	if l := evil.postForm("/oauth/sandbox/callback", url.Values{"provider": {"google"}, "state": {state}, "email": {"g@test.by"}}).Header.Get("Location"); !strings.Contains(l, "oauth_error=state") {
		t.Fatalf("без cookie: %s", l)
	}
	// state одноразовый: попытка выше его не сожгла только потому, что cookie не совпала — проверяем честный вход
	resp := c.postForm("/oauth/sandbox/callback", url.Values{"provider": {"google"}, "state": {state}, "email": {"G@Test.by"}, "name": {"Гугл Тестов"}})
	if l := resp.Header.Get("Location"); l != "/app/plans" {
		t.Fatalf("callback: %d %s", resp.StatusCode, l)
	}
	var me struct{ User *store.User }
	c.do("GET", "/api/v1/auth/me", nil, &me)
	if me.User == nil || me.User.Email != "g@test.by" || me.User.HasPassword || !me.User.EmailVerified {
		t.Fatalf("me: %+v", me.User)
	}
	// у аккаунта без пароля можно задать пароль без текущего
	if code := c.do("POST", "/api/v1/me/password", map[string]any{"current": "", "new": "novyiparol1"}, nil); code != 200 {
		t.Fatalf("задать пароль: %d", code)
	}

	// существующий пользователь привязывает Google и затем входит им
	c2 := newClient(t, srv.URL)
	c2.register("link@test.by")
	loc = c2.get("/oauth/google/start?mode=link").Header.Get("Location")
	st2 := strings.TrimPrefix(loc, "/oauth-sandbox?provider=google&state=")
	if l := c2.postForm("/oauth/sandbox/callback", url.Values{"provider": {"google"}, "state": {st2}, "email": {"other@gmail.test"}}).Header.Get("Location"); l != "/app/profile/security?oauth=linked" {
		t.Fatalf("link: %s", l)
	}
	var ids struct{ Identities []store.Identity }
	c2.do("GET", "/api/v1/me/identities", nil, &ids)
	if len(ids.Identities) != 1 || ids.Identities[0].Provider != "google" {
		t.Fatalf("identities: %+v", ids)
	}
	c3 := newClient(t, srv.URL)
	st3 := strings.TrimPrefix(c3.get("/oauth/google/start").Header.Get("Location"), "/oauth-sandbox?provider=google&state=")
	c3.postForm("/oauth/sandbox/callback", url.Values{"provider": {"google"}, "state": {st3}, "email": {"other@gmail.test"}})
	c3.do("GET", "/api/v1/auth/me", nil, &me)
	if me.User == nil || me.User.Email != "link@test.by" {
		t.Fatalf("вход привязанным Google попал не в тот аккаунт: %+v", me.User)
	}
	// открытый редирект после входа не пропускаем
	if safeNext("//evil.com") != "/app" || safeNext("https://evil.com") != "/app" || safeNext("/app/cards") != "/app/cards" {
		t.Fatal("safeNext")
	}
}

func TestWithdrawNeedsAuthenticatorCode(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv.URL)
	c.register("totp@test.by")
	c.bindCard()
	id := c.plan("A", true)
	var setup struct{ Secret string }
	c.do("POST", "/api/v1/me/2fa/setup", map[string]any{}, &setup)
	if code := c.do("POST", "/api/v1/me/2fa/enable", map[string]any{"code": auth.TOTPCode(setup.Secret, time.Now())}, nil); code != 200 {
		t.Fatalf("2fa enable: %d", code)
	}
	var e apiErr
	if code := c.do("POST", fmt.Sprintf("/api/v1/plans/%d/withdraw", id), map[string]any{"all": true, "password": "parol2026"}, &e); code != 422 || e.Field != "code" {
		t.Fatalf("вывод без кода: %d %+v", code, e)
	}
	if code := c.do("POST", fmt.Sprintf("/api/v1/plans/%d/withdraw", id), map[string]any{"all": true, "password": "parol2026", "code": auth.TOTPCode(setup.Secret, time.Now().Add(30*time.Second))}, nil); code != 200 {
		t.Fatalf("вывод с кодом: %d", code)
	}
	// вход по паролю теперь требует второй шаг
	c2 := newClient(t, srv.URL)
	var r struct {
		MFARequired bool            `json:"mfa_required"`
		MFA         map[string]bool `json:"mfa"`
	}
	c2.do("POST", "/api/v1/auth/login", map[string]any{"email": "totp@test.by", "password": "parol2026"}, &r)
	if !r.MFARequired || !r.MFA["totp"] || r.MFA["passkey"] {
		t.Fatalf("второй шаг: %+v", r)
	}
}

func TestRegisterNeedsEmailCode(t *testing.T) {
	srv, s := newServer(t)
	c := newClient(t, srv.URL)
	var st struct {
		TestCode string `json:"test_code"`
	}
	c.do("POST", "/api/v1/auth/register/start", map[string]any{"name": "Анна", "email": "anna@test.by", "password": "parol2026", "accept_offer": true, "accept_privacy": true}, &st)
	if _, err := s.St.UserByEmail(context.Background(), "anna@test.by"); err == nil {
		t.Fatal("аккаунт создан до ввода кода")
	}
	wrong := "000000"
	if st.TestCode == wrong {
		wrong = "111111"
	}
	if code := c.do("POST", "/api/v1/auth/register/confirm", map[string]any{"email": "anna@test.by", "code": wrong}, nil); code != 422 {
		t.Fatalf("неверный код: %d", code)
	}
	var me struct{ User *store.User }
	if code := c.do("POST", "/api/v1/auth/register/confirm", map[string]any{"email": "anna@test.by", "code": st.TestCode}, &me); code != 201 || me.User == nil || !me.User.EmailVerified {
		t.Fatalf("подтверждение: %d %+v", code, me.User)
	}
	if code := c.do("POST", "/api/v1/auth/register/start", map[string]any{"name": "Анна", "email": "anna@test.by", "password": "parol2026", "accept_offer": true, "accept_privacy": true}, nil); code != 422 {
		t.Fatalf("повторная регистрация той же почты: %d", code)
	}
}

func TestPasskeyUserFallbackCode(t *testing.T) {
	srv, s := newServer(t)
	c := newClient(t, srv.URL)
	c.register("pk@test.by")
	u, _ := s.St.UserByEmail(context.Background(), "pk@test.by")
	s.St.AddPasskey(context.Background(), u.ID, "cred-1", "Mac · Safari", `{}`)
	c2 := newClient(t, srv.URL)
	var r struct {
		MFARequired bool            `json:"mfa_required"`
		MFA         map[string]bool `json:"mfa"`
	}
	c2.do("POST", "/api/v1/auth/login", map[string]any{"email": "pk@test.by", "password": "parol2026"}, &r)
	if !r.MFARequired || !r.MFA["passkey"] || !r.MFA["email"] || r.MFA["totp"] || r.MFA["telegram"] {
		t.Fatalf("методы: %+v", r)
	}
	var sent struct {
		TestCode string `json:"test_code"`
	}
	if code := c2.do("POST", "/api/v1/auth/login/code", map[string]any{"via": "telegram"}, nil); code != 400 {
		t.Fatalf("telegram без привязки: %d", code)
	}
	if code := c2.do("POST", "/api/v1/auth/login/code", map[string]any{"via": "email"}, &sent); code != 200 || len(sent.TestCode) != 6 {
		t.Fatalf("код на почту: %d %+v", code, sent)
	}
	if code := c2.do("POST", "/api/v1/auth/login/2fa", map[string]any{"code": "999999x"}, nil); code == 200 {
		t.Fatal("неверный код принят")
	}
	if code := c2.do("POST", "/api/v1/auth/login/2fa", map[string]any{"code": sent.TestCode}, nil); code != 200 {
		t.Fatalf("вход по коду: %d", code)
	}
}
