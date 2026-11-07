package api

// Вход и регистрация через Google и Apple, привязка их к существующему аккаунту.
// Поток: /oauth/{provider}/start → страница провайдера (или тестовая страница на стенде) → /oauth/{provider}/callback.
// state одноразовый, хранится на сервере и дублируется в cookie браузера: чужой code+state в ссылке не войдёт.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"autoinvest/internal/auth"
	"autoinvest/internal/notify"
	"autoinvest/internal/oauth"
	"autoinvest/internal/store"
)

const oauthCookie = "ai_oauth"

var providerNames = map[string]string{"google": "Google", "apple": "Apple"}

type oauthState struct {
	Provider string `json:"provider"`
	Mode     string `json:"mode"` // login | link
	UserID   int64  `json:"uid"`
	Next     string `json:"next"`
	Exp      int64  `json:"exp"`
}

func (s *Server) oauthMode(provider string) string {
	switch provider {
	case "google":
		return s.Cfg.OAuthGoogle
	case "apple":
		return s.Cfg.OAuthApple
	}
	return "off"
}

func (s *Server) google() *oauth.Google {
	return &oauth.Google{ClientID: s.Cfg.GoogleClientID, ClientSecret: s.Cfg.GoogleClientSecret, RedirectURL: s.Cfg.PublicURL + "/oauth/google/callback"}
}

func (s *Server) apple() *oauth.Apple {
	return &oauth.Apple{ClientID: s.Cfg.AppleClientID, TeamID: s.Cfg.AppleTeamID, KeyID: s.Cfg.AppleKeyID, PrivateKey: s.Cfg.ApplePrivateKey,
		RedirectURL: s.Cfg.PublicURL + "/oauth/apple/callback"}
}

// safeNext — только относительные адреса внутри сайта (без //evil.com).
func safeNext(n string) string {
	if !strings.HasPrefix(n, "/") || strings.HasPrefix(n, "//") || strings.HasPrefix(n, "/\\") || len(n) > 300 {
		return "/app"
	}
	return n
}

// oauthStart — GET /oauth/{provider}/start?mode=login|link&next=/app
func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	mode := s.oauthMode(provider)
	if providerNames[provider] == "" || mode == "off" {
		http.Redirect(w, r, "/login?oauth_error=off", http.StatusFound)
		return
	}
	st := oauthState{Provider: provider, Mode: "login", Next: safeNext(r.URL.Query().Get("next")), Exp: time.Now().Add(10 * time.Minute).Unix()}
	if r.URL.Query().Get("mode") == "link" {
		u := currentUser(r)
		if u == nil {
			http.Redirect(w, r, "/login?next="+url.QueryEscape("/app/profile/security"), http.StatusFound)
			return
		}
		st.Mode, st.UserID, st.Next = "link", u.ID, "/app/profile/security"
	}
	if !s.limiter.Allow("oauth:"+clientIP(r), 30, 15*time.Minute) {
		http.Redirect(w, r, "/login?oauth_error=limit", http.StatusFound)
		return
	}
	state := strings.ToLower(auth.RandomCode(32, ""))
	raw, _ := json.Marshal(st)
	s.St.SetKV(r.Context(), "oauth_state:"+state, string(raw))
	// Apple возвращает ответ POST-запросом с appleid.apple.com: cookie с SameSite=Lax в такой запрос не попадёт,
	// поэтому на HTTPS ставим SameSite=None (cookie всё равно HttpOnly, живёт 10 минут и годится только для сверки state).
	ck := &http.Cookie{Name: oauthCookie, Value: state, Path: "/oauth/", HttpOnly: true, MaxAge: 600, SameSite: http.SameSiteLaxMode}
	if s.Cfg.SecureCookies {
		ck.Secure, ck.SameSite = true, http.SameSiteNoneMode
	}
	http.SetCookie(w, ck)
	target := "/oauth-sandbox?provider=" + provider + "&state=" + state
	if mode == "on" {
		if provider == "google" {
			target = s.google().AuthURL(state)
		} else {
			target = s.apple().AuthURL(state)
		}
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// takeState проверяет state: выдан нами, тем же провайдером, не истёк, совпадает с cookie браузера. Одноразовый.
func (s *Server) takeState(w http.ResponseWriter, r *http.Request, provider, state string) (*oauthState, bool) {
	ck, _ := r.Cookie(oauthCookie)
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/oauth/", MaxAge: -1, HttpOnly: true})
	if state == "" || len(state) > 64 || ck == nil || ck.Value != state {
		return nil, false
	}
	raw := s.St.GetKV(r.Context(), "oauth_state:"+state)
	s.St.SetKV(r.Context(), "oauth_state:"+state, "")
	var st oauthState
	if raw == "" || json.Unmarshal([]byte(raw), &st) != nil || st.Provider != provider || st.Exp < time.Now().Unix() {
		return nil, false
	}
	return &st, true
}

func (s *Server) oauthGoogleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.oauthFinish(w, r, "google", q.Get("state"), q.Get("error"), func() (*oauth.Profile, error) {
		return s.google().Exchange(r.Context(), q.Get("code"))
	})
}

