// Package api — HTTP API (JSON) и раздача фронтенда.
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"autoinvest/internal/auth"
	"autoinvest/internal/config"
	"autoinvest/internal/engine"
	"autoinvest/internal/market"
	"autoinvest/internal/notify"
	"autoinvest/internal/payments"
	"autoinvest/internal/store"
	"autoinvest/internal/telegram"
)

const (
	sessionCookie = "ai_session"
	csrfCookie    = "ai_csrf"
	sessionTTL    = 30 * 24 * time.Hour
)

type Server struct {
	Cfg     *config.Config
	St      *store.Store
	DB      *sql.DB
	Hub     *market.Hub
	En      *engine.Engine
	Pay     payments.Provider
	Sandbox *payments.Sandbox
	N       *notify.Notifier
	Bot     *telegram.Bot
	TG      *telegram.Sandbox // тестовый Telegram (только при разработке)
	Log     *slog.Logger
	Static  fs.FS
	Version string
	Started time.Time

	limiter *limiter
	statsMu sync.Mutex
	stats   map[string]statsEntry
	showMu  sync.Mutex
	show    map[string]showcaseEntry
}

type showcaseEntry struct {
	at    time.Time
	items []engine.ShowcaseItem
}

type statsEntry struct {
	at    time.Time
	stats market.Stats
}

type ctxKey int

const (
	keyUser ctxKey = iota
	keySession
)

func (s *Server) Handler() http.Handler {
	s.limiter = newLimiter()
	s.stats = map[string]statsEntry{}
	mux := http.NewServeMux()
	s.routes(mux)
	return s.recoverer(s.securityHeaders(s.logRequests(s.withSession(s.csrf(mux)))))
}

