package api

// Запуск через лист ожидания (LAUNCH_MODE=waitlist): человек из рекламы регистрируется, проходит анкету,
// настраивает копилку — и на шаге оплаты попадает в лист ожидания. Сразу же может попробовать всё на демо-счёте:
// это отдельный «теневой» пользователь (роль demo) с виртуальными деньгами, копилками и картой.
// Настоящий аккаунт и демо-счёт никогда не смешиваются; в статистику демо не попадает.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"autoinvest/internal/auth"
	"autoinvest/internal/engine"
	"autoinvest/internal/market"
	"autoinvest/internal/notify"
	"autoinvest/internal/store"
)

const mainCookie = "ai_main" // сеанс основного аккаунта, пока человек на демо-счёте

func (s *Server) waitlistMode() bool { return s.Cfg.LaunchMode == "waitlist" }

// waitlistBlock — для настоящего аккаунта до запуска оплаты закрыты: ставим в лист ожидания и отвечаем code=waitlist.
func (s *Server) waitlistBlock(w http.ResponseWriter, r *http.Request) bool {
	u := currentUser(r)
	if !s.waitlistMode() || u == nil || u.IsDemo() {
		return false
	}
	pos, _ := s.joinWaitlist(r.Context(), u)
	writeJSON(w, http.StatusConflict, map[string]any{"error": "Пополнения откроются совсем скоро — вы в списке первых", "code": "waitlist", "position": pos})
	return true
}

// paid — обёртка для действий с деньгами: до запуска настоящий аккаунт попадает в лист ожидания.
func (s *Server) paid(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.waitlistBlock(w, r) {
			return
		}
		h(w, r)
	}
}

// realUserID — для демо-счёта это основной аккаунт: обращения в поддержку и отзывы — от настоящего человека.
func realUserID(u *store.User) int64 {
	if u.DemoOf != nil {
		return *u.DemoOf
	}
	return u.ID
}

// demoDenied — что нельзя делать с демо-счёта: настройки безопасности и личные данные живут в основном аккаунте.
var demoDenied = []string{"/api/v1/me/password", "/api/v1/me/2fa", "/api/v1/me/identities", "/api/v1/me/phone", "/api/v1/me/passkeys",
	"/api/v1/me/stepup", "/api/v1/me/delete", "/api/v1/me/sessions", "/api/v1/me/export", "/api/v1/me/audit", "/api/v1/kyc",
	"/api/v1/telegram/link", "/api/v1/billing", "/api/v1/referral"}

func (s *Server) demoGuard(w http.ResponseWriter, r *http.Request) bool {
	u := currentUser(r)
	if u == nil || !u.IsDemo() {
		return false
	}
	for _, p := range demoDenied {
		if strings.HasPrefix(r.URL.Path, p) && (r.Method != http.MethodGet || p != "/api/v1/billing") {
			writeJSON(w, http.StatusForbidden, apiErr{Error: "На демо-счёте это недоступно — вернитесь в основной аккаунт", Code: "demo"})
			return true
		}
	}
	return false
}

// GET /waitlist — место в очереди.
func (s *Server) waitlistInfo(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	pos, _ := s.St.WaitlistPosition(r.Context(), realUserID(u))
	writeJSON(w, 200, map[string]any{"mode": s.Cfg.LaunchMode, "joined": pos > 0, "position": pos})
}

// POST /waitlist/join
func (s *Server) waitlistJoin(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	real := u
	if u.IsDemo() {
		real, _ = s.St.UserByID(r.Context(), *u.DemoOf)
	}
	if real == nil {
		s.internal(w, r, errors.New("нет основного аккаунта"))
		return
	}
	pos, err := s.joinWaitlist(r.Context(), real)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"joined": true, "position": pos})
}

