package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"autoinvest/internal/config"
	"autoinvest/internal/db"
	"autoinvest/internal/engine"
	"autoinvest/internal/market"
	"autoinvest/internal/notify"
	"autoinvest/internal/payments"
	"autoinvest/internal/store"
)

type client struct {
	t    *testing.T
	base string
	hc   *http.Client
}

func newServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.OpenPath(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(d)
	hub := market.NewFixtureHub(100, log)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	cfg := &config.Config{DataDir: dir, PublicURL: "http://test", Payments: "sandbox", Broker: "paper", Market: "fixture", FeeFreeBP: 100, FeeProBP: 50,
		SpreadBP: 100, MinContribKop: 500, MaxContribKop: 500000, FreePlanLimit: 3, ProPlanLimit: 20, ProMonthKop: 990, ProYearKop: 8900, SchedulerEvery: time.Hour}
	sb := payments.NewSandbox(cfg.PublicURL)
	n := notify.New(st, notify.NewMailer("", 0, "", "", "", log), cfg.PublicURL, log)
	en := &engine.Engine{St: st, DB: d, Hub: hub, Pay: sb, Broker: engine.PaperBroker{Hub: hub}, N: n, Cfg: cfg, Log: log}
	s := &Server{Cfg: cfg, St: st, DB: d, Hub: hub, En: en, Pay: sb, Sandbox: sb, N: n, Log: log,
		Static: fstest.MapFS{"index.html": {Data: []byte("<html></html>")}}, Version: "test", Started: time.Now()}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return srv, s
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	c := &client{t: t, base: base, hc: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	c.do("GET", "/api/v1/config", nil, nil)
	return c
}

func (c *client) csrf() string {
	u, _ := url.Parse(c.base)
	for _, ck := range c.hc.Jar.Cookies(u) {
		if ck.Name == csrfCookie {
			return ck.Value
		}
	}
	return ""
}

func (c *client) do(method, path string, body any, out any) int {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if method != "GET" {
		req.Header.Set("X-CSRF-Token", c.csrf())
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func (c *client) register(email string) {
	if code := c.do("POST", "/api/v1/auth/register", map[string]any{"name": "Иван Петров", "email": email, "password": "parol2026", "accept_offer": true, "accept_privacy": true}, nil); code != 201 {
		c.t.Fatalf("register %s: %d", email, code)
	}
}

func (c *client) bindCard() int64 {
	var b struct{ Ref string }
	c.do("POST", "/api/v1/cards/bind", map[string]any{}, &b)
	if code := c.do("POST", "/api/v1/sandbox/checkout/"+b.Ref, map[string]any{"number": "4200000000000000", "holder": "IVAN", "exp_month": 12, "exp_year": 2099, "cvc": "123"}, nil); code != 200 {
		c.t.Fatalf("sandbox pay %d", code)
	}
	var cards struct{ Cards []store.Card }
	c.do("GET", "/api/v1/cards", nil, &cards)
	return cards.Cards[0].ID
}

func (c *client) plan(name string, topup bool) int64 {
	var r struct{ Plan struct{ ID int64 } }
	code := c.do("POST", "/api/v1/plans", map[string]any{"name": name, "amount_kop": 2000, "frequency": "weekly", "weekday": 5, "strategy": "reserve", "first_topup": topup}, &r)
	if code != 201 {
		c.t.Fatalf("create plan %d", code)
	}
	return r.Plan.ID
}

func TestCSRFRequired(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv.URL)
	c.register("a@test.by")
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/plans", bytes.NewReader([]byte(`{}`)))
	resp, _ := c.hc.Do(req)
	if resp.StatusCode != 403 {
		t.Fatalf("no csrf: %d", resp.StatusCode)
	}
}

func TestIsolationBetweenUsers(t *testing.T) {
	srv, _ := newServer(t)
	a := newClient(t, srv.URL)
	a.register("a@test.by")
	cardA := a.bindCard()
	planA := a.plan("A", true)
	var ops struct{ Operations []store.Order }
	a.do("GET", "/api/v1/operations", nil, &ops)
	if len(ops.Operations) != 1 || ops.Operations[0].Status != "filled" {
		t.Fatalf("first topup: %+v", ops)
	}
	var tk struct{ Ticket struct{ ID int64 } }
	a.do("POST", "/api/v1/support/tickets", map[string]any{"subject": "Вопрос", "body": "Подробное описание вопроса"}, &tk)

	b := newClient(t, srv.URL)
	b.register("b@test.by")
	checks := []struct{ m, p string }{
		{"GET", fmt.Sprintf("/api/v1/plans/%d", planA)},
		{"POST", fmt.Sprintf("/api/v1/plans/%d/withdraw", planA)},
		{"POST", fmt.Sprintf("/api/v1/plans/%d/pause", planA)},
		{"GET", fmt.Sprintf("/api/v1/operations/%d", ops.Operations[0].ID)},
		{"GET", fmt.Sprintf("/api/v1/support/tickets/%d", tk.Ticket.ID)},
		{"DELETE", fmt.Sprintf("/api/v1/cards/%d", cardA)},
		{"POST", fmt.Sprintf("/api/v1/cards/%d/default", cardA)},
	}
	for _, ch := range checks {
		if code := b.do(ch.m, ch.p, map[string]any{"amount_kop": 500, "all": true}, nil); code != 404 {
			t.Errorf("%s %s by other user: %d", ch.m, ch.p, code)
		}
	}
	// карта A по-прежнему на месте
	var cards struct{ Cards []store.Card }
	a.do("GET", "/api/v1/cards", nil, &cards)
	if len(cards.Cards) != 1 {
		t.Fatal("card of A removed by B")
	}
	if code := b.do("GET", "/api/v1/admin/overview", nil, nil); code != 403 {
		t.Errorf("admin for user: %d", code)
	}
	anon := newClient(t, srv.URL)
	if code := anon.do("GET", "/api/v1/portfolio", nil, nil); code != 401 {
		t.Errorf("anon portfolio: %d", code)
	}
}

func TestPlanLimitAndPaidFeatures(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv.URL)
	c.register("a@test.by")
	c.bindCard()
	for i := 0; i < 3; i++ {
		c.plan(fmt.Sprint("P", i), false)
	}
	var e apiErr
	if code := c.do("POST", "/api/v1/plans", map[string]any{"name": "4", "amount_kop": 2000, "frequency": "weekly", "strategy": "reserve"}, &e); code != 409 || e.Code != "plan_limit" {
		t.Fatalf("limit: %d %+v", code, e)
	}
	// Pro снимает ограничение и открывает свой состав
	if code := c.do("POST", "/api/v1/billing/pro", map[string]any{"period": "month"}, nil); code != 200 {
		t.Fatalf("buy pro %d", code)
	}
	code := c.do("POST", "/api/v1/plans", map[string]any{"name": "Свой", "amount_kop": 2000, "frequency": "weekly", "strategy": "custom",
		"allocations": []map[string]any{{"asset": "BTC", "weight_bp": 6000}, {"asset": "ETH", "weight_bp": 3000}}}, &e)
	if code != 422 {
		t.Fatalf("90%% allocation accepted: %d", code)
	}
	code = c.do("POST", "/api/v1/plans", map[string]any{"name": "Свой", "amount_kop": 2000, "frequency": "weekly", "strategy": "custom",
		"allocations": []map[string]any{{"asset": "BTC", "weight_bp": 7000}, {"asset": "ETH", "weight_bp": 3000}}}, nil)
	if code != 201 {
		t.Fatalf("custom plan for pro: %d", code)
	}
}

func TestBlockedUserLoggedOut(t *testing.T) {
	srv, s := newServer(t)
	c := newClient(t, srv.URL)
	c.register("a@test.by")
	u, _ := s.St.UserByEmail(context.Background(), "a@test.by")
	s.St.UpdateUserFields(context.Background(), u.ID, map[string]any{"status": "blocked"})
	if code := c.do("GET", "/api/v1/portfolio", nil, nil); code != 401 {
		t.Fatalf("blocked user still in: %d", code)
	}
	if code := c.do("POST", "/api/v1/auth/login", map[string]any{"email": "a@test.by", "password": "parol2026"}, nil); code != 403 {
		t.Fatalf("blocked login: %d", code)
	}
}

func TestLoginRateLimit(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv.URL)
	c.register("a@test.by")
	last := 0
	for i := 0; i < 10; i++ {
		last = c.do("POST", "/api/v1/auth/login", map[string]any{"email": "a@test.by", "password": "wrong"}, nil)
	}
	if last != 429 {
		t.Fatalf("no rate limit: %d", last)
	}
}

