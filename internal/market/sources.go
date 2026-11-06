package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func getJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "AutoInvestBY/1.0")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d: %.200s", req.URL.Host, resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

func pf(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// ---------------------------------------------------------------- Binance

type Binance struct{ Base string }

func NewBinance() *Binance { return &Binance{Base: "https://api.binance.com"} }

func (b *Binance) Name() string { return "binance" }

func (b *Binance) Quotes(ctx context.Context, assets []Asset) (map[string]Quote, error) {
	var syms []string
	bySym := map[string]string{}
	for _, a := range assets {
		if a.Binance != "" {
			syms = append(syms, `"`+a.Binance+`"`)
			bySym[a.Binance] = a.Symbol
		}
	}
	var rows []struct {
		Symbol             string `json:"symbol"`
		LastPrice          string `json:"lastPrice"`
		PriceChangePercent string `json:"priceChangePercent"`
		HighPrice          string `json:"highPrice"`
		LowPrice           string `json:"lowPrice"`
		QuoteVolume        string `json:"quoteVolume"`
	}
	u := b.Base + "/api/v3/ticker/24hr?symbols=" + url.QueryEscape("["+strings.Join(syms, ",")+"]")
	if err := getJSON(ctx, u, &rows); err != nil {
		return nil, err
	}
	now := time.Now()
	out := map[string]Quote{}
	for _, r := range rows {
		s, ok := bySym[r.Symbol]
		if !ok || pf(r.LastPrice) <= 0 {
			continue
		}
		out[s] = Quote{Symbol: s, PriceUSD: pf(r.LastPrice), Change24h: pf(r.PriceChangePercent),
			High24h: pf(r.HighPrice), Low24h: pf(r.LowPrice), VolumeUSD: pf(r.QuoteVolume), Source: "Binance", UpdatedAt: now}
	}
	return out, nil
}

func (b *Binance) Candles(ctx context.Context, a Asset, interval string, limit int, start, end int64) ([]Candle, error) {
	if a.Binance == "" {
		return nil, ErrUnknownAsset
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	q := url.Values{}
	q.Set("symbol", a.Binance)
	q.Set("interval", interval)
	q.Set("limit", strconv.Itoa(limit))
	if start > 0 {
		q.Set("startTime", strconv.FormatInt(start*1000, 10))
	}
	if end > 0 {
		q.Set("endTime", strconv.FormatInt(end*1000, 10))
	}
	var raw [][]any
	if err := getJSON(ctx, b.Base+"/api/v3/klines?"+q.Encode(), &raw); err != nil {
		return nil, err
	}
	out := make([]Candle, 0, len(raw))
	for _, k := range raw {
		if len(k) < 6 {
			continue
		}
		ot, _ := k[0].(float64)
		c := Candle{T: int64(ot) / 1000}
		c.O, c.H, c.L, c.C, c.V = str(k[1]), str(k[2]), str(k[3]), str(k[4]), str(k[5])
		out = append(out, c)
	}
	return out, nil
}

func str(v any) float64 {
	switch x := v.(type) {
	case string:
		return pf(x)
	case float64:
		return x
	}
	return 0
}

// ---------------------------------------------------------------- Bybit

type Bybit struct{ Base string }

func NewBybit() *Bybit { return &Bybit{Base: "https://api.bybit.com"} }

func (b *Bybit) Name() string { return "bybit" }

func (b *Bybit) Quotes(ctx context.Context, assets []Asset) (map[string]Quote, error) {
	var resp struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List []struct {
				Symbol       string `json:"symbol"`
				LastPrice    string `json:"lastPrice"`
				Price24hPcnt string `json:"price24hPcnt"`
				HighPrice24h string `json:"highPrice24h"`
				LowPrice24h  string `json:"lowPrice24h"`
				Turnover24h  string `json:"turnover24h"`
			} `json:"list"`
		} `json:"result"`
	}
	if err := getJSON(ctx, b.Base+"/v5/market/tickers?category=spot", &resp); err != nil {
		return nil, err
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit retCode %d", resp.RetCode)
	}
	bySym := map[string]string{}
	for _, a := range assets {
		if a.Bybit != "" {
			bySym[a.Bybit] = a.Symbol
		}
	}
	now := time.Now()
	out := map[string]Quote{}
	for _, r := range resp.Result.List {
		s, ok := bySym[r.Symbol]
		if !ok || pf(r.LastPrice) <= 0 {
			continue
		}
		out[s] = Quote{Symbol: s, PriceUSD: pf(r.LastPrice), Change24h: pf(r.Price24hPcnt) * 100,
			High24h: pf(r.HighPrice24h), Low24h: pf(r.LowPrice24h), VolumeUSD: pf(r.Turnover24h), Source: "Bybit", UpdatedAt: now}
	}
	return out, nil
}

