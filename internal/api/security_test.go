package api

// Проверки безопасности, которые должны держаться всегда (перед каждым выкладыванием — go test ./...).

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"autoinvest/internal/store"
)

type route struct{ guard, method, path string }

// routes читает server.go: каждый маршрут API и чем он защищён (pub | user | staff | admin).
func routesFromSource(t *testing.T) []route {
	t.Helper()
	b, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^\s*(pub|user|staff|admin)\("([A-Z]+) ([^"]+)"`)
	var out []route
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		out = append(out, route{m[1], m[2], m[3]})
	}
	// кроме этих обёрток маршруты регистрировать нельзя — иначе можно забыть проверку прав
	if n := len(regexp.MustCompile(`(?m)^\s*m\.Handle(Func)?\(`).FindAllString(string(b), -1)); n != 2 {
		t.Fatalf("в server.go %d прямых m.Handle/HandleFunc (ожидалось 2: /api/ и /) — новые маршруты только через pub/user/staff/admin", n)
	}
	if len(out) < 100 {
		t.Fatalf("распознано слишком мало маршрутов: %d", len(out))
	}
	return out
}

func fillPath(p string) string {
	r := strings.NewReplacer("{provider}", "google", "{symbol}", "BTC", "{kind}", "doc", "{ref}", "nope", "{id}", "1")
	return r.Replace(p)
}

// Публичных маршрутов ровно столько, сколько задумано: новый публичный маршрут — только осознанно, с правкой этого списка.
var publicRoutes = map[string]bool{
	"GET /healthz": true, "GET /readyz": true, "GET /api/v1/config": true, "POST /api/v1/track": true,
	"POST /api/v1/auth/register/start": true, "POST /api/v1/auth/register/confirm": true, "POST /api/v1/auth/login": true,
	"POST /api/v1/auth/login/2fa": true, "POST /api/v1/auth/login/code": true, "POST /api/v1/auth/login/passkey/begin": true,
	"POST /api/v1/auth/login/passkey/finish": true, "POST /api/v1/auth/logout": true, "GET /api/v1/auth/me": true,
	"POST /api/v1/auth/verify-email": true, "POST /api/v1/auth/forgot": true, "POST /api/v1/auth/reset": true,
	"POST /api/v1/auth/phone/code": true, "POST /api/v1/auth/phone/login": true, "GET /oauth/{provider}/start": true,
	"GET /oauth/google/callback": true, "POST /oauth/apple/callback": true, "POST /oauth/sandbox/callback": true,
	"GET /pay/return": true, "POST /api/v1/webhooks/bepaid": true, "POST /api/v1/webhooks/sumsub": true,
	"GET /api/v1/market": true, "GET /api/v1/market/assets/{symbol}": true, "GET /api/v1/market/candles": true,
	"GET /api/v1/stream": true, "POST /api/v1/calculator/backtest": true, "GET /api/v1/calculator/showcase": true,
}

// Каждый закрытый маршрут: гость — 401; обычный человек к админке — 403; поддержка к действиям админа — 403.
func TestEveryRouteChecksAccess(t *testing.T) {
	srv, s := newServer(t)
	ctx := context.Background()
	anon := newClient(t, srv.URL)
	anon.get("/api/v1/config") // получить CSRF-cookie

	usr := newClient(t, srv.URL)
	usr.register("plain@test.by")
	sup := newClient(t, srv.URL)
	sup.register("helper@test.by")
	su, _ := s.St.UserByEmail(ctx, "helper@test.by")
	s.St.UpdateUserFields(ctx, su.ID, map[string]any{"role": "support"})

	seenPub := map[string]bool{}
	for _, r := range routesFromSource(t) {
		key := r.method + " " + r.path
		p := fillPath(r.path)
		if r.guard == "pub" {
			if !publicRoutes[key] {
				t.Errorf("новый публичный маршрут %s — проверьте, что он не отдаёт чужие данные, и добавьте в publicRoutes", key)
			}
			seenPub[key] = true
			continue
		}
		if publicRoutes[key] {
			t.Errorf("%s в списке публичных, но закрыт — уберите из publicRoutes", key)
		}
		if code := anon.do(r.method, p, map[string]any{}, nil); code != http.StatusUnauthorized {
			t.Errorf("гость → %s: %d, нужно 401", key, code)
		}
		if r.guard == "staff" || r.guard == "admin" {
			if code := usr.do(r.method, p, map[string]any{}, nil); code != http.StatusForbidden {
				t.Errorf("обычный пользователь → %s: %d, нужно 403", key, code)
			}
		}
		if r.guard == "admin" {
			if code := sup.do(r.method, p, map[string]any{}, nil); code != http.StatusForbidden {
				t.Errorf("поддержка → %s (только админ): %d, нужно 403", key, code)
			}
		}
	}
	for k := range publicRoutes {
		if !seenPub[k] {
			t.Errorf("в publicRoutes есть %s, а в server.go его нет", k)
		}
	}
	// выдача Pro, роли, блокировка, удаление, рассылка — только админ (проверено выше); убедимся, что они в списке admin
	must := []string{"POST /api/v1/admin/users/{id}/pro", "POST /api/v1/admin/users/{id}/role", "POST /api/v1/admin/users/{id}/status",
		"POST /api/v1/admin/users/{id}/delete", "POST /api/v1/admin/users/{id}/bonus", "POST /api/v1/admin/launch/announce"}
	guards := map[string]string{}
	for _, r := range routesFromSource(t) {
		guards[r.method+" "+r.path] = r.guard
	}
	for _, k := range must {
		if guards[k] != "admin" {
			t.Errorf("%s защищён как %q, нужно admin", k, guards[k])
		}
	}
}