func (s *Server) routes(m *http.ServeMux) {
	pub := func(p string, h http.HandlerFunc) { m.HandleFunc(p, h) }
	user := func(p string, h http.HandlerFunc) { m.HandleFunc(p, s.requireUser(h)) }
	staff := func(p string, h http.HandlerFunc) { m.HandleFunc(p, s.requireStaff(h, false)) }
	admin := func(p string, h http.HandlerFunc) { m.HandleFunc(p, s.requireStaff(h, true)) }

	pub("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	pub("GET /readyz", s.readyz)
	pub("GET /api/v1/config", s.getConfig)

	// авторизация
	pub("POST /api/v1/auth/register/start", s.registerStart)
	pub("POST /api/v1/auth/register/confirm", s.registerConfirm)
	pub("POST /api/v1/auth/login", s.login)
	pub("POST /api/v1/auth/login/2fa", s.login2FA)
	pub("POST /api/v1/auth/login/code", s.loginSendCode)
	pub("POST /api/v1/auth/login/passkey/begin", s.loginPasskeyBegin)
	pub("POST /api/v1/auth/login/passkey/finish", s.loginPasskeyFinish)
	pub("POST /api/v1/auth/logout", s.logout)
	pub("GET /api/v1/auth/me", s.me)
	pub("POST /api/v1/auth/verify-email", s.verifyEmail)
	pub("POST /api/v1/auth/forgot", s.forgot)
	pub("POST /api/v1/auth/reset", s.reset)
	pub("POST /api/v1/auth/phone/code", s.phoneLoginCode)
	pub("POST /api/v1/auth/phone/login", s.phoneLogin)
	pub("GET /oauth/{provider}/start", s.oauthStart)
	pub("GET /oauth/google/callback", s.oauthGoogleCallback)
	pub("POST /oauth/apple/callback", s.oauthAppleCallback)
	pub("POST /oauth/sandbox/callback", s.oauthSandboxCallback)
	user("POST /api/v1/auth/resend-verification", s.resendVerification)

	// профиль и безопасность
	user("PATCH /api/v1/me", s.updateProfile)
	user("POST /api/v1/me/password", s.changePassword)
	user("GET /api/v1/me/sessions", s.listSessions)
	user("DELETE /api/v1/me/sessions/{id}", s.revokeSession)
	user("POST /api/v1/me/sessions/revoke-others", s.revokeOthers)
	user("POST /api/v1/me/sessions/revoke-all", s.revokeAll)
	user("GET /api/v1/me/logins", s.loginHistory)
	user("GET /api/v1/me/consents", s.listConsents)
	user("GET /api/v1/me/export", s.exportMyData)
	user("POST /api/v1/me/onboarding", s.submitOnboarding)
	user("POST /api/v1/me/onboarding/skip", s.skipOnboarding)
	user("GET /api/v1/operations/statement", s.statement)
	user("POST /api/v1/me/2fa/setup", s.setup2FA)
	user("POST /api/v1/me/2fa/enable", s.enable2FA)
	user("POST /api/v1/me/2fa/disable", s.disable2FA)
	user("PATCH /api/v1/me/notifications", s.updateNotifySettings)
	user("POST /api/v1/me/risk", s.submitRisk)
	user("GET /api/v1/me/audit", s.audit)
	user("GET /api/v1/me/identities", s.identities)
	user("GET /api/v1/me/passkeys", s.passkeyList)
	user("POST /api/v1/me/passkeys/begin", s.passkeyRegisterBegin)
	user("POST /api/v1/me/passkeys/finish", s.passkeyRegisterFinish)
	user("DELETE /api/v1/me/passkeys/{id}", s.passkeyDelete)
	user("POST /api/v1/me/stepup/begin", s.stepUpBegin)
	user("POST /api/v1/me/stepup/finish", s.stepUpFinish)
	user("DELETE /api/v1/me/identities/{provider}", s.unlinkIdentity)
	user("POST /api/v1/me/phone/telegram", s.phoneViaTelegram)
	user("DELETE /api/v1/me/phone", s.unlinkPhone)
	user("GET /api/v1/me/audit/export.csv", s.exportAudit)
	user("POST /api/v1/me/delete", s.deleteAccount)
	user("GET /api/v1/kyc", s.getKYC)
	user("POST /api/v1/kyc", s.submitKYC)
	user("POST /api/v1/kyc/msi/start", s.msiStart)
	user("GET /api/v1/kyc/msi/callback", s.msiCallback)
	user("POST /api/v1/kyc/msi/sandbox", s.msiSandbox)
	user("POST /api/v1/kyc/docs/start", s.docsStart)
	user("POST /api/v1/kyc/docs/submitted", s.docsSubmitted)
	user("POST /api/v1/kyc/docs/sandbox", s.docsSandbox)
	user("POST /api/v1/telegram/link", s.telegramLink)
	user("DELETE /api/v1/telegram/link", s.telegramUnlink)
	user("GET /api/v1/telegram/sandbox", s.tgSandboxMessages)
	user("POST /api/v1/telegram/sandbox/send", s.tgSandboxSend)
	user("POST /api/v1/telegram/sandbox/press", s.tgSandboxPress)
	user("POST /api/v1/telegram/sandbox/contact", s.tgSandboxContact)
	user("GET /api/v1/referral", s.referral)
	user("GET /api/v1/billing", s.billing)
	user("POST /api/v1/billing/pro", s.buyPro)
	user("POST /api/v1/billing/autorenew", s.setAutorenew)

	// карты и платежи
	user("GET /api/v1/cards", s.listCards)
	user("POST /api/v1/cards/bind", s.startBinding)
	user("GET /api/v1/cards/bindings/{ref}", s.bindingStatus)
	user("POST /api/v1/cards/{id}/default", s.defaultCard)
	user("DELETE /api/v1/cards/{id}", s.deleteCard)
	user("GET /api/v1/sandbox/checkout/{ref}", s.sandboxCheckout)
	user("POST /api/v1/sandbox/checkout/{ref}", s.sandboxPay)
	user("POST /api/v1/sandbox/plans/{id}/run-schedule", s.sandboxRunSchedule)
	pub("GET /pay/return", s.payReturn)
	pub("POST /api/v1/webhooks/bepaid", s.bepaidWebhook)
	pub("POST /api/v1/webhooks/sumsub", s.sumsubWebhook)

	// копилки, портфель, операции
	user("GET /api/v1/plans", s.listPlans)
	user("POST /api/v1/plans", s.createPlan)
	user("GET /api/v1/plans/{id}", s.getPlan)
	user("PATCH /api/v1/plans/{id}", s.updatePlan)
	user("POST /api/v1/plans/{id}/pause", s.pausePlan)
	user("POST /api/v1/plans/{id}/resume", s.resumePlan)
	user("POST /api/v1/plans/{id}/topup", s.topupPlan)
	user("POST /api/v1/plans/{id}/withdraw", s.withdrawPlan)
	user("POST /api/v1/plans/{id}/close", s.closePlan)
	user("GET /api/v1/plans/{id}/history", s.planHistory)
	user("GET /api/v1/portfolio", s.portfolio)
	user("GET /api/v1/portfolio/history", s.portfolioHistory)
	user("GET /api/v1/operations", s.listOperations)
	user("GET /api/v1/operations/export.csv", s.exportOperations)
	user("GET /api/v1/operations/{id}", s.getOperation)

	// рынок
	pub("GET /api/v1/market", s.marketOverview)
	pub("GET /api/v1/market/assets/{symbol}", s.marketAsset)
	pub("GET /api/v1/market/candles", s.marketCandles)
	pub("GET /api/v1/stream", s.stream)
	pub("POST /api/v1/calculator/backtest", s.backtest)
	pub("GET /api/v1/calculator/showcase", s.showcase)

	// уведомления и поддержка
	user("GET /api/v1/notifications", s.listNotifications)
	user("POST /api/v1/notifications/read", s.readNotifications)
	user("GET /api/v1/support/tickets", s.listTickets)
	user("POST /api/v1/support/tickets", s.createTicket)
	user("GET /api/v1/support/tickets/{id}", s.getTicket)
	user("POST /api/v1/support/tickets/{id}/messages", s.replyTicket)
	user("POST /api/v1/support/tickets/{id}/close", s.closeTicket)

	// админка
	staff("GET /api/v1/admin/overview", s.adminOverview)
	staff("GET /api/v1/admin/metrics", s.adminMetrics)
	staff("GET /api/v1/admin/users", s.adminUsers)
	staff("GET /api/v1/admin/users/{id}", s.adminUser)
	admin("POST /api/v1/admin/users/{id}/status", s.adminUserStatus)
	admin("POST /api/v1/admin/users/{id}/role", s.adminUserRole)
	admin("POST /api/v1/admin/users/{id}/pro", s.adminGrantPro)
	staff("GET /api/v1/admin/kyc", s.adminKYC)
	staff("POST /api/v1/admin/kyc/{id}/review", s.adminKYCReview)
	staff("GET /api/v1/admin/kyc/{id}/file/{kind}", s.adminKYCFile)
	staff("GET /api/v1/admin/tickets", s.adminTickets)
	staff("GET /api/v1/admin/tickets/{id}", s.adminTicket)
	staff("POST /api/v1/admin/tickets/{id}/reply", s.adminTicketReply)
	staff("GET /api/v1/admin/users/{id}/export/{kind}", s.adminUserExport)
	staff("GET /api/v1/admin/orders/export.csv", s.adminOrdersExport)
	staff("POST /api/v1/admin/tickets/{id}/status", s.adminTicketStatus)
	staff("GET /api/v1/admin/orders", s.adminOrders)
	admin("POST /api/v1/admin/orders/{id}/payout-done", s.adminPayoutDone)
	admin("POST /api/v1/admin/orders/{id}/fill", s.adminOrderFill)
	admin("POST /api/v1/admin/orders/{id}/refund", s.adminOrderRefund)
	staff("GET /api/v1/admin/system", s.adminSystem)

	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "Метод не найден")
	})
	m.Handle("/", s.spa())
}