func TestConfigRefusesRealMoneyWithPaperBroker(t *testing.T) {
	os.Setenv("PAYMENTS", "bepaid")
	os.Setenv("BEPAID_SHOP_ID", "1")
	os.Setenv("BEPAID_SECRET_KEY", "s")
	os.Setenv("BEPAID_TEST", "false")
	defer func() {
		for _, k := range []string{"PAYMENTS", "BEPAID_SHOP_ID", "BEPAID_SECRET_KEY", "BEPAID_TEST"} {
			os.Unsetenv(k)
		}
	}()
	if _, err := config.Load(); err == nil {
		t.Fatal("live bePaid with paper broker allowed")
	}
}

func TestSpoofedForwardedForIgnored(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if ip := clientIP(r); ip != "203.0.113.7" {
		t.Fatalf("spoofed XFF trusted from public remote: %s", ip)
	}
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 198.51.100.9")
	if ip := clientIP(r); ip != "198.51.100.9" {
		t.Fatalf("must take proxy-appended (last) address: %s", ip)
	}
}

func TestWithdrawNeedsPassword(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv.URL)
	c.register("a@test.by")
	c.bindCard()
	id := c.plan("A", true)
	var e apiErr
	if code := c.do("POST", fmt.Sprintf("/api/v1/plans/%d/withdraw", id), map[string]any{"all": true, "password": "wrong"}, &e); code != 422 || e.Field != "password" {
		t.Fatalf("withdraw with wrong password: %d %+v", code, e)
	}
	if code := c.do("POST", fmt.Sprintf("/api/v1/plans/%d/withdraw", id), map[string]any{"all": true, "password": "parol2026"}, nil); code != 200 {
		t.Fatalf("withdraw: %d", code)
	}
}
