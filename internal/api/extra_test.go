package api

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"autoinvest/internal/store"
)

func TestRegistrationNeedsBothSeparateConsentsAndStoresThem(t *testing.T) {
	srv, s := newServer(t)
	c := newClient(t, srv.URL)
	base := map[string]any{"name": "Иван Петров", "email": "c@test.by", "password": "parol2026"}
	for _, missing := range []string{"accept_offer", "accept_privacy"} {
		body := map[string]any{"accept_offer": true, "accept_privacy": true}
		for k, v := range base {
			body[k] = v
		}
		delete(body, missing)
		var e apiErr
		if code := c.do("POST", "/api/v1/auth/register", body, &e); code != 422 || e.Field != missing {
			t.Fatalf("without %s: code %d field %q", missing, code, e.Field)
		}
	}
	c.register("c@test.by")
	u, _ := s.St.UserByEmail(t.Context(), "c@test.by")
	consents, err := s.St.Consents(t.Context(), u.ID)
	if err != nil || len(consents) != 3 {
		t.Fatalf("expected offer, risks and privacy consents, got %+v %v", consents, err)
	}
	for _, x := range consents {
		if x.Version != LegalVersion || x.IP == "" {
			t.Fatalf("consent without version or IP: %+v", x)
		}
	}
}

func TestLoginHistoryRecordsDeviceAndRevokeAllLogsOut(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv.URL)
	c.register("h@test.by")
	c.do("POST", "/api/v1/auth/login", map[string]any{"email": "h@test.by", "password": "wrong-pass1"}, nil)

	var hist struct{ Events []store.AuditEntry }
	if code := c.do("GET", "/api/v1/me/logins", nil, &hist); code != 200 {
		t.Fatalf("logins: %d", code)
	}
	actions := map[string]bool{}
	for _, e := range hist.Events {
		actions[e.Action] = true
		if e.UserAgent == "" {
			t.Fatalf("event without device: %+v", e)
		}
	}
	if !actions["auth.register"] || !actions["auth.login_failed"] {
		t.Fatalf("history must contain registration and the failed attempt: %v", actions)
	}

	other := newClient(t, srv.URL)
	other.do("POST", "/api/v1/auth/login", map[string]any{"email": "h@test.by", "password": "parol2026"}, nil)
	if code := c.do("POST", "/api/v1/me/sessions/revoke-all", map[string]any{}, nil); code != 200 {
		t.Fatalf("revoke-all: %d", code)
	}
	for name, cl := range map[string]*client{"current": c, "other": other} {
		if code := cl.do("GET", "/api/v1/me/logins", nil, nil); code != 401 {
			t.Fatalf("%s device still logged in: %d", name, code)
		}
	}
}

func TestCardMaskNeverContainsFullNumber(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv.URL)
	c.register("m@test.by")
	c.bindCard()
	var cards struct{ Cards []store.Card }
	c.do("GET", "/api/v1/cards", nil, &cards)
	got := cards.Cards[0]
	if got.Mask != "4200 00•• •••• 0000" || got.BIN != "420000" {
		t.Fatalf("mask %q bin %q", got.Mask, got.BIN)
	}
}

func TestStatementShowsVenueAndRespectsPeriod(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv.URL)
	c.register("st@test.by")
	c.bindCard()
	c.plan("Подушка", true)

	var st struct {
		Operations []struct {
			Venue  string  `json:"venue"`
			USDBYN float64 `json:"usd_byn"`
		}
		LiveBroker bool `json:"live_broker"`
	}
	today := time.Now().Format("2006-01-02")
	if code := c.do("GET", "/api/v1/operations/statement?from="+today+"&to="+today, nil, &st); code != 200 || len(st.Operations) != 1 {
		t.Fatalf("statement: %d %+v", code, st)
	}
	if st.LiveBroker || st.Operations[0].Venue != venueLabel("paper") || st.Operations[0].USDBYN <= 0 {
		t.Fatalf("paper trade must be labelled honestly and carry the NBRB rate: %+v", st)
	}
	if code := c.do("GET", "/api/v1/operations/statement?from=2020-01-01&to=2020-01-31", nil, &st); code != 200 || len(st.Operations) != 0 {
		t.Fatalf("period filter ignored: %d %d", code, len(st.Operations))
	}
	if code := c.do("GET", "/api/v1/operations/statement?from=2020-02-01&to=2020-01-01", nil, nil); code != 400 {
		t.Fatalf("reversed period must be rejected: %d", code)
	}
}