// ---------------------------------------------------------------- middleware

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.Log.Error("panic", "path", r.URL.Path, "err", v, "stack", string(debug.Stack()))
				writeErr(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", s.csp())
		if s.Cfg.SecureCookies {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(c int) { w.code = c; w.ResponseWriter.WriteHeader(c) }
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		sw := &statusWriter{ResponseWriter: w, code: 200}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/stream" {
			s.Log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.code, "ms", time.Since(t).Milliseconds())
		}
	})
}

// clientIP — адрес клиента. X-Forwarded-For учитывается только если запрос пришёл от своего прокси
// (loopback/частная сеть), и берётся последний адрес — его добавил наш Caddy, подделать его клиент не может.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remote := net.ParseIP(host)
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" && remote != nil && (remote.IsLoopback() || remote.IsPrivate()) {
		parts := strings.Split(xf, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); net.ParseIP(ip) != nil {
			return ip
		}
	}
	return host
}

func (s *Server) withSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(store.WithClient(r.Context(), store.ClientInfo{IP: clientIP(r), UserAgent: r.UserAgent()}))
		c, err := r.Cookie(sessionCookie)
		if err == nil && c.Value != "" {
			hash := auth.HashToken(c.Value)
			if sess, err := s.St.SessionByHash(r.Context(), hash); err == nil {
				if u, err := s.St.UserByID(r.Context(), sess.UserID); err == nil && u.Status == "active" {
					ctx := context.WithValue(r.Context(), keySession, sess)
					if !sess.MFAPending {
						ctx = context.WithValue(ctx, keyUser, u)
						if time.Since(time.Unix(sess.LastSeenAt, 0)) > 5*time.Minute {
							s.St.TouchSession(r.Context(), hash, clientIP(r))
						}
					}
					r = r.WithContext(ctx)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// csp — политика источников. Виджет Sumsub (скрипт и iframe с их домена) разрешаем, только если он подключён.
func (s *Server) csp() string {
	script, frame, connect := "'self'", "'self'", "'self'"
	if s.Cfg.KYCDocs == "sumsub" {
		script += " https://static.sumsub.com"
		frame += " https://*.sumsub.com"
		connect += " https://*.sumsub.com"
	}
	return "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src " + script + "; connect-src " + connect +
		"; frame-src " + frame + "; font-src 'self' data:; frame-ancestors 'none'; form-action 'self' https://checkout.bepaid.by https://gateway.bepaid.by; base-uri 'self'"
}

// csrf — double-submit cookie: для изменяющих запросов заголовок X-CSRF-Token должен совпасть с cookie.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(csrfCookie); err != nil {
			tok, _ := auth.NewToken()
			http.SetCookie(w, &http.Cookie{Name: csrfCookie, Value: tok, Path: "/", SameSite: http.SameSiteLaxMode, Secure: s.Cfg.SecureCookies, MaxAge: 365 * 86400})
			r.AddCookie(&http.Cookie{Name: csrfCookie, Value: tok})
		}
		unsafe := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		if unsafe && strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/v1/webhooks/") {
			c, _ := r.Cookie(csrfCookie)
			if c == nil || r.Header.Get("X-CSRF-Token") == "" || r.Header.Get("X-CSRF-Token") != c.Value {
				writeErr(w, http.StatusForbidden, "Обновите страницу и повторите действие")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(keyUser).(*store.User)
	return u
}

func currentSession(r *http.Request) *store.Session {
	s, _ := r.Context().Value(keySession).(*store.Session)
	return s
}

func (s *Server) requireUser(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r) == nil {
			writeErr(w, http.StatusUnauthorized, "Войдите в аккаунт")
			return
		}
		h(w, r)
	}
}

