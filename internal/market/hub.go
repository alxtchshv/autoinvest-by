package market

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"
)

// Event рассылается подписчикам SSE при каждом обновлении котировок.
type Event struct {
	Quotes []Quote `json:"quotes"`
	Rates  Rates   `json:"rates"`
}

type candleKey struct{ sym, interval string }

type candleEntry struct {
	at      time.Time
	candles []Candle // USD
}

type SourceStatus struct {
	Name     string    `json:"name"`
	OK       bool      `json:"ok"`
	LastOK   time.Time `json:"last_ok"`
	LastErr  string    `json:"last_error"`
	ErrCount int       `json:"error_count"`
}

// Hub собирает котировки из цепочки источников и раздаёт их приложению.
type Hub struct {
	sources  []Source
	stockSrc Source // акции и фонды (Yahoo); опрашивается реже, чем крипта
	rateSrc  RateSource
	fixture  bool
	spreadBP int64
	log      *slog.Logger

	mu       sync.RWMutex
	quotes   map[string]Quote
	rates    Rates
	prevUSD  float64
	usdHist  map[string]float64
	histAt   time.Time
	candles  map[candleKey]candleEntry
	daily    map[string]candleEntry
	status   map[string]*SourceStatus
	subs     map[chan Event]struct{}
	inflight sync.Mutex
	persist  RatePersister
}

// RatePersister сохраняет последний настоящий курс НБРБ, чтобы после перезапуска без интернета
// сервис знал реальный курс, а не подставлял выдуманный.
type RatePersister interface {
	LoadRates(ctx context.Context) (Rates, bool)
	SaveRates(ctx context.Context, r Rates)
}

// SetRatePersister подключает хранилище курса. Вызывать до Start.
func (h *Hub) SetRatePersister(p RatePersister) { h.persist = p }

// ratesFreshFor — сколько курс считается годным для сделок. НБРБ публикует курс раз в рабочий день,
// поэтому граница с запасом: сутки и пара часов.
const ratesFreshFor = 26 * time.Hour

func NewHub(sources []Source, rates RateSource, spreadBP int64, fixture bool, log *slog.Logger) *Hub {
	h := &Hub{sources: sources, rateSrc: rates, spreadBP: spreadBP, fixture: fixture, log: log,
		quotes: map[string]Quote{}, usdHist: map[string]float64{}, candles: map[candleKey]candleEntry{},
		daily: map[string]candleEntry{}, status: map[string]*SourceStatus{}, subs: map[chan Event]struct{}{}}
	for _, s := range sources {
		h.status[s.Name()] = &SourceStatus{Name: s.Name()}
	}
	h.status["nbrb"] = &SourceStatus{Name: "nbrb"}
	return h
}

// NewLiveHub — реальные источники.
func NewLiveHub(spreadBP int64, log *slog.Logger) *Hub {
	h := NewHub([]Source{NewBinance(), NewBybit(), NewCoinGecko()}, NewNBRB(), spreadBP, false, log)
	h.stockSrc = NewYahoo()
	h.status["yahoo"] = &SourceStatus{Name: "yahoo"}
	return h
}

// NewFixtureHub — офлайн-данные для разработки.
func NewFixtureHub(spreadBP int64, log *slog.Logger) *Hub {
	h := NewHub([]Source{Fixture{}}, FixtureRates{}, spreadBP, true, log)
	h.stockSrc = Fixture{}
	return h
}

// stockRefreshEvery — акции торгуются по расписанию биржи, частый опрос не нужен.
const stockRefreshEvery = 30 * time.Second

// refreshStocks обновляет котировки акций. Работает в своём цикле: медленный ответ Yahoo
// не должен задерживать обновление крипты, а при сбое источник не дёргается чаще раза в 30 секунд.
// Новые цены уходят подписчикам со следующим обновлением крипты (не позже чем через 5 секунд).
func (h *Hub) refreshStocks(ctx context.Context) {
	if h.stockSrc == nil {
		return
	}
	var stocks []Asset
	for _, a := range Catalog {
		if a.Yahoo != "" {
			stocks = append(stocks, a)
		}
	}
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	sq, err := h.stockSrc.Quotes(c, stocks)
	cancel()
	h.mark(h.stockSrc.Name(), err)
	if err != nil {
		h.log.Debug("котировки акций недоступны", "err", err)
		return
	}
	h.mu.Lock()
	for k, v := range sq {
		if a, ok := Find(k); ok && a.Yahoo != "" {
			h.quotes[k] = v
		}
	}
	h.mu.Unlock()
}

