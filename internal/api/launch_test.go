package api

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"autoinvest/internal/store"
)

func TestWaitlistAndDemo(t *testing.T) {
	srv, s := newServer(t)
	s.Cfg.LaunchMode = "waitlist"
	c := newClient(t, srv.URL)
	c.do("POST", "/api/v1/track", map[string]any{"source": "instagram", "campaign": "nov-test"}, nil)
	c.register("lead@test.by")

	// настроил копилку — она ждёт запуска, человек в листе ожидания
	var cr struct {
		Plan struct {
			ID      int64
			Status  string
			Waiting bool
		}
		Waitlist struct{ Position int }
	}
	if code := c.do("POST", "/api/v1/plans", map[string]any{"name": "На отпуск", "amount_kop": 2000, "frequency": "weekly", "weekday": 5, "strategy": "balanced", "first_topup": true}, &cr); code != 201 {
		t.Fatalf("копилка: %d", code)
	}
	if !cr.Plan.Waiting || cr.Plan.Status != "paused" || cr.Waitlist.Position != 1 {
		t.Fatalf("ожидание: %+v", cr)
	}
	// в лист ожидания — уведомление про идеи и ошибки (окно листа ожидания остаётся коротким)
	var ns struct {
		Notifications []struct{ Title, Link string }
	}
	c.do("GET", "/api/v1/notifications", nil, &ns)
	fb := 0
	for _, n := range ns.Notifications {
		if n.Link == "/app/support?new=idea" {
			fb++
		}
	}
	if fb != 1 {
		t.Fatalf("уведомление про идеи: %d, %+v", fb, ns.Notifications)
	}
	// оплаты закрыты
	var e struct{ Code string }
	if code := c.do("POST", "/api/v1/cards/bind", map[string]any{}, &e); code != 409 || e.Code != "waitlist" {
		t.Fatalf("привязка карты до запуска: %d %+v", code, e)
	}

	// демо-счёт: своя карта, копия копилки, первое пополнение
	var me struct {
		User *store.User
		Demo bool
	}
	if code := c.do("POST", "/api/v1/demo/enter", map[string]any{}, &me); code != 200 || !me.Demo {
		t.Fatalf("вход в демо: %d %+v", code, me)
	}
	var pl struct{ Plans []planView }
	c.do("GET", "/api/v1/plans", nil, &pl)
	if len(pl.Plans) != 1 || pl.Plans[0].Status != "active" || pl.Plans[0].Valuation.InvestedKop <= 0 {
		t.Fatalf("демо-копилка: %+v", pl.Plans)
	}
	// в демо нельзя менять безопасность основного аккаунта
	if code := c.do("POST", "/api/v1/me/password", map[string]any{"current": "parol2026", "new": "novyiparol1"}, nil); code != 403 {
		t.Fatalf("смена пароля с демо: %d", code)
	}
	// идея с демо-счёта записывается на основной аккаунт
	if code := c.do("POST", "/api/v1/support/tickets", map[string]any{"subject": "Идея про цели", "category": "idea", "body": "Хочу видеть цель в процентах на главной"}, nil); code != 201 {
		t.Fatalf("идея: %d", code)
	}
	real, _ := s.St.UserByEmail(context.Background(), "lead@test.by")
	tickets, _ := s.St.Tickets(context.Background(), real.ID, "", 10)
	if len(tickets) != 1 || tickets[0].Category != "idea" {
		t.Fatalf("идея не у основного аккаунта: %+v", tickets)
	}

	// обратно в основной аккаунт: там по-прежнему копилка «ждёт запуска»
	c.do("POST", "/api/v1/demo/exit", map[string]any{}, &me)
	if me.Demo || me.User == nil || me.User.Email != "lead@test.by" {
		t.Fatalf("выход из демо: %+v", me)
	}
	c.do("GET", "/api/v1/plans", nil, &pl)
	if len(pl.Plans) != 1 || !pl.Plans[0].Waiting {
		t.Fatalf("основные копилки: %+v", pl.Plans)
	}

	// воронка: посещение, регистрация с меткой, лист ожидания, демо, идея; демо-пользователя нет в списке
	f, _ := s.St.LaunchFunnel(context.Background())
	if f.VisitsToday != 1 || f.Registered != 1 || f.Waitlist != 1 || f.DemoUsers != 1 || f.DemoInvested != 1 || f.Feedback != 1 {
		t.Fatalf("воронка: %+v", f)
	}
	list, total, _ := s.St.AdminUsers(context.Background(), "", 50, 0)
	for _, u := range list {
		if u.Role == "demo" {
			t.Fatal("демо-счёт в списке пользователей админки")
		}
	}
	_ = total
}