// joinWaitlist — в лист ожидания; при первом попадании — уведомления: идеи и ошибки (за полезные — Pro),
// и, если Telegram ещё не подключён, предложение подключить его. Окно листа ожидания остаётся коротким.
func (s *Server) joinWaitlist(ctx context.Context, u *store.User) (int, error) {
	pos, first, err := s.St.JoinWaitlistFirst(ctx, u.ID)
	if err != nil || !first {
		return pos, err
	}
	s.N.Send(ctx, u.ID, notify.Message{Kind: "support", Title: "Помогите сделать AutoInvest лучше",
		Body: "Нашли ошибку или есть идея? Напишите нам в поддержку — за полезные предложения добавим дни Pro к запуску.",
		Link: "/app/support?new=idea"})
	if u.TelegramChatID == nil && s.Bot != nil {
		s.N.Send(ctx, u.ID, notify.Message{Kind: "billing", Title: "Узнайте о запуске первыми",
			Body: "Подключите Telegram — пришлём новость о запуске и коды входа прямо в чат.",
			Link: "/app/profile/notifications"})
	}
	return pos, nil
}

// POST /demo/enter — переход на демо-счёт. При первом входе: демо-карта, копии настроенных копилок и первое пополнение,
// чтобы сразу было что посмотреть.
func (s *Server) demoEnter(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.IsDemo() {
		writeJSON(w, 200, s.mePayload(r, u))
		return
	}
	if !s.waitlistMode() || s.Sandbox == nil {
		writeErr(w, http.StatusNotFound, "Демо-счёт сейчас недоступен")
		return
	}
	d, created, err := s.St.DemoUser(r.Context(), u)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if created {
		s.seedDemo(r.Context(), u, d)
	}
	// запоминаем сеанс основного аккаунта и открываем сеанс демо-счёта
	if c, err := r.Cookie(sessionCookie); err == nil {
		http.SetCookie(w, &http.Cookie{Name: mainCookie, Value: c.Value, Path: "/", HttpOnly: true, Secure: s.Cfg.SecureCookies,
			SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL / time.Second)})
	}
	if err := s.setSession(w, r, d, false); err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "demo.enter", "", clientIP(r))
	writeJSON(w, 200, s.mePayload(r, d))
}

// POST /demo/exit — обратно в основной аккаунт.
func (s *Server) demoExit(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !u.IsDemo() {
		writeJSON(w, 200, s.mePayload(r, u))
		return
	}
	c, err := r.Cookie(mainCookie)
	var main *store.User
	if err == nil && c.Value != "" {
		if sess, err := s.St.SessionByHash(r.Context(), auth.HashToken(c.Value)); err == nil && sess.UserID == *u.DemoOf && !sess.MFAPending {
			main, _ = s.St.UserByID(r.Context(), sess.UserID)
		}
	}
	if sess := currentSession(r); sess != nil {
		s.St.DeleteSession(r.Context(), sess.ID)
	}
	http.SetCookie(w, &http.Cookie{Name: mainCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	if main == nil {
		s.clearSession(w)
		writeJSON(w, 200, map[string]any{"user": nil})
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: c.Value, Path: "/", HttpOnly: true, Secure: s.Cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL / time.Second)})
	writeJSON(w, 200, s.mePayload(r, main))
}

