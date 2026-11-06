package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"autoinvest/internal/engine"
	"autoinvest/internal/market"
	"autoinvest/internal/notify"
)

func (s *Server) marketOverview(w http.ResponseWriter, r *http.Request) {
	snap := s.Hub.Snapshot()
	writeJSON(w, 200, map[string]any{"assets": market.Catalog, "quotes": snap.Quotes, "rates": snap.Rates, "strategies": market.Strategies, "demo": s.Hub.IsFixture()})
}

func (s *Server) assetStats(r *http.Request, sym string) (market.Stats, bool) {
	s.statsMu.Lock()
	e, ok := s.stats[sym]
	s.statsMu.Unlock()
	if ok && time.Since(e.at) < time.Hour {
		return e.stats, true
	}
	daily, err := s.Hub.DailyHistory(r.Context(), sym, time.Now().AddDate(-1, 0, -5))
	if err != nil || len(daily) < 10 {
		return market.Stats{}, false
	}
	st := market.ComputeStats(daily)
	s.statsMu.Lock()
	s.stats[sym] = statsEntry{at: time.Now(), stats: st}
	s.statsMu.Unlock()
	return st, true
}

func (s *Server) marketAsset(w http.ResponseWriter, r *http.Request) {
	sym := r.PathValue("symbol")
	a, ok := market.Find(sym)
	if !ok {
		writeErr(w, http.StatusNotFound, "Актив не найден")
		return
	}
	q, qerr := s.Hub.Quote(sym)
	out := map[string]any{"asset": a}
	if qerr == nil {
		out["quote"] = q
	}
	if st, ok := s.assetStats(r, sym); ok {
		out["stats"] = st
	}
	writeJSON(w, 200, out)
}

func (s *Server) marketCandles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cur := q.Get("currency")
	if cur != "USD" {
		cur = "BYN"
	}
	interval := q.Get("interval")
	if interval == "" {
		interval = "1h"
	}
	cs, err := s.Hub.Candles(r.Context(), q.Get("symbol"), interval, cur, qInt(r, "limit", 500))
	if errors.Is(err, market.ErrUnknownAsset) {
		writeErr(w, http.StatusNotFound, "Актив не найден")
		return
	}
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "Не удалось получить свечи с биржи. Попробуйте через минуту")
		return
	}
	writeJSON(w, 200, map[string]any{"candles": cs, "currency": cur, "interval": interval})
}

// stream — Server-Sent Events: котировки каждые ~5 секунд и (для вошедших) личные события.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	// у SSE нет таймаута на запись: снимаем его для этого соединения
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	send := func(event string, v any) bool {
		b, _ := json.Marshal(v)
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return false
		}
		fl.Flush()
		return true
	}
	send("quotes", s.Hub.Snapshot())
	mch, mcancel := s.Hub.Subscribe()
	defer mcancel()
	var uch chan notify.UserEvent // nil-канал никогда не срабатывает — для гостей
	if u := currentUser(r); u != nil {
		ch, cancel := s.N.Subscribe(u.ID)
		defer cancel()
		uch = ch
	}
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-mch:
			if !send("quotes", ev) {
				return
			}
		case ev, ok := <-uch:
			if ok && !send(ev.Type, ev.Data) {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func (s *Server) backtest(w http.ResponseWriter, r *http.Request) {
	var in engine.BacktestRequest
	if !readJSON(w, r, &in) {
		return
	}
	if !s.limiter.Allow("bt:"+clientIP(r), 60, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много расчётов. Попробуйте позже")
		return
	}
	if in.DepositRate <= 0 || in.DepositRate > 30 {
		in.DepositRate = 9
	}
	res, err := s.En.Backtest(r.Context(), in)
	if err != nil {
		if errors.Is(err, engine.ErrMarket) {
			writeErr(w, http.StatusServiceUnavailable, "История цен сейчас недоступна. Попробуйте через минуту")
			return
		}
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, 200, res)
}

// showcase — «машина времени» на главной: все стратегии и активы на одном отрезке, лучшие сверху.
// Суммы и сроки — только из коротких списков, а результат кэшируется на час: расчёт тяжёлый,
// а главную открывают все подряд.
func (s *Server) showcase(w http.ResponseWriter, r *http.Request) {
	amount, _ := strconv.ParseInt(r.URL.Query().Get("amount_kop"), 10, 64)
	years, _ := strconv.Atoi(r.URL.Query().Get("years"))
	switch amount {
	case 1000, 2000, 5000, 10000:
	default:
		amount = 2000
	}
	switch years {
	case 3, 5, 7:
	default:
		years = 7
	}
	items, err := s.showcaseFor(r.Context(), amount, years, false)
	if err != nil {
		writeErr(w, http.StatusServiceUnavailable, "История цен сейчас недоступна. Попробуйте через минуту")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "amount_kop": amount, "years": years})
}

var (
	showcaseAmounts = []int64{1000, 2000, 5000, 10000}
	showcaseYears   = []int{7, 5, 3}
)

func (s *Server) showcaseFor(ctx context.Context, amount int64, years int, force bool) ([]engine.ShowcaseItem, error) {
	key := fmt.Sprintf("%d/%d", amount, years)
	s.showMu.Lock()
	defer s.showMu.Unlock()
	if e, ok := s.show[key]; ok && !force && time.Since(e.at) < time.Hour {
		return e.items, nil
	}
	items, err := s.En.Showcase(ctx, amount, float64(years))
	if err != nil {
		if e, ok := s.show[key]; ok && time.Since(e.at) < 6*time.Hour {
			return e.items, nil // источник истории моргнул — покажем прошлый расчёт
		}
		return nil, err
	}
	if s.show == nil {
		s.show = map[string]showcaseEntry{}
	}
	s.show[key] = showcaseEntry{at: time.Now(), items: items}
	return items, nil
}

// WarmShowcase заранее считает витрину для всех сумм и сроков, чтобы главная открывалась сразу,
// и обновляет её раньше, чем истечёт часовой кэш.
func (s *Server) WarmShowcase(ctx context.Context) {
	t := time.NewTimer(3 * time.Second) // дать хабу загрузить курс НБРБ
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for _, y := range showcaseYears {
			for _, a := range showcaseAmounts {
				if _, err := s.showcaseFor(ctx, a, y, true); err != nil && ctx.Err() == nil {
					s.Log.Warn("витрина доходности не посчитана", "amount", a, "years", y, "err", err)
				}
			}
		}
		t.Reset(50 * time.Minute)
	}
}