// Пути вида ../../etc/passwd: сервер не отдаёт ничего, кроме собранного фронтенда.
func TestPathTraversal(t *testing.T) {
	srv, _ := newServer(t)
	for _, p := range []string{"/../../etc/passwd", "/..%2f..%2fetc%2fpasswd", "/%2e%2e/%2e%2e/etc/passwd", "/assets/..%2f..%2f..%2fetc%2fpasswd",
		"/....//....//etc/passwd", "/%252e%252e/etc/passwd", "/.env", "/../.env", "/autoinvest.db", "/data/autoinvest.db", "/.git/config", "/go.mod",
		"/api/v1/admin/kyc/1/file/..%2f..%2fautoinvest.db", "/api/v1/market/assets/..%2f..%2fetc%2fpasswd"} {
		req, _ := http.NewRequest("GET", srv.URL+p, nil)
		req.URL.Opaque = p // отправить путь как есть, без нормализации клиентом
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), "root:") || strings.Contains(string(body), "SQLite format") || strings.Contains(string(body), "module ") ||
			strings.Contains(string(body), "[core]") || strings.Contains(string(body), "TELEGRAM_BOT_TOKEN") {
			t.Errorf("%s отдал содержимое файла", p)
		}
	}
}

// Один IP не может засыпать API запросами: после всплеска — 429.
func TestThrottle(t *testing.T) {
	srv, s := newServer(t)
	s.RatePerIP = 0 // боевые лимиты
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	_ = srv
	limited := false
	for i := 0; i < 200 && !limited; i++ {
		resp, err := http.Get(h.URL + "/api/v1/config")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		limited = resp.StatusCode == http.StatusTooManyRequests
	}
	if !limited {
		t.Fatal("200 запросов подряд прошли без ограничения")
	}
	// проверка здоровья сервера под лимит не попадает (мониторинг и Docker)
	if resp, _ := http.Get(h.URL + "/healthz"); resp.StatusCode != 200 {
		t.Fatalf("healthz под лимитом: %d", resp.StatusCode)
	}
}

// Один человек — один аккаунт: варианты одного ящика Gmail и один Telegram на два аккаунта не проходят.
func TestOneAccountPerPerson(t *testing.T) {
	srv, s := newServer(t)
	ctx := context.Background()
	c := newClient(t, srv.URL)
	c.register("ivan.petrov@gmail.com")
	for _, dup := range []string{"ivanpetrov@gmail.com", "Ivan.Petrov+promo@gmail.com", "i.v.a.n.petrov@googlemail.com"} {
		var e apiErr
		code := newClient(t, srv.URL).do("POST", "/api/v1/auth/register/start", map[string]any{"name": "Двойник", "email": dup, "password": "parol2026dlinnyi",
			"accept_offer": true, "accept_privacy": true}, &e)
		if code == 200 {
			t.Errorf("второй аккаунт на тот же ящик: %s", dup)
		}
	}
	// вход по варианту адреса попадает в тот же аккаунт
	if code := newClient(t, srv.URL).do("POST", "/api/v1/auth/login", map[string]any{"email": "IvanPetrov+x@gmail.com", "password": "parol2026"}, nil); code != 200 {
		t.Errorf("вход по варианту адреса Gmail: %d", code)
	}
	// один Telegram — один аккаунт (уникальный индекс в базе)
	c2 := newClient(t, srv.URL)
	c2.register("other@test.by")
	a, _ := s.St.UserByEmail(ctx, "ivan.petrov@gmail.com")
	b, _ := s.St.UserByEmail(ctx, "other@test.by")
	if err := s.St.UpdateUserFields(ctx, a.ID, map[string]any{"telegram_chat_id": int64(777)}); err != nil {
		t.Fatal(err)
	}
	if err := s.St.UpdateUserFields(ctx, b.ID, map[string]any{"telegram_chat_id": int64(777)}); err == nil {
		t.Error("один Telegram подключился ко второму аккаунту")
	}
	if id, _ := s.St.TelegramOwner(ctx, 777); id != a.ID {
		t.Errorf("владелец Telegram: %d", id)
	}
}