// seedDemo — демо-карта и копии копилок, которые человек настроил в основном аккаунте, с первым пополнением.
func (s *Server) seedDemo(ctx context.Context, real, d *store.User) {
	card, err := s.St.AddCard(ctx, store.Card{UserID: d.ID, Provider: "sandbox", Token: "sbx_ok_demo" + strings.ToLower(auth.RandomCode(16, "")),
		Brand: "visa", Last4: "0000", ExpMonth: 12, ExpYear: time.Now().Year() + 3, Holder: "DEMO", Issuer: "Демо-карта", BIN: "420000", IsDefault: true, Status: "active"})
	if err != nil {
		s.Log.Error("demo: карта", "err", err)
		return
	}
	plans, _ := s.St.Plans(ctx, real.ID, false)
	if len(plans) == 0 {
		plans = []*store.Plan{{Name: "Первая копилка", Icon: "piggy", Strategy: "balanced", AmountKop: 2000, Frequency: "weekly", Weekday: 5, Monthday: 1, RunMinute: 600}}
	}
	for _, p := range plans {
		cp := *p
		cp.ID, cp.UserID, cp.CardID, cp.Status, cp.Positions = 0, d.ID, &card.ID, "active", nil
		if cp.Frequency == "manual" {
			cp.Frequency = "weekly"
		}
		cp.NextRunAt = engine.NextRun(&cp, time.Now(), false)
		if len(cp.Allocations) == 0 {
			if st, ok := market.FindStrategy(cp.Strategy); ok {
				for a, wbp := range st.Weights {
					cp.Allocations = append(cp.Allocations, store.Alloc{Asset: a, WeightBP: wbp})
				}
			}
		}
		if err := s.St.CreatePlan(ctx, &cp); err != nil {
			s.Log.Error("demo: копилка", "err", err)
			continue
		}
		if _, err := s.En.Buy(ctx, cp.ID, cp.AmountKop, "manual", fmt.Sprintf("demo_first_%d", cp.ID)); err != nil {
			s.Log.Warn("demo: первое пополнение", "err", err)
		}
	}
	msg := notify.Message{Kind: "welcome", Title: "Это ваш демо-счёт",
		Body: "Деньги здесь виртуальные, а цены — настоящие. Пополняйте, выводите, меняйте план — как будет после запуска. " +
			"Нашли ошибку или есть идея? Напишите в «Поддержку» → «Идея» или «Ошибка»: за полезные подарим дополнительное время Pro при запуске.",
		Link: "/app/support"}
	s.N.Send(ctx, d.ID, msg)
	if real.TelegramChatID != nil && s.Bot != nil {
		go s.Bot.Send(*real.TelegramChatID, "<b>Демо-счёт открыт</b>\n"+msg.Body)
	}
}

// POST /track {source, campaign} — посещение сайта (раз в день с браузера), для оценки рекламы. Без персональных данных.
func (s *Server) track(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source   string `json:"source"`
		Campaign string `json:"campaign"`
	}
	readJSON(w, r, &in)
	if s.limiter.Allow("track:"+clientIP(r), 30, time.Hour) {
		s.St.TrackVisit(r.Context(), cleanTag(in.Source), cleanTag(in.Campaign))
	}
	w.WriteHeader(http.StatusNoContent)
}

// cleanTag — метка рекламы: латиница, цифры, - _ . не длиннее 60 символов.
func cleanTag(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	var b strings.Builder
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
		if b.Len() >= 60 {
			break
		}
	}
	return b.String()
}

// GET /admin/launch — воронка запуска для админки.
func (s *Server) adminLaunch(w http.ResponseWriter, r *http.Request) {
	f, err := s.St.LaunchFunnel(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if f.Sources == nil {
		f.Sources = []store.LaunchSource{}
	}
	writeJSON(w, 200, map[string]any{"funnel": f, "mode": s.Cfg.LaunchMode})
}

// POST /admin/users/{id}/bonus {days, reason} — подарить дни Pro к запуску за полезную идею или найденную ошибку.
func (s *Server) adminBonus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Days   int    `json:"days"`
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Days < 1 || in.Days > 90 {
		writeFieldErr(w, "days", "От 1 до 90 дней")
		return
	}
	id := pathID(r)
	total, err := s.St.AddBonusProDays(r.Context(), id, in.Days)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	staff := currentUser(r)
	s.St.Audit(r.Context(), id, staff.ID, "launch.bonus", fmt.Sprintf("+%d дн. Pro: %s", in.Days, truncate(in.Reason, 100)), clientIP(r))
	s.N.Send(r.Context(), id, notify.Message{Kind: "billing", Title: fmt.Sprintf("Спасибо! +%d дней Pro при запуске", in.Days),
		Body: fmt.Sprintf("Ваше предложение помогает сделать AutoInvest лучше. Всего в подарок к запуску: %d дней Pro.", total), Link: "/app/support"})
	writeJSON(w, 200, map[string]any{"bonus_pro_days": total})
}
