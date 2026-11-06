package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

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

func TestPhoneVerifyAndLogin(t *testing.T) {
	srv, s := newServer(t)
	s.Cfg.SMSProvider = "sandbox"
	c := newClient(t, srv.URL)
	c.register("phone@test.by")
	anon := newClient(t, srv.URL)
	// неподтверждённый номер для входа не годится
	if code := anon.do("POST", "/api/v1/auth/phone/code", map[string]any{"phone": "+375 29 111-22-33"}, nil); code != 422 {
		t.Fatalf("неподтверждённый номер: %d", code)
	}
	var sent struct {
		TestCode string `json:"test_code"`
	}
	if code := c.do("POST", "/api/v1/me/phone/code", map[string]any{"phone": "80291112233"}, &sent); code != 200 || len(sent.TestCode) != 6 {
		t.Fatalf("код подтверждения: %d %+v", code, sent)
	}
	if code := c.do("POST", "/api/v1/me/phone/verify", map[string]any{"phone": "+375291112233", "code": "000000"}, nil); code != 422 && sent.TestCode != "000000" {
		t.Fatalf("неверный код принят: %d", code)
	}
	if code := c.do("POST", "/api/v1/me/phone/verify", map[string]any{"phone": "+375291112233", "code": sent.TestCode}, nil); code != 200 {
		t.Fatalf("подтверждение: %d", code)
	}
	// вход по коду
	if code := anon.do("POST", "/api/v1/auth/phone/code", map[string]any{"phone": "+375291112233"}, &sent); code != 200 {
		t.Fatalf("код входа: %d", code)
	}
	if code := anon.do("POST", "/api/v1/auth/phone/login", map[string]any{"phone": "+375291112233", "code": sent.TestCode}, nil); code != 200 {
		t.Fatalf("вход по коду: %d", code)
	}
	// код одноразовый
	again := newClient(t, srv.URL)
	if code := again.do("POST", "/api/v1/auth/phone/login", map[string]any{"phone": "+375291112233", "code": sent.TestCode}, nil); code == 200 {
		t.Fatal("код сработал дважды")
	}
	// вход по номеру и паролю
	if code := again.do("POST", "/api/v1/auth/login", map[string]any{"email": "+375 (29) 111-22-33", "password": "parol2026"}, nil); code != 200 {
		t.Fatalf("вход по номеру и паролю: %d", code)
	}
	// второй аккаунт не может подтвердить тот же номер
	c2 := newClient(t, srv.URL)
	c2.register("phone2@test.by")
	if code := c2.do("POST", "/api/v1/me/phone/code", map[string]any{"phone": "+375291112233"}, nil); code != 422 {
		t.Fatalf("чужой номер: %d", code)
	}
}