func (s *Server) requireStaff(h http.HandlerFunc, adminOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "Войдите в аккаунт")
			return
		}
		if !u.IsStaff() || (adminOnly && u.Role != "admin") {
			writeErr(w, http.StatusForbidden, "Недостаточно прав")
			return
		}
		// В боевом режиме доступ к данным клиентов — только с двухфакторной защитой.
		if s.Cfg.Env == "production" && !u.TOTPEnabled {
			writeJSON(w, http.StatusForbidden, apiErr{Error: "Для доступа к админке включите двухфакторную защиту: Профиль → Безопасность", Code: "staff_2fa_required"})
			return
		}
		h(w, r)
	}
}

// ---------------------------------------------------------------- rate limit

type limiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func newLimiter() *limiter { return &limiter{hits: map[string][]time.Time{}} }

// Allow — не больше n событий за окно.
func (l *limiter) Allow(key string, n int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	h := l.hits[key]
	i := 0
	for i < len(h) && now.Sub(h[i]) > window {
		i++
	}
	h = h[i:]
	if len(h) >= n {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, now)
	if len(l.hits) > 50000 {
		// убираем только ключи без свежих событий (окна не длиннее суток); остальные лимиты сохраняются
		for k, v := range l.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) > 24*time.Hour {
				delete(l.hits, k)
			}
		}
		if len(l.hits) > 200000 {
			return false // при атаке на память отказываем новым ключам, а не сбрасываем лимиты
		}
	}
	return true
}

// ---------------------------------------------------------------- helpers

type apiErr struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
	Code  string `json:"code,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiErr{Error: msg})
}

func writeFieldErr(w http.ResponseWriter, field, msg string) {
	writeJSON(w, http.StatusUnprocessableEntity, apiErr{Error: msg, Field: field})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "Некорректный запрос")
		return false
	}
	return true
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func qInt(r *http.Request, k string, def int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(k))
	if err != nil {
		return def
	}
	return v
}

// internal — логирует ошибку и отвечает 500 без подробностей.
func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "Не найдено")
		return
	}
	s.Log.Error("handler", "path", r.URL.Path, "err", err)
	writeErr(w, http.StatusInternalServerError, "Что-то пошло не так. Мы уже разбираемся")
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "db")
		return
	}
	if len(s.Hub.Snapshot().Quotes) == 0 {
		writeErr(w, http.StatusServiceUnavailable, "market")
		return
	}
	w.Write([]byte("ready"))
}
