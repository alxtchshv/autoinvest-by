package market

import (
	"context"
	"hash/fnv"
	"math"
	"math/rand"
	"time"
)

// Fixture — ОФЛАЙН-источник только для разработки без доступа к биржам (MARKET=fixture).
// В production запрещён конфигом, а интерфейс показывает плашку «демо-данные».
type Fixture struct{}

var fixtureBase = map[string]float64{"BTC": 96000, "ETH": 3400, "TON": 3.2, "SOL": 185, "PAXG": 3950,
	"SPY": 680, "AAPL": 270, "MSFT": 520, "NVDA": 190, "TSLA": 430}

func (Fixture) Name() string { return "fixture" }

func (f Fixture) Quotes(ctx context.Context, assets []Asset) (map[string]Quote, error) {
	out := map[string]Quote{}
	now := time.Now()
	for _, a := range assets {
		if a.Kind == "stable" || a.Kind == "bond" {
			continue
		}
		cs, _ := f.Candles(ctx, a, "1h", 25, 0, 0)
		last := cs[len(cs)-1]
		first := cs[0]
		// небольшой «живой» шум, чтобы в разработке было видно обновления
		wobble := 1 + 0.0008*math.Sin(float64(now.Unix())/7+float64(len(a.Symbol)))
		p := last.C * wobble
		hi, lo := 0.0, math.MaxFloat64
		for _, c := range cs {
			hi, lo = math.Max(hi, c.H), math.Min(lo, c.L)
		}
		out[a.Symbol] = Quote{Symbol: a.Symbol, PriceUSD: p, Change24h: (p/first.O - 1) * 100,
			High24h: hi, Low24h: lo, VolumeUSD: 1e9, Source: "Демо", UpdatedAt: now}
	}
	return out, nil
}

func (Fixture) Candles(ctx context.Context, a Asset, interval string, limit int, start, end int64) ([]Candle, error) {
	step := int64(Intervals[interval] / time.Second)
	if step == 0 {
		step = 3600
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	if end <= 0 {
		end = time.Now().Unix()
	}
	end = end / step * step
	if start > 0 {
		n := int((end-start)/step) + 1
		if n < limit {
			limit = n
		}
	}
	base := fixtureBase[a.Symbol]
	if base == 0 {
		base = 1
	}
	// Детерминированное блуждание от «сегодня» назад, чтобы разные запросы стыковались.
	h := fnv.New64a()
	h.Write([]byte(a.Symbol + interval))
	r := rand.New(rand.NewSource(int64(h.Sum64())))
	vol := 0.012 * math.Sqrt(float64(step)/3600)
	if a.Kind == "gold" {
		vol /= 4
	}
	total := int((time.Now().Unix()/step*step-end)/step) + limit
	closes := make([]float64, total)
	p := base
	for i := total - 1; i >= 0; i-- {
		closes[i] = p
		p = p / math.Exp(r.NormFloat64()*vol+vol*vol*0.05)
	}
	closes = closes[:limit]
	out := make([]Candle, limit)
	for i := range out {
		c := closes[i]
		o := c * (1 + r.NormFloat64()*vol/3)
		if i > 0 {
			o = closes[i-1]
		}
		out[i] = Candle{T: end - int64(limit-1-i)*step, O: o, C: c,
			H: math.Max(o, c) * (1 + math.Abs(r.NormFloat64())*vol/2),
			L: math.Min(o, c) * (1 - math.Abs(r.NormFloat64())*vol/2), V: 100 + r.Float64()*1000}
	}
	return out, nil
}

type FixtureRates struct{}

func (FixtureRates) Current(ctx context.Context) (Rates, error) {
	return Rates{USD: 2.9612, EUR: 3.4410, RUB100: 3.6501, Date: time.Now().Format("2006-01-02"), UpdatedAt: time.Now(), Source: "Демо"}, nil
}

func (FixtureRates) USDHistory(ctx context.Context, from, to time.Time) (map[string]float64, error) {
	out := map[string]float64{}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		years := to.Sub(d).Hours() / 24 / 365
		out[d.Format("2006-01-02")] = 2.9612 / math.Pow(1.06, years) * (1 + 0.01*math.Sin(float64(d.Unix())/2e6))
	}
	return out, nil
}