func (h *Hub) IsFixture() bool { return h.fixture }

// Start запускает фоновое обновление котировок (каждые 5 с) и курсов (каждые 30 мин).
func (h *Hub) Start(ctx context.Context) {
	h.refreshRates(ctx)
	h.refreshStocks(ctx)
	h.refreshQuotes(ctx)
	go h.loadHistory(ctx)
	if h.stockSrc != nil {
		go func() {
			t := time.NewTicker(stockRefreshEvery)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					h.refreshStocks(ctx)
				}
			}
		}()
	}
	go func() {
		qt := time.NewTicker(5 * time.Second)
		rt := time.NewTicker(30 * time.Minute)
		ht := time.NewTicker(12 * time.Hour)
		defer qt.Stop()
		defer rt.Stop()
		defer ht.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-qt.C:
				h.refreshQuotes(ctx)
			case <-rt.C:
				h.refreshRates(ctx)
			case <-ht.C:
				h.loadHistory(ctx)
			}
		}
	}()
}

func (h *Hub) mark(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.status[name]
	if st == nil {
		st = &SourceStatus{Name: name}
		h.status[name] = st
	}
	if err != nil {
		st.OK = false
		st.LastErr = err.Error()
		st.ErrCount++
		return
	}
	st.OK = true
	st.LastOK = time.Now()
}

func (h *Hub) refreshRates(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	r, err := h.rateSrc.Current(c)
	h.mark("nbrb", err)
	if err != nil {
		h.log.Warn("курсы НБРБ недоступны", "err", err)
		h.mu.Lock()
		empty := h.rates.USD <= 0
		h.mu.Unlock()
		if empty && h.persist != nil {
			if saved, ok := h.persist.LoadRates(ctx); ok && saved.USD > 0 {
				saved.Source = "НБРБ, сохранённый курс"
				h.mu.Lock()
				h.rates = saved
				h.mu.Unlock()
				h.log.Warn("используется последний сохранённый курс НБРБ", "usd", saved.USD, "date", saved.Date)
			}
		}
		return
	}
	h.mu.Lock()
	h.rates = r
	h.mu.Unlock()
	if h.persist != nil && !h.fixture {
		h.persist.SaveRates(ctx, r)
	}
}

func (h *Hub) loadHistory(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	to := time.Now()
	from := time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC)
	hist, err := h.rateSrc.USDHistory(c, from, to)
	if err != nil || len(hist) == 0 {
		if ctx.Err() != nil {
			return // сервер останавливается — это не ошибка источника
		}
		h.log.Warn("история курса USD НБРБ недоступна", "err", err)
		return
	}
	h.mu.Lock()
	h.usdHist = hist
	h.histAt = time.Now()
	y := to.AddDate(0, 0, -1).Format("2006-01-02")
	h.prevUSD = hist[y]
	h.mu.Unlock()
}

func (h *Hub) refreshQuotes(ctx context.Context) {
	var got map[string]Quote
	for _, s := range h.sources {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		q, err := s.Quotes(c, Catalog)
		cancel()
		h.mark(s.Name(), err)
		if err == nil && len(q) > 0 {
			got = q
			break
		}
		if err != nil {
			h.log.Debug("источник котировок недоступен", "source", s.Name(), "err", err)
		}
	}
	h.mu.Lock()
	for k, v := range got {
		h.quotes[k] = v
	}
	ev := h.snapshotLocked()
	subs := make([]chan Event, 0, len(h.subs))
	for ch := range h.subs {
		subs = append(subs, ch)
	}
	h.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default: // медленный клиент пропускает тик
		}
	}
}

func (h *Hub) snapshotLocked() Event {
	ev := Event{Rates: h.rates}
	ev.Rates.Stale = h.ratesStaleLocked()
	for _, a := range Catalog {
		if q, ok := h.quoteLocked(a.Symbol); ok {
			ev.Quotes = append(ev.Quotes, q)
		}
	}
	return ev
}