var bybitInterval = map[string]string{"15m": "15", "1h": "60", "4h": "240", "1d": "D", "1w": "W"}

func (b *Bybit) Candles(ctx context.Context, a Asset, interval string, limit int, start, end int64) ([]Candle, error) {
	iv, ok := bybitInterval[interval]
	if !ok || a.Bybit == "" {
		return nil, ErrUnknownAsset
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	q := url.Values{}
	q.Set("category", "spot")
	q.Set("symbol", a.Bybit)
	q.Set("interval", iv)
	q.Set("limit", strconv.Itoa(limit))
	if start > 0 {
		q.Set("start", strconv.FormatInt(start*1000, 10))
	}
	if end > 0 {
		q.Set("end", strconv.FormatInt(end*1000, 10))
	}
	var resp struct {
		RetCode int `json:"retCode"`
		Result  struct {
			List [][]string `json:"list"`
		} `json:"result"`
	}
	if err := getJSON(ctx, b.Base+"/v5/market/kline?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit retCode %d", resp.RetCode)
	}
	out := make([]Candle, 0, len(resp.Result.List))
	for _, k := range resp.Result.List {
		if len(k) < 6 {
			continue
		}
		ms, _ := strconv.ParseInt(k[0], 10, 64)
		out = append(out, Candle{T: ms / 1000, O: pf(k[1]), H: pf(k[2]), L: pf(k[3]), C: pf(k[4]), V: pf(k[5])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out, nil
}

// ---------------------------------------------------------------- CoinGecko (только котировки и дневные свечи)

type CoinGecko struct{ Base string }

func NewCoinGecko() *CoinGecko { return &CoinGecko{Base: "https://api.coingecko.com/api/v3"} }

func (c *CoinGecko) Name() string { return "coingecko" }

func (c *CoinGecko) Quotes(ctx context.Context, assets []Asset) (map[string]Quote, error) {
	var ids []string
	byID := map[string]string{}
	for _, a := range assets {
		if a.CoinGecko != "" && a.Kind != "stable" {
			ids = append(ids, a.CoinGecko)
			byID[a.CoinGecko] = a.Symbol
		}
	}
	var resp map[string]map[string]float64
	u := c.Base + "/simple/price?vs_currencies=usd&include_24hr_change=true&include_24hr_vol=true&ids=" + strings.Join(ids, ",")
	if err := getJSON(ctx, u, &resp); err != nil {
		return nil, err
	}
	now := time.Now()
	out := map[string]Quote{}
	for id, v := range resp {
		s := byID[id]
		if s == "" || v["usd"] <= 0 {
			continue
		}
		out[s] = Quote{Symbol: s, PriceUSD: v["usd"], Change24h: v["usd_24h_change"], VolumeUSD: v["usd_24h_vol"], Source: "CoinGecko", UpdatedAt: now}
	}
	return out, nil
}

func (c *CoinGecko) Candles(ctx context.Context, a Asset, interval string, limit int, start, end int64) ([]Candle, error) {
	if a.CoinGecko == "" || (interval != "1d" && interval != "4h") {
		return nil, ErrUnknownAsset
	}
	days := "90"
	if interval == "1d" {
		days = "365"
	}
	var raw [][]float64
	if err := getJSON(ctx, c.Base+"/coins/"+a.CoinGecko+"/ohlc?vs_currency=usd&days="+days, &raw); err != nil {
		return nil, err
	}
	out := make([]Candle, 0, len(raw))
	for _, k := range raw {
		if len(k) >= 5 {
			out = append(out, Candle{T: int64(k[0]) / 1000, O: k[1], H: k[2], L: k[3], C: k[4]})
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- НБРБ

type NBRB struct{ Base string }

func NewNBRB() *NBRB { return &NBRB{Base: "https://api.nbrb.by"} }

type nbrbRate struct {
	CurID           int     `json:"Cur_ID"`
	Date            string  `json:"Date"`
	CurAbbreviation string  `json:"Cur_Abbreviation"`
	CurScale        int     `json:"Cur_Scale"`
	CurOfficialRate float64 `json:"Cur_OfficialRate"`
}

func (n *NBRB) Current(ctx context.Context) (Rates, error) {
	var list []nbrbRate
	if err := getJSON(ctx, n.Base+"/exrates/rates?periodicity=0", &list); err != nil {
		return Rates{}, err
	}
	r := Rates{UpdatedAt: time.Now(), Source: "НБРБ"}
	for _, x := range list {
		if x.CurScale <= 0 {
			continue
		}
		switch x.CurAbbreviation {
		case "USD":
			r.USD = x.CurOfficialRate / float64(x.CurScale)
			if len(x.Date) >= 10 {
				r.Date = x.Date[:10]
			}
		case "EUR":
			r.EUR = x.CurOfficialRate / float64(x.CurScale)
		case "RUB":
			r.RUB100 = x.CurOfficialRate / float64(x.CurScale) * 100
		}
	}
	if r.USD <= 0 {
		return Rates{}, errors.New("НБРБ не вернул курс доллара")
	}
	return r, nil
}

// USDHistory — динамика официального курса USD. API НБРБ отдаёт не больше года за запрос.
func (n *NBRB) USDHistory(ctx context.Context, from, to time.Time) (map[string]float64, error) {
	var cur nbrbRate
	if err := getJSON(ctx, n.Base+"/exrates/rates/USD?parammode=2", &cur); err != nil {
		return nil, err
	}
	id := cur.CurID
	if id == 0 {
		id = 431
	}
	out := map[string]float64{}
	for s := from; s.Before(to); s = s.AddDate(1, 0, 0) {
		e := s.AddDate(1, 0, -1)
		if e.After(to) {
			e = to
		}
		var list []nbrbRate
		u := fmt.Sprintf("%s/exrates/rates/dynamics/%d?startdate=%s&enddate=%s", n.Base, id, s.Format("2006-01-02"), e.Format("2006-01-02"))
		if err := getJSON(ctx, u, &list); err != nil {
			return nil, err
		}
		for _, x := range list {
			if len(x.Date) >= 10 && x.CurOfficialRate > 0 {
				out[x.Date[:10]] = x.CurOfficialRate
			}
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- Yahoo Finance (акции и фонды США)

// Yahoo — котировки и свечи акций из публичного chart API Yahoo Finance.
// Перед коммерческим запуском источник нужно заменить на лицензированный (Finnhub, Polygon и т.п.):
// условия Yahoo не разрешают коммерческое использование данных.
type Yahoo struct{ Base string }

func NewYahoo() *Yahoo { return &Yahoo{Base: "https://query1.finance.yahoo.com"} }

func (y *Yahoo) Name() string { return "yahoo" }

type yahooChart struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Price     float64 `json:"regularMarketPrice"`
				PrevClose float64 `json:"chartPreviousClose"`
				DayHigh   float64 `json:"regularMarketDayHigh"`
				DayLow    float64 `json:"regularMarketDayLow"`
				Volume    float64 `json:"regularMarketVolume"`
				Time      int64   `json:"regularMarketTime"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*float64 `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

func (y *Yahoo) chart(ctx context.Context, ticker string, q url.Values) (*yahooChart, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, y.Base+"/v8/finance/chart/"+url.PathEscape(ticker)+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	// без браузерного User-Agent Yahoo отвечает 429
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AutoInvestBY/1.0)")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("yahoo %s: HTTP %d", ticker, resp.StatusCode)
	}
	var c yahooChart
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, err
	}
	if c.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo %s: %s", ticker, c.Chart.Error.Description)
	}
	if len(c.Chart.Result) == 0 {
		return nil, fmt.Errorf("yahoo %s: пустой ответ", ticker)
	}
	return &c, nil
}

func (y *Yahoo) Quotes(ctx context.Context, assets []Asset) (map[string]Quote, error) {
	out := map[string]Quote{}
	var lastErr error
	for _, a := range assets {
		if a.Yahoo == "" {
			continue
		}
		c, err := y.chart(ctx, a.Yahoo, url.Values{"interval": {"1d"}, "range": {"1d"}})
		if err != nil {
			lastErr = err
			continue
		}
		m := c.Chart.Result[0].Meta
		if m.Price <= 0 {
			continue
		}
		q := Quote{Symbol: a.Symbol, PriceUSD: m.Price, High24h: m.DayHigh, Low24h: m.DayLow, VolumeUSD: m.Volume * m.Price,
			Source: "Yahoo Finance", UpdatedAt: time.Now()}
		if m.PrevClose > 0 {
			q.Change24h = (m.Price/m.PrevClose - 1) * 100
		}
		out[a.Symbol] = q
	}
	if len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

func (y *Yahoo) Candles(ctx context.Context, a Asset, interval string, limit int, start, end int64) ([]Candle, error) {
	if a.Yahoo == "" {
		return nil, ErrUnknownAsset
	}
	yi := map[string]string{"15m": "15m", "1h": "1h", "4h": "1h", "1d": "1d", "1w": "1wk"}[interval]
	q := url.Values{"interval": {yi}}
	if start > 0 {
		if end <= 0 {
			end = time.Now().Unix()
		}
		q.Set("period1", strconv.FormatInt(start, 10))
		q.Set("period2", strconv.FormatInt(end, 10))
	} else {
		q.Set("range", map[string]string{"15m": "1mo", "1h": "6mo", "4h": "2y", "1d": "10y", "1w": "5y"}[interval])
	}
	c, err := y.chart(ctx, a.Yahoo, q)
	if err != nil {
		return nil, err
	}
	r := c.Chart.Result[0]
	if len(r.Indicators.Quote) == 0 {
		return nil, ErrUnavailable
	}
	k := r.Indicators.Quote[0]
	var out []Candle
	for i, t := range r.Timestamp {
		// у Yahoo бывают пустые точки — пропускаем, а не подставляем нули
		if i >= len(k.Close) || k.Open[i] == nil || k.High[i] == nil || k.Low[i] == nil || k.Close[i] == nil {
			continue
		}
		var v float64
		if i < len(k.Volume) && k.Volume[i] != nil {
			v = *k.Volume[i]
		}
		out = append(out, Candle{T: t, O: *k.Open[i], H: *k.High[i], L: *k.Low[i], C: *k.Close[i], V: v})
	}
	if interval == "4h" {
		out = aggregate(out, 4*3600)
	}
	if limit > 0 && len(out) > limit && start == 0 {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// aggregate склеивает свечи в более крупный интервал step (секунды).
func aggregate(in []Candle, step int64) []Candle {
	var out []Candle
	for _, c := range in {
		b := c.T / step * step
		if n := len(out); n > 0 && out[n-1].T == b {
			l := &out[n-1]
			l.H, l.L, l.C, l.V = max(l.H, c.H), min(l.L, c.L), c.C, l.V+c.V
			continue
		}
		c.T = b
		out = append(out, c)
	}
	return out
}
