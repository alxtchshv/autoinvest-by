package market

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBinanceParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/ticker/24hr":
			if !strings.Contains(r.URL.Query().Get("symbols"), "BTCUSDT") {
				t.Error("symbols missing")
			}
			io.WriteString(w, `[{"symbol":"BTCUSDT","lastPrice":"95000.10","priceChangePercent":"-2.5","highPrice":"97000","lowPrice":"94000","quoteVolume":"123"}]`)
		case "/api/v3/klines":
			io.WriteString(w, `[[1700000000000,"1","2","0.5","1.5","10",1700003599999,"15",1,"1","1","0"]]`)
		}
	}))
	defer srv.Close()
	b := &Binance{Base: srv.URL}
	q, err := b.Quotes(context.Background(), Catalog)
	if err != nil || q["BTC"].PriceUSD != 95000.10 || q["BTC"].Change24h != -2.5 {
		t.Fatalf("%v %+v", err, q)
	}
	a, _ := Find("BTC")
	cs, err := b.Candles(context.Background(), a, "1h", 10, 0, 0)
	if err != nil || len(cs) != 1 || cs[0].T != 1700000000 || cs[0].C != 1.5 {
		t.Fatalf("%v %+v", err, cs)
	}
}

func TestBybitParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v5/market/tickers" {
			io.WriteString(w, `{"retCode":0,"result":{"list":[{"symbol":"ETHUSDT","lastPrice":"3400","price24hPcnt":"0.0123","highPrice24h":"3500","lowPrice24h":"3300","turnover24h":"9"}]}}`)
			return
		}
		io.WriteString(w, `{"retCode":0,"result":{"list":[["1700003600000","2","3","1","2.5","5","1"],["1700000000000","1","2","0.5","1.5","10","1"]]}}`)
	}))
	defer srv.Close()
	b := &Bybit{Base: srv.URL}
	q, err := b.Quotes(context.Background(), Catalog)
	if err != nil || q["ETH"].PriceUSD != 3400 || q["ETH"].Change24h < 1.22 || q["ETH"].Change24h > 1.24 {
		t.Fatalf("%v %+v", err, q)
	}
	a, _ := Find("ETH")
	cs, _ := b.Candles(context.Background(), a, "1h", 2, 0, 0)
	if len(cs) != 2 || cs[0].T > cs[1].T {
		t.Fatal("bybit candles must be sorted ascending")
	}
}

func TestNBRB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/exrates/rates":
			io.WriteString(w, `[{"Cur_ID":431,"Date":"2026-11-05T00:00:00","Cur_Abbreviation":"USD","Cur_Scale":1,"Cur_OfficialRate":2.9612},{"Cur_Abbreviation":"RUB","Cur_Scale":100,"Cur_OfficialRate":3.65},{"Cur_Abbreviation":"EUR","Cur_Scale":1,"Cur_OfficialRate":3.44}]`)
		case r.URL.Path == "/exrates/rates/USD":
			io.WriteString(w, `{"Cur_ID":431}`)
		default:
			io.WriteString(w, `[{"Date":"2026-11-04T00:00:00","Cur_OfficialRate":2.95}]`)
		}
	}))
	defer srv.Close()
	n := &NBRB{Base: srv.URL}
	r, err := n.Current(context.Background())
	if err != nil || r.USD != 2.9612 || r.RUB100 < 3.64 || r.Date != "2026-11-05" {
		t.Fatalf("%v %+v", err, r)
	}
	h, err := n.USDHistory(context.Background(), time.Now().AddDate(0, 0, -3), time.Now())
	if err != nil || h["2026-11-04"] != 2.95 {
		t.Fatalf("%v %v", err, h)
	}
}

type failing struct{}

func (failing) Name() string                                              { return "fail" }
func (failing) Quotes(context.Context, []Asset) (map[string]Quote, error) { return nil, ErrUnavailable }
func (failing) Candles(context.Context, Asset, string, int, int64, int64) ([]Candle, error) {
	return nil, ErrUnavailable
}