func (h *Hub) quoteLocked(sym string) (Quote, bool) {
	usd := h.rates.USD
	if sym == "USDT" {
		if usd <= 0 {
			return Quote{}, false
		}
		q := Quote{Symbol: "USDT", PriceUSD: 1, PriceBYN: usd, High24h: 1, Low24h: 1, Source: "Курс НБРБ", UpdatedAt: h.rates.UpdatedAt}
		if h.prevUSD > 0 {
			q.Change24h = (usd/h.prevUSD - 1) * 100
		}
		return q, true
	}
	if a, known := Find(sym); known && a.Kind == "bond" {
		return h.accrualQuoteLocked(a, usd)
	}
	q, ok := h.quotes[sym]
	if !ok || usd <= 0 {
		return Quote{}, false
	}
	q.PriceBYN = q.PriceUSD * usd
	q.Stale = time.Since(q.UpdatedAt) > 2*time.Minute
	return q, true
}

// accrualQuoteLocked — расчётная цена токена с фиксированной доходностью: номинал плюс накопленный доход.
func (h *Hub) accrualQuoteLocked(a Asset, usd float64) (Quote, bool) {
	if usd <= 0 {
		return Quote{}, false
	}
	now := time.Now()
	face := a.AccruedPrice(now)
	q := Quote{Symbol: a.Symbol, Change24h: float64(a.CouponBP) / 100 / 365, Source: "Расчётная цена", UpdatedAt: now}
	if a.FaceCurrency == "BYN" {
		q.PriceBYN, q.PriceUSD = face, face/usd
	} else {
		q.PriceUSD, q.PriceBYN = face, face*usd
	}
	q.High24h, q.Low24h = q.PriceUSD, q.PriceUSD
	return q, true
}

// Snapshot — все котировки и курсы.
func (h *Hub) Snapshot() Event {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.snapshotLocked()
}

// Quote — котировка одного актива.
func (h *Hub) Quote(sym string) (Quote, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	q, ok := h.quoteLocked(sym)
	if !ok {
		if _, known := Find(sym); !known {
			return Quote{}, ErrUnknownAsset
		}
		return Quote{}, ErrUnavailable
	}
	return q, nil
}

func (h *Hub) ratesStaleLocked() bool {
	return h.rates.UpdatedAt.IsZero() || time.Since(h.rates.UpdatedAt) > ratesFreshFor
}

// TradeQuote — котировка, пригодная для сделки (не старше 5 минут, курс НБРБ не старше суток).
func (h *Hub) TradeQuote(sym string) (Quote, error) {
	q, err := h.Quote(sym)
	if err != nil {
		return q, err
	}
	h.mu.RLock()
	stale := h.ratesStaleLocked()
	h.mu.RUnlock()
	if stale {
		return q, fmt.Errorf("%w: официальный курс НБРБ не обновлялся больше суток", ErrUnavailable)
	}
	if time.Since(q.UpdatedAt) > 5*time.Minute && sym != "USDT" {
		return q, fmt.Errorf("%w: котировка %s устарела", ErrUnavailable, sym)
	}
	return q, nil
}

func (h *Hub) Rates() Rates {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rates
}

// BuyPriceBYN / SellPriceBYN — цена с учётом спреда партнёра.
func (h *Hub) BuyPriceBYN(q Quote) float64  { return q.PriceBYN * (1 + float64(h.spreadBP)/10000) }
func (h *Hub) SellPriceBYN(q Quote) float64 { return q.PriceBYN * (1 - float64(h.spreadBP)/10000) }

// Subscribe — канал событий для SSE.
func (h *Hub) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 4)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *Hub) Status() []SourceStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var out []SourceStatus
	for _, s := range h.status {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// USDRateOn — официальный курс на дату (ближайший предыдущий доступный).
func (h *Hub) USDRateOn(t time.Time) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.usdRateOnLocked(t)
}

func (h *Hub) usdRateOnLocked(t time.Time) float64 {
	for i := 0; i < 10; i++ {
		if r, ok := h.usdHist[t.AddDate(0, 0, -i).UTC().Format("2006-01-02")]; ok {
			return r
		}
	}
	return h.rates.USD
}

