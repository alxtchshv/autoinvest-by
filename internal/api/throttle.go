package api

// Защита от перегрузки (DDoS на уровне приложения). Стоит первой в цепочке — до сессии и базы:
//   - каждый IP: не больше ~10 запросов к API в секунду (с запасом на всплеск 60);
//   - каждый IP: не больше 6 одновременных живых потоков котировок (SSE);
//   - всего на сервер: не больше 512 запросов одновременно — дальше «503, попробуйте позже», а не падение.
// Объёмные атаки (гигабиты трафика) приложение не остановит — от них защищает хостинг или Cloudflare перед сервером.

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// rateGate — «ведро с жетонами» на каждый IP: rate жетонов в секунду, не больше burst.
type rateGate struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	buckets map[string]*bucket
	streams map[string]int
	swept   time.Time
}

func newRateGate(rate, burst float64) *rateGate {
	return &rateGate{rate: rate, burst: burst, buckets: map[string]*bucket{}, streams: map[string]int{}, swept: time.Now()}
}

func (g *rateGate) allow(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if now.Sub(g.swept) > time.Minute { // старые вёдра (заполнились до краёв) больше не нужны
		for k, b := range g.buckets {
			if now.Sub(b.last) > 2*time.Minute {
				delete(g.buckets, k)
			}
		}
		g.swept = now
	}
	b := g.buckets[ip]
	if b == nil {
		b = &bucket{tokens: g.burst, last: now}
		g.buckets[ip] = b
	}
	b.tokens = min(g.burst, b.tokens+now.Sub(b.last).Seconds()*g.rate)
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (g *rateGate) openStream(ip string, max int) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.streams[ip] >= max {
		return false
	}
	g.streams[ip]++
	return true
}

func (g *rateGate) closeStream(ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.streams[ip]--; g.streams[ip] <= 0 {
		delete(g.streams, ip)
	}
}

const (
	maxInFlight      = 512
	maxStreamsPerIP  = 6
	apiRatePerSecond = 10
	apiBurst         = 60
)

func (s *Server) throttle(next http.Handler) http.Handler {
	rate, burst := float64(apiRatePerSecond), float64(apiBurst)
	if s.RatePerIP > 0 {
		rate, burst = s.RatePerIP, s.RatePerIP*6
	}
	gate := newRateGate(rate, burst)
	inflight := make(chan struct{}, maxInFlight)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)
		api := strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/oauth/")
		if api && !gate.allow(ip) {
			w.Header().Set("Retry-After", "5")
			writeErr(w, http.StatusTooManyRequests, "Слишком много запросов. Подождите несколько секунд")
			return
		}
		if r.URL.Path == "/api/v1/stream" {
			if !gate.openStream(ip, maxStreamsPerIP) {
				writeErr(w, http.StatusTooManyRequests, "Слишком много открытых вкладок с котировками")
				return
			}
			defer gate.closeStream(ip)
			next.ServeHTTP(w, r) // долгие потоки не занимают общий лимит одновременных запросов
			return
		}
		select {
		case inflight <- struct{}{}:
			defer func() { <-inflight }()
		default:
			w.Header().Set("Retry-After", "10")
			writeErr(w, http.StatusServiceUnavailable, "Сервис перегружен. Попробуйте через минуту")
			return
		}
		next.ServeHTTP(w, r)
	})
}