// Даже если базу украдут, секреты в ней не читаются без DATA_KEY: шифр вместо значений, хэши вместо кодов.
func TestSecretsEncryptedAtRest(t *testing.T) {
	srv, s := newServer(t)
	ctx := context.Background()
	c := newClient(t, srv.URL)
	c.register("enc@test.by")
	c.bindCard()
	u, _ := s.St.UserByEmail(ctx, "enc@test.by")
	s.St.UpdateUserFields(ctx, u.ID, map[string]any{"totp_secret": "JBSWY3DPEHPK3PXP"})
	s.St.CreateTelegramLink(ctx, u.ID, "plaincode123", 600)
	s.St.SubmitKYC(ctx, store.KYC{UserID: u.ID, LastName: "Иванов", FirstName: "Иван", BirthDate: "1990-01-02",
		PersonalNumber: "3020190A001PB1", DocNumber: "MP1234567", Citizenship: "BY", Address: "Минск, ул. Тестовая, 1"})

	raw := func(q string) string {
		var v string
		s.DB.QueryRowContext(ctx, q, u.ID).Scan(&v)
		return v
	}
	checks := map[string]string{
		"пароль":                      `SELECT password_hash FROM users WHERE id=?`,
		"секрет Google Authenticator": `SELECT totp_secret FROM users WHERE id=?`,
		"токен карты":                 `SELECT token FROM cards WHERE user_id=?`,
		"ссылка Telegram":             `SELECT code FROM telegram_links WHERE user_id=?`,
		"идентификационный номер":     `SELECT personal_number FROM kyc WHERE user_id=?`,
		"номер паспорта":              `SELECT doc_number FROM kyc WHERE user_id=?`,
		"адрес":                       `SELECT address FROM kyc WHERE user_id=?`,
	}
	plain := []string{"parol2026", "JBSWY3DPEHPK3PXP", "sbx_ok", "plaincode123", "3020190A001PB1", "MP1234567", "Тестовая"}
	for name, q := range checks {
		v := raw(q)
		if v == "" {
			t.Errorf("%s: пусто", name)
		}
		for _, p := range plain {
			if strings.Contains(v, p) {
				t.Errorf("%s лежит в базе открытым текстом", name)
			}
		}
	}
	// в базе нет полного номера карты нигде
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM cards WHERE token LIKE '%4200000000000000%' OR last4 LIKE '%420000000%'`).Scan(&n)
	if n != 0 {
		t.Error("полный номер карты в базе")
	}
	// а приложение читает всё как раньше
	u2, _ := s.St.UserByID(ctx, u.ID)
	k, _ := s.St.KYC(ctx, u.ID)
	cards, _ := s.St.Cards(ctx, u.ID)
	if u2.TOTPSecret != "JBSWY3DPEHPK3PXP" || k.PersonalNumber != "3020190A001PB1" || k.DocNumber != "MP1234567" ||
		!strings.Contains(k.Address, "Тестовая") || len(cards) != 1 || !strings.HasPrefix(cards[0].Token, "sbx_ok") {
		t.Fatalf("расшифровка: totp=%q pn=%q doc=%q card=%+v", u2.TOTPSecret, k.PersonalNumber, k.DocNumber, cards)
	}
	if uid, err := s.St.UseTelegramLink(ctx, "plaincode123"); err != nil || uid != u.ID {
		t.Fatalf("ссылка Telegram по хэшу: %d %v", uid, err)
	}
	_ = time.Now
}