var candleTTL = map[string]time.Duration{"15m": 30 * time.Second, "1h": time.Minute, "4h": 5 * time.Minute, "1d": 10 * time.Minute, "1w": time.Hour}

// Candles — свечи по активу. currency: "USD" или "BYN" (пересчёт по курсу НБРБ на дату свечи).
// Последняя свеча «доживляется» текущей ценой, поэтому график двигается в реальном времени.
func (h *Hub) Candles(ctx context.Context, sym, interval, currency string, limit int) ([]Candle, error) {
	a, ok := Find(sym)
	if !ok {
		return nil, ErrUnknownAsset
	}
	if _, ok := Intervals[interval]; !ok {
		return nil, errors.New("неподдерживаемый интервал")
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	var usdC []Candle
	if a.Kind == "stable" {
		usdC = h.stableCandles(interval, limit, currency)
		return usdC, nil
	}
	if a.Kind == "bond" {
		return h.accrualCandles(a, interval, limit, currency), nil
	}
	key := candleKey{sym, interval}
	h.mu.RLock()
	ent, ok := h.candles[key]
	h.mu.RUnlock()
	if !ok || time.Since(ent.at) > candleTTL[interval] || len(ent.candles) < limit {
		fresh, err := h.fetchCandles(ctx, a, interval, max(limit, 300), 0)
		if err == nil && len(fresh) > 0 {
			ent = candleEntry{at: time.Now(), candles: fresh}
			h.mu.Lock()
			h.candles[key] = ent
			h.mu.Unlock()
		} else if !ok {
			if err == nil {
				err = ErrUnavailable
			}
			return nil, err
		}
	}
	usdC = append([]Candle(nil), ent.candles...)
	if len(usdC) > limit {
		usdC = usdC[len(usdC)-limit:]
	}
	// доживляем последнюю свечу текущей ценой
	if q, err := h.Quote(sym); err == nil && len(usdC) > 0 && !q.Stale {
		step := int64(Intervals[interval] / time.Second)
		last := &usdC[len(usdC)-1]
		now := time.Now().Unix()
		if now >= last.T+step {
			usdC = append(usdC, Candle{T: now / step * step, O: last.C, H: math.Max(last.C, q.PriceUSD), L: math.Min(last.C, q.PriceUSD), C: q.PriceUSD})
		} else {
			last.C = q.PriceUSD
			last.H = math.Max(last.H, q.PriceUSD)
			last.L = math.Min(last.L, q.PriceUSD)
		}
	}
	if currency == "BYN" {
		h.mu.RLock()
		for i := range usdC {
			r := h.usdRateOnLocked(time.Unix(usdC[i].T, 0))
			c := &usdC[i]
			c.O, c.H, c.L, c.C = c.O*r, c.H*r, c.L*r, c.C*r
		}
		h.mu.RUnlock()
	}
	return usdC, nil
}

func (h *Hub) fetchCandles(ctx context.Context, a Asset, interval string, limit int, start int64) ([]Candle, error) {
	var lastErr error = ErrUnavailable
	sources := h.sources
	if a.Yahoo != "" && h.stockSrc != nil {
		sources = []Source{h.stockSrc}
	}
	for _, s := range sources {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		cs, err := s.Candles(c, a, interval, limit, start, 0)
		cancel()
		if err == nil && len(cs) > 0 {
			return cs, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	return nil, lastErr
}

// stableCandles — USDT: в USD цена 1, в BYN это официальный курс доллара по дням.
func (h *Hub) stableCandles(interval string, limit int, currency string) []Candle {
	h.mu.RLock()
	defer h.mu.RUnlock()
	step := Intervals[interval]
	if step < 24*time.Hour {
		step = 24 * time.Hour
	}
	now := time.Now().UTC().Truncate(24 * time.Hour)
	out := make([]Candle, 0, limit)
	for i := limit - 1; i >= 0; i-- {
		t := now.Add(-time.Duration(i) * step)
		v := 1.0
		if currency == "BYN" {
			v = h.usdRateOnLocked(t)
			if v <= 0 {
				continue
			}
		}
		out = append(out, Candle{T: t.Unix(), O: v, H: v, L: v, C: v})
	}
	// склеиваем открытие с предыдущим закрытием
	for i := 1; i < len(out); i++ {
		out[i].O = out[i-1].C
		out[i].H = math.Max(out[i].O, out[i].C)
		out[i].L = math.Min(out[i].O, out[i].C)
	}
	return out
}

// accrualCandles — «свечи» токена с фиксированной доходностью: цена растёт по ставке без колебаний.
func (h *Hub) accrualCandles(a Asset, interval string, limit int, currency string) []Candle {
	h.mu.RLock()
	defer h.mu.RUnlock()
	step := Intervals[interval]
	now := time.Now().UTC()
	out := make([]Candle, 0, limit)
	for i := limit - 1; i >= 0; i-- {
		t := now.Truncate(step).Add(-time.Duration(i) * step)
		if t.Before(accrualStart) {
			continue
		}
		face := a.AccruedPrice(t)
		var v float64
		switch {
		case a.FaceCurrency == currency || (currency != "BYN" && a.FaceCurrency == "USD"):
			v = face
		case a.FaceCurrency == "USD": // показываем в BYN
			v = face * h.usdRateOnLocked(t)
		default: // номинал в BYN, показываем в USD
			if r := h.usdRateOnLocked(t); r > 0 {
				v = face / r
			}
		}
		if v > 0 {
			out = append(out, Candle{T: t.Unix(), O: v, H: v, L: v, C: v})
		}
	}
	for i := 1; i < len(out); i++ {
		out[i].O = out[i-1].C
		out[i].H, out[i].L = math.Max(out[i].O, out[i].C), math.Min(out[i].O, out[i].C)
	}
	return out
}

// DailyHistory — полная дневная история в USD с даты from (для калькулятора). Кэш на 6 часов.
func (h *Hub) DailyHistory(ctx context.Context, sym string, from time.Time) ([]Candle, error) {
	a, ok := Find(sym)
	if !ok {
		return nil, ErrUnknownAsset
	}
	if a.Kind == "stable" {
		var out []Candle
		for d := from.UTC().Truncate(24 * time.Hour); d.Before(time.Now()); d = d.AddDate(0, 0, 1) {
			out = append(out, Candle{T: d.Unix(), O: 1, H: 1, L: 1, C: 1})
		}
		return out, nil
	}
	if a.Kind == "bond" {
		var out []Candle
		for d := from.UTC().Truncate(24 * time.Hour); d.Before(time.Now()); d = d.AddDate(0, 0, 1) {
			if d.Before(accrualStart) {
				continue
			}
			v := a.AccruedPrice(d)
			if a.FaceCurrency == "BYN" {
				r := h.USDRateOn(d)
				if r <= 0 {
					continue
				}
				v /= r
			}
			out = append(out, Candle{T: d.Unix(), O: v, H: v, L: v, C: v})
		}
		return out, nil
	}
	h.inflight.Lock()
	defer h.inflight.Unlock()
	h.mu.RLock()
	ent, ok := h.daily[sym]
	h.mu.RUnlock()
	if !ok || time.Since(ent.at) > 6*time.Hour {
		listed, _ := time.Parse("2006-01-02", a.ListedSince)
		start := listed.Unix()
		var all []Candle
		for i := 0; i < 15; i++ {
			cs, err := h.fetchCandles(ctx, a, "1d", 1000, start)
			if err != nil {
				if len(all) == 0 {
					return nil, err
				}
				break
			}
			if len(all) > 0 {
				for len(cs) > 0 && cs[0].T <= all[len(all)-1].T {
					cs = cs[1:]
				}
			}
			all = append(all, cs...)
			if len(cs) < 900 {
				break
			}
			start = cs[len(cs)-1].T + 86400
		}
		ent = candleEntry{at: time.Now(), candles: all}
		h.mu.Lock()
		h.daily[sym] = ent
		h.mu.Unlock()
	}
	fromU := from.Unix()
	i := sort.Search(len(ent.candles), func(i int) bool { return ent.candles[i].T >= fromU })
	return ent.candles[i:], nil
}

// HistoryReady — загружена ли история курса доллара.
func (h *Hub) HistoryReady() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.usdHist) > 0
}
