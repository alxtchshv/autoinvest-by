package api

import (
	"context"
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