func TestPersonalNumberChecks(t *testing.T) {
	born := time.Date(1995, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := map[string]bool{
		"3010195A001PB7": true,  // мужчина, 01.01.1995, Брестская обл.
		"3010195Z001PB7": false, // нет такой области
		"7010195A001PB7": false, // первая цифра вне 1–6
		"3020195A001PB7": false, // дата не совпадает
		"3013295A001PB7": false, // 32-й месяц
		"3010195a001pb7": false, // строчные буквы
	}
	for pn, ok := range cases {
		if got := checkPersonalNumber(pn, born) == ""; got != ok {
			t.Errorf("%s: valid=%v, want %v (%s)", pn, got, ok, checkPersonalNumber(pn, born))
		}
	}
}

func TestRecommendations(t *testing.T) {
	cases := []struct {
		answers  [4]int
		strategy string
	}{
		{[4]int{2, 1, 3, 3}, "reserve"},  // деньги нужны через полгода
		{[4]int{2, 3, 1, 3}, "reserve"},  // паника при просадке
		{[4]int{2, 3, 3, 1}, "reserve"},  // подушка безопасности
		{[4]int{2, 2, 2, 2}, "balanced"}, // середина
		{[4]int{3, 3, 3, 3}, "crypto"},   // долгий горизонт, докупает, пенсия
		{[4]int{3, 3, 2, 3}, "balanced"}, // готов лишь переждать падение — без крипто-роста
		{[4]int{3, 3, 3, 2}, "balanced"}, // крупная покупка к сроку
	}
	for _, c := range cases {
		r := Recommend(c.answers)
		if r.Strategy != c.strategy || len(r.Reasons) == 0 || r.AmountKop <= 0 {
			t.Errorf("%v: got %+v, want %s", c.answers, r, c.strategy)
		}
	}
	if Recommend([4]int{1, 3, 3, 3}).AmountKop >= Recommend([4]int{3, 3, 3, 3}).AmountKop {
		t.Error("amount must grow with the comfortable monthly sum")
	}
}

func TestOnboardingSavesProfile(t *testing.T) {
	srv, s := newServer(t)
	c := newClient(t, srv.URL)
	c.register("o@test.by")
	var out struct{ Recommendation Recommendation }
	if code := c.do("POST", "/api/v1/me/onboarding", map[string]any{"answers": []int{2, 3, 2, 3}}, &out); code != 200 {
		t.Fatalf("onboarding: %d", code)
	}
	u, _ := s.St.UserByEmail(t.Context(), "o@test.by")
	if u.OnboardedAt == nil || u.RiskProfile != out.Recommendation.Profile {
		t.Fatalf("profile not saved: %+v", u)
	}
	if code := c.do("POST", "/api/v1/me/onboarding", map[string]any{"answers": []int{2, 3}}, nil); code != 400 {
		t.Fatalf("partial answers must be rejected: %d", code)
	}
}

func TestPartnerMetricsFunnel(t *testing.T) {
	srv, s := newServer(t)
	a := newClient(t, srv.URL)
	a.register("f1@test.by")
	a.bindCard()
	a.plan("Подушка", true)
	b := newClient(t, srv.URL)
	b.register("f2@test.by")

	m, err := s.St.PartnerMetrics(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f := m.Funnel
	if f.Registered != 2 || f.WithCard != 1 || f.WithPlan != 1 || f.Invested != 1 {
		t.Fatalf("funnel %+v", f)
	}
	if len(m.AssetVolume30d) == 0 || len(m.Holdings) == 0 || len(m.Cohorts) != 1 || m.Cohorts[0].Invested != 1 {
		t.Fatalf("metrics %+v", m)
	}
}

func TestStaffNeeds2FAInProduction(t *testing.T) {
	srv, s := newServer(t)
	c := newClient(t, srv.URL)
	c.register("adm@test.by")
	u, _ := s.St.UserByEmail(t.Context(), "adm@test.by")
	s.St.UpdateUserFields(t.Context(), u.ID, map[string]any{"role": "admin"})
	if code := c.do("GET", "/api/v1/admin/metrics", nil, nil); code != 200 {
		t.Fatalf("development: %d", code)
	}
	s.Cfg.Env = "production"
	var e apiErr
	if code := c.do("GET", "/api/v1/admin/metrics", nil, &e); code != 403 || e.Code != "staff_2fa_required" {
		t.Fatalf("production without 2FA: %d %+v", code, e)
	}
}

func TestSandboxRunScheduleExecutesPlannedTopup(t *testing.T) {
	srv, s := newServer(t)
	c := newClient(t, srv.URL)
	c.register("run@test.by")
	c.bindCard()
	id := c.plan("Подушка", false)
	if code := c.do("POST", fmt.Sprintf("/api/v1/sandbox/plans/%d/run-schedule", id), map[string]any{}, nil); code != 200 {
		t.Fatalf("run-schedule: %d", code)
	}
	orders, _, _ := s.St.Orders(t.Context(), store.OrderFilter{PlanID: id})
	if len(orders) != 1 || orders[0].Trigger != "schedule" || orders[0].Status != "filled" {
		t.Fatalf("expected one filled scheduled order, got %+v", orders)
	}
	p, _ := s.St.PlanByID(t.Context(), id)
	if p.NextRunAt == nil || *p.NextRunAt <= time.Now().Unix() {
		t.Fatalf("next run must move to the future: %v", p.NextRunAt)
	}
	other := newClient(t, srv.URL)
	other.register("other@test.by")
	if code := other.do("POST", fmt.Sprintf("/api/v1/sandbox/plans/%d/run-schedule", id), map[string]any{}, nil); code != 404 {
		t.Fatalf("another user must not run someone else's plan: %d", code)
	}
	s.Cfg.Broker = "whitebird"
	if code := c.do("POST", fmt.Sprintf("/api/v1/sandbox/plans/%d/run-schedule", id), map[string]any{}, nil); code != 404 {
		t.Fatalf("must be unavailable outside the test stand: %d", code)
	}
}

func TestKYCViaMSISandbox(t *testing.T) {
	srv, s := newServer(t)
	s.Cfg.MSIMode, s.Cfg.KYCDocs = "sandbox", "sandbox"
	c := newClient(t, srv.URL)
	c.register("msi@test.by")
	// ручная анкета в этом режиме закрыта
	if code := c.do("POST", "/api/v1/kyc", map[string]any{}, nil); code != 409 {
		t.Fatalf("анкета: %d", code)
	}
	var st struct{ RedirectURL string `json:"redirect_url"` }
	c.do("POST", "/api/v1/kyc/msi/start", map[string]any{}, &st)
	state := strings.TrimPrefix(st.RedirectURL, "/verify/msi-sandbox?state=")
	if state == "" || state == st.RedirectURL {
		t.Fatalf("redirect %q", st.RedirectURL)
	}
	if code := c.do("POST", "/api/v1/kyc/msi/sandbox", map[string]any{"state": state, "personal_number": "3010195A001PB7", "code": "12"}, nil); code != 422 && code != 400 {
		t.Fatalf("короткий код принят: %d", code)
	}
	if code := c.do("POST", "/api/v1/kyc/msi/sandbox", map[string]any{"state": state, "personal_number": "3010195A001PB7", "code": "123456"}, nil); code != 200 {
		t.Fatalf("мси: %d", code)
	}
	var k struct{ KYC store.KYC `json:"kyc"` }
	c.do("GET", "/api/v1/kyc", nil, &k)
	if k.KYC.Status != "approved" || k.KYC.Method != "msi-sandbox" || k.KYC.BirthDate != "1995-01-01" || k.KYC.LastName != "Петров" {
		t.Fatalf("%+v", k.KYC)
	}
	// state одноразовый
	if code := c.do("POST", "/api/v1/kyc/msi/sandbox", map[string]any{"state": state, "personal_number": "3010195A001PB7", "code": "123456"}, nil); code == 200 {
		t.Fatal("повтор state принят")
	}
	// чужой state не подходит
	c2 := newClient(t, srv.URL)
	c2.register("other@test.by")
	var st2 struct{ RedirectURL string `json:"redirect_url"` }
	c2.do("POST", "/api/v1/kyc/msi/start", map[string]any{}, &st2)
	if code := c.do("POST", "/api/v1/kyc/msi/sandbox", map[string]any{"state": strings.TrimPrefix(st2.RedirectURL, "/verify/msi-sandbox?state="), "personal_number": "3020195A001PB7", "code": "123456"}, nil); code == 200 {
		t.Fatal("чужой state принят")
	}
}

func TestSumsubWebhookRejectsBadSignature(t *testing.T) {
	srv, s := newServer(t)
	s.Cfg.KYCDocs, s.Cfg.SumsubHook = "sumsub", "hook"
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/webhooks/sumsub", strings.NewReader(`{"type":"applicantReviewed","externalUserId":"ai-1","reviewResult":{"reviewAnswer":"GREEN"}}`))
	req.Header.Set("X-Payload-Digest", "deadbeef")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("без подписи: %d", resp.StatusCode)
	}
}

func TestAuditExportAndSessionRevoke(t *testing.T) {
	srv, _ := newServer(t)
	c := newClient(t, srv.URL)
	c.register("journal@test.by")
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/me/audit/export.csv", nil)
	resp, err := c.hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "Регистрация") || !strings.Contains(resp.Header.Get("Content-Disposition"), "autoinvest-journal.csv") {
		t.Fatalf("журнал: %d %q", resp.StatusCode, b)
	}
	// второй вход — второй сеанс; завершаем именно его
	c2 := newClient(t, srv.URL)
	if code := c2.do("POST", "/api/v1/auth/login", map[string]any{"email": "journal@test.by", "password": "parol2026"}, nil); code != 200 {
		t.Fatalf("вход: %d", code)
	}
	var ss struct{ Sessions []store.Session }
	c.do("GET", "/api/v1/me/sessions", nil, &ss)
	var other string
	for _, x := range ss.Sessions {
		if !x.Current {
			other = x.ID
		}
	}
	if len(ss.Sessions) != 2 || other == "" {
		t.Fatalf("сеансы: %+v", ss.Sessions)
	}
	c.do("DELETE", "/api/v1/me/sessions/"+other, nil, nil)
	if code := c2.do("GET", "/api/v1/plans", nil, nil); code != 401 {
		t.Fatalf("завершённый сеанс работает: %d", code)
	}
	if code := c.do("GET", "/api/v1/plans", nil, nil); code != 200 {
		t.Fatalf("текущий сеанс сломался: %d", code)
	}
}