func (s *Server) oauthAppleCallback(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	r.ParseForm()
	s.oauthFinish(w, r, "apple", r.PostForm.Get("state"), r.PostForm.Get("error"), func() (*oauth.Profile, error) {
		return s.apple().Exchange(r.Context(), r.PostForm.Get("code"), r.PostForm.Get("user"))
	})
}

// oauthSandboxCallback — ответ тестовой страницы входа (только в режиме sandbox).
func (s *Server) oauthSandboxCallback(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	r.ParseForm()
	provider := r.PostForm.Get("provider")
	if s.oauthMode(provider) != "sandbox" {
		http.Error(w, "тестовый вход выключен", http.StatusNotFound)
		return
	}
	s.oauthFinish(w, r, provider, r.PostForm.Get("state"), r.PostForm.Get("error"), func() (*oauth.Profile, error) {
		email := strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))
		if !validEmail(email) {
			return nil, errors.New("неверная почта")
		}
		sum := sha256.Sum256([]byte(provider + ":" + email))
		return &oauth.Profile{Subject: "sandbox-" + hex.EncodeToString(sum[:8]), Email: email, EmailVerified: true, Name: strings.TrimSpace(r.PostForm.Get("name"))}, nil
	})
}

func (s *Server) oauthFinish(w http.ResponseWriter, r *http.Request, provider, state, provErr string, exchange func() (*oauth.Profile, error)) {
	st, ok := s.takeState(w, r, provider, state)
	if !ok {
		http.Redirect(w, r, "/login?oauth_error=state", http.StatusFound)
		return
	}
	fail := func(code string) {
		if st.Mode == "link" {
			http.Redirect(w, r, "/app/profile/security?oauth_error="+code, http.StatusFound)
			return
		}
		http.Redirect(w, r, "/login?oauth_error="+code, http.StatusFound)
	}
	if provErr != "" {
		fail("cancel") // пользователь закрыл окно провайдера или отказался
		return
	}
	p, err := exchange()
	if err != nil {
		s.Log.Warn("oauth", "provider", provider, "err", err)
		fail("provider")
		return
	}
	ctx := r.Context()

	if st.Mode == "link" {
		if err := s.St.LinkIdentity(ctx, st.UserID, provider, p.Subject, p.Email); err != nil {
			if errors.Is(err, store.ErrIdentityTaken) {
				fail("taken")
				return
			}
			s.Log.Error("oauth link", "err", err)
			fail("provider")
			return
		}
		s.St.Audit(ctx, st.UserID, st.UserID, "security.identity_linked", providerNames[provider], clientIP(r))
		http.Redirect(w, r, "/app/profile/security?oauth=linked", http.StatusFound)
		return
	}

	u, err := s.St.UserByIdentity(ctx, provider, p.Subject)
	if errors.Is(err, store.ErrNotFound) {
		u, err = s.oauthUser(r, provider, p)
		if err != nil {
			s.Log.Warn("oauth user", "provider", provider, "err", err)
			fail("email")
			return
		}
	} else if err != nil {
		s.Log.Error("oauth", "err", err)
		fail("provider")
		return
	}
	if u.Status == "blocked" {
		fail("blocked")
		return
	}
	// вход через Google второй защиты не требует: у Google своя проверка (опасные действия всё равно подтверждаются)
	if err := s.setSession(w, r, u, false); err != nil {
		s.Log.Error("oauth session", "err", err)
		fail("provider")
		return
	}
	s.afterLogin(r, u)
	http.Redirect(w, r, st.Next, http.StatusFound)
}