// Удалил аккаунт — и зашёл тем же Google заново: это новый аккаунт, а не «Удалённый пользователь»;
// демо-счёт, Face ID и место в листе ожидания ушли вместе с аккаунтом, воронка его не считает.
func TestDeleteAccountThenSignUpAgain(t *testing.T) {
	srv, s := newServer(t)
	s.Cfg.LaunchMode = "waitlist"
	s.Cfg.OAuthGoogle = "sandbox"
	ctx := context.Background()
	google := func(c *client, email string) string {
		st := strings.TrimPrefix(c.get("/oauth/google/start").Header.Get("Location"), "/oauth-sandbox?provider=google&state=")
		return c.postForm("/oauth/sandbox/callback", url.Values{"provider": {"google"}, "state": {st}, "email": {email}, "name": {"Тест Тестов"}}).Header.Get("Location")
	}
	c := newClient(t, srv.URL)
	c.register("again@test.by")
	loc := c.get("/oauth/google/start?mode=link").Header.Get("Location")
	c.postForm("/oauth/sandbox/callback", url.Values{"provider": {"google"}, "state": {strings.TrimPrefix(loc, "/oauth-sandbox?provider=google&state=")}, "email": {"again@gmail.test"}})
	c.do("POST", "/api/v1/plans", map[string]any{"name": "Отпуск", "amount_kop": 2000, "frequency": "weekly", "weekday": 5, "strategy": "balanced"}, nil)
	if code := c.do("POST", "/api/v1/demo/enter", map[string]any{}, nil); code != 200 {
		t.Fatalf("демо: %d", code)
	}
	c.do("POST", "/api/v1/demo/exit", map[string]any{}, nil)
	old, _ := s.St.UserByEmail(ctx, "again@test.by")
	s.St.AddPasskey(ctx, old.ID, "cred-del", "Mac", `{}`)

	if code := c.do("POST", "/api/v1/me/delete", map[string]any{"password": "parol2026"}, nil); code != 200 {
		t.Fatalf("удаление: %d", code)
	}
	if f, _ := s.St.LaunchFunnel(ctx); f.Registered != 0 || f.Waitlist != 0 || f.DemoUsers != 0 {
		t.Fatalf("удалённый аккаунт в воронке: %+v", f)
	}
	if n := s.St.CountPasskeys(ctx, old.ID); n != 0 {
		t.Fatalf("Face ID остался: %d", n)
	}

	c2 := newClient(t, srv.URL)
	if l := google(c2, "again@gmail.test"); strings.Contains(l, "oauth_error") || strings.Contains(l, "2fa") {
		t.Fatalf("вход Google после удаления: %s", l)
	}
	var me struct{ User *store.User }
	c2.do("GET", "/api/v1/auth/me", nil, &me)
	if me.User == nil || me.User.ID == old.ID || me.User.Status != "active" || me.User.WaitlistAt != nil {
		t.Fatalf("после удаления Google открыл не новый аккаунт: %+v", me.User)
	}
	// и почту можно занять заново
	newClient(t, srv.URL).register("again@test.by")
}

// Блокировка — с причиной, которую человек видит при входе (по паролю и через Google);
// удаление из админки — как самостоятельное: после него та же почта регистрируется заново.
func TestAdminBlockReasonAndDelete(t *testing.T) {
	srv, s := newServer(t)
	s.Cfg.OAuthGoogle = "sandbox"
	ctx := context.Background()
	adm := newClient(t, srv.URL)
	adm.register("boss@test.by")
	a, _ := s.St.UserByEmail(ctx, "boss@test.by")
	s.St.UpdateUserFields(ctx, a.ID, map[string]any{"role": "admin"})

	c := newClient(t, srv.URL)
	c.register("bad@test.by")
	loc := c.get("/oauth/google/start?mode=link").Header.Get("Location")
	c.postForm("/oauth/sandbox/callback", url.Values{"provider": {"google"}, "state": {strings.TrimPrefix(loc, "/oauth-sandbox?provider=google&state=")}, "email": {"bad@gmail.test"}})
	u, _ := s.St.UserByEmail(ctx, "bad@test.by")
	path := "/api/v1/admin/users/" + strconv.FormatInt(u.ID, 10)

	if code := adm.do("POST", path+"/status", map[string]any{"status": "blocked", "reason": "  "}, nil); code == 200 {
		t.Fatal("заблокировали без причины")
	}
	if code := adm.do("POST", path+"/status", map[string]any{"status": "blocked", "reason": "Подозрение на мошенничество."}, nil); code != 200 {
		t.Fatalf("блокировка: %d", code)
	}
	var e apiErr
	if code := newClient(t, srv.URL).do("POST", "/api/v1/auth/login", map[string]any{"email": "bad@test.by", "password": "parol2026"}, &e); code != 403 ||
		!strings.Contains(e.Error, "Причина: Подозрение на мошенничество.") || strings.Contains(e.Error, "..") {
		t.Fatalf("вход заблокированного: %d %q", code, e.Error)
	}
	g := newClient(t, srv.URL)
	st := strings.TrimPrefix(g.get("/oauth/google/start").Header.Get("Location"), "/oauth-sandbox?provider=google&state=")
	resp := g.postForm("/oauth/sandbox/callback", url.Values{"provider": {"google"}, "state": {st}, "email": {"bad@gmail.test"}})
	var reason string
	for _, ck := range resp.Cookies() {
		if ck.Name == "ai_block" {
			reason, _ = url.QueryUnescape(ck.Value)
		}
	}
	if !strings.Contains(resp.Header.Get("Location"), "oauth_error=blocked") || !strings.Contains(reason, "мошенничество") {
		t.Fatalf("Google заблокированного: %s %q", resp.Header.Get("Location"), reason)
	}

	// удаление: себя и сотрудников нельзя, пользователя — можно
	if code := adm.do("POST", "/api/v1/admin/users/"+strconv.FormatInt(a.ID, 10)+"/delete", map[string]any{}, nil); code == 200 {
		t.Fatal("админ удалил сам себя")
	}
	if code := adm.do("POST", path+"/delete", map[string]any{}, nil); code != 200 {
		t.Fatalf("удаление: %d", code)
	}
	if code := adm.do("POST", path+"/status", map[string]any{"status": "active"}, nil); code == 200 {
		t.Fatal("удалённый аккаунт разблокировали обратно")
	}
	if n := s.St.CountPasskeys(ctx, u.ID); n != 0 {
		t.Fatal("ключи остались")
	}
	newClient(t, srv.URL).register("bad@test.by")
}