func TestHubFallbackAndBYN(t *testing.T) {
	h := NewHub([]Source{failing{}, Fixture{}}, FixtureRates{}, 100, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.Start(ctx)
	q, err := h.Quote("BTC")
	if err != nil || q.Source != "Демо" {
		t.Fatalf("fallback failed: %v %+v", err, q)
	}
	if q.PriceBYN < q.PriceUSD*2.9 || q.PriceBYN > q.PriceUSD*3 {
		t.Errorf("BYN conversion %v %v", q.PriceBYN, q.PriceUSD)
	}
	u, _ := h.Quote("USDT")
	if u.PriceUSD != 1 || u.PriceBYN != 2.9612 {
		t.Errorf("USDT %+v", u)
	}
	if b := h.BuyPriceBYN(q); b <= q.PriceBYN {
		t.Error("spread not applied")
	}
	cs, err := h.Candles(ctx, "BTC", "1h", "BYN", 50)
	if err != nil || len(cs) < 50 {
		t.Fatalf("%v %d", err, len(cs))
	}
	if last := cs[len(cs)-1].C; last < q.PriceBYN*0.98 || last > q.PriceBYN*1.02 {
		t.Errorf("last candle not live: %v vs %v", last, q.PriceBYN)
	}
}

type downRates struct{}

func (downRates) Current(context.Context) (Rates, error) { return Rates{}, ErrUnavailable }
func (downRates) USDHistory(context.Context, time.Time, time.Time) (map[string]float64, error) {
	return nil, ErrUnavailable
}

type memRates struct{ r *Rates }

func (m *memRates) LoadRates(context.Context) (Rates, bool) {
	if m.r == nil {
		return Rates{}, false
	}
	return *m.r, true
}
func (m *memRates) SaveRates(_ context.Context, r Rates) { m.r = &r }

func TestNoInventedRateWhenNBRBIsDown(t *testing.T) {
	h := NewHub([]Source{Fixture{}}, downRates{}, 100, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.refreshRates(context.Background())
	h.refreshQuotes(context.Background())
	if r := h.Rates(); r.USD != 0 {
		t.Fatalf("rate must not be invented: %+v", r)
	}
	if _, err := h.TradeQuote("BTC"); err == nil {
		t.Fatal("trading without an official rate must be refused")
	}
}

func TestSavedRateIsShownButTooOldToTrade(t *testing.T) {
	saved := Rates{USD: 2.98, UpdatedAt: time.Now().Add(-48 * time.Hour), Date: "2026-11-04"}
	h := NewHub([]Source{Fixture{}}, downRates{}, 100, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.SetRatePersister(&memRates{r: &saved})
	h.refreshRates(context.Background())
	h.refreshQuotes(context.Background())
	if r := h.Snapshot().Rates; r.USD != 2.98 || !r.Stale {
		t.Fatalf("saved rate must be shown and marked stale: %+v", r)
	}
	if _, err := h.TradeQuote("BTC"); err == nil {
		t.Fatal("a rate older than a day must not be used for trades")
	}
}

func TestFreshRateIsPersisted(t *testing.T) {
	m := &memRates{}
	h := NewHub([]Source{Fixture{}}, FixtureRates{}, 100, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	h.SetRatePersister(m)
	h.refreshRates(context.Background())
	if m.r == nil || m.r.USD != 2.9612 {
		t.Fatalf("rate not persisted: %+v", m.r)
	}
}

func TestStocksAndBondsArePricedByTheirOwnSources(t *testing.T) {
	h := NewFixtureHub(100, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.Start(ctx)
	if q, err := h.Quote("SPY"); err != nil || q.PriceUSD <= 0 {
		t.Fatalf("stock quote: %+v %v", q, err)
	}
	usd, _ := h.Quote("BYCORP_USD")
	byn, _ := h.Quote("BYCORP_BYN")
	if usd.PriceUSD < 1 || usd.PriceUSD > 1.2 || byn.PriceBYN < 1 || byn.PriceBYN > 1.3 {
		t.Fatalf("accrual prices out of range: %+v %+v", usd, byn)
	}
	cs, err := h.Candles(ctx, "BYCORP_USD", "1d", "USD", 30)
	if err != nil || len(cs) == 0 || cs[len(cs)-1].C < cs[0].C {
		t.Fatalf("accrual candles must grow: %v %d", err, len(cs))
	}
	for _, a := range Catalog {
		if (a.Kind == "stock" || a.Kind == "bond") && !a.ProOnly {
			t.Errorf("%s must be Pro-only", a.Symbol)
		}
		if a.Kind == "bond" && !a.Indicative {
			t.Errorf("%s has no market data and must be flagged indicative", a.Symbol)
		}
	}
}

func TestYahooSkipsEmptyPoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"chart":{"result":[{"meta":{"regularMarketPrice":101,"chartPreviousClose":100},"timestamp":[1,2,3],
			"indicators":{"quote":[{"open":[10,null,12],"high":[11,null,13],"low":[9,null,11],"close":[10.5,null,12.5],"volume":[1,null,3]}]}}]}}`))
	}))
	defer srv.Close()
	y := &Yahoo{Base: srv.URL}
	a, _ := Find("SPY")
	cs, err := y.Candles(context.Background(), a, "1d", 10, 0, 0)
	if err != nil || len(cs) != 2 || cs[1].T != 3 {
		t.Fatalf("%v %+v", err, cs)
	}
	q, err := y.Quotes(context.Background(), []Asset{a})
	if err != nil || q["SPY"].Change24h < 0.99 || q["SPY"].Change24h > 1.01 {
		t.Fatalf("%v %+v", err, q)
	}
}