// oauthUser — первый вход через провайдера: привязываем к аккаунту с той же подтверждённой почтой или создаём новый.
func (s *Server) oauthUser(r *http.Request, provider string, p *oauth.Profile) (*store.User, error) {
	ctx := r.Context()
	if p.Email == "" {
		return nil, errors.New("провайдер не передал почту")
	}
	if p.EmailVerified {
		if u, err := s.St.UserByEmail(ctx, p.Email); err == nil {
			if err := s.St.LinkIdentity(ctx, u.ID, provider, p.Subject, p.Email); err != nil {
				return nil, err
			}
			s.St.Audit(ctx, u.ID, u.ID, "security.identity_linked", providerNames[provider], clientIP(r))
			return u, nil
		}
	}
	if !s.limiter.Allow("reg:"+clientIP(r), 10, time.Hour) {
		return nil, errors.New("слишком много регистраций")
	}
	name := p.Name
	if l := len([]rune(name)); l < 2 || l > 60 {
		name = strings.Split(p.Email, "@")[0]
		if len([]rune(name)) < 2 {
			name = "Пользователь"
		}
	}
	u, err := s.St.CreateUser(ctx, store.NewUser{Email: p.Email, Name: name, EmailVerified: p.EmailVerified})
	if err != nil {
		return nil, err // в том числе ErrEmailTaken: почта занята, но провайдер не подтвердил её — не связываем молча
	}
	if err := s.St.LinkIdentity(ctx, u.ID, provider, p.Subject, p.Email); err != nil {
		return nil, err
	}
	// на странице входа под кнопками Google/Apple написано, что продолжая, человек принимает оферту и согласие на обработку данных
	for _, kind := range []string{store.ConsentOffer, store.ConsentRisks, store.ConsentPrivacy} {
		s.St.RecordConsent(ctx, u.ID, kind, LegalVersion)
	}
	s.St.Audit(ctx, u.ID, u.ID, "auth.register", providerNames[provider], clientIP(r))
	s.N.Send(ctx, u.ID, notify.Message{Kind: "welcome", Title: "Добро пожаловать в AutoInvest",
		Body: "Создайте первую копилку: выберите сумму, расписание и план — дальше всё произойдёт само.", Link: "/app/plans/new"})
	return u, nil
}

// ---------------------------------------------------------------- способы входа в профиле

func (s *Server) identities(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	list, err := s.St.Identities(r.Context(), u.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"identities": list, "has_password": u.HasPassword, "phone": u.Phone, "phone_verified": u.PhoneVerified,
		"providers": map[string]string{"google": s.Cfg.OAuthGoogle, "apple": s.Cfg.OAuthApple, "phone": s.phoneMode()}})
}

// unlinkIdentity — отвязать Google/Apple, если остаётся другой способ войти.
func (s *Server) unlinkIdentity(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	provider := r.PathValue("provider")
	list, _ := s.St.Identities(r.Context(), u.ID)
	others := 0
	for _, i := range list {
		if i.Provider != provider {
			others++
		}
	}
	if !u.HasPassword && !u.PhoneVerified && others == 0 {
		writeErr(w, http.StatusConflict, "Это единственный способ входа. Сначала задайте пароль во вкладке «Пароль»")
		return
	}
	s.St.UnlinkIdentity(r.Context(), u.ID, provider)
	s.St.Audit(r.Context(), u.ID, u.ID, "security.identity_unlinked", providerNames[provider], clientIP(r))
	s.identities(w, r)
}

// passwordGate — проверка пароля перед важным действием; у аккаунта без пароля (вход через Google/Apple) — понятная подсказка.
func passwordGate(u *store.User, password string) string {
	if !u.HasPassword {
		return "У аккаунта ещё нет пароля — задайте его в Профиль → Пароль"
	}
	if !auth.CheckPassword(u.PasswordHash, password) {
		return "Неверный пароль"
	}
	return ""
}
