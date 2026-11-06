package engine

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"

	"autoinvest/internal/market"
)

// BacktestRequest — «машина времени»: что было бы, если копить по плану на реальной истории цен.
type BacktestRequest struct {
	Weights     map[string]int64 `json:"weights"` // базисные пункты
	AmountKop   int64            `json:"amount_kop"`
	Frequency   string           `json:"frequency"` // daily | weekly | monthly
	Years       float64          `json:"years"`
	DepositRate float64          `json:"deposit_rate"` // годовая ставка депозита в BYN для сравнения, %
}

type BacktestPoint struct {
	T        int64 `json:"t"`
	Invested int64 `json:"invested_kop"`
	Value    int64 `json:"value_kop"`
	USD      int64 `json:"usd_kop"`     // те же деньги, купленные долларами по курсу НБРБ
	Deposit  int64 `json:"deposit_kop"` // те же деньги на депозите
}

type BacktestResult struct {
	Points       []BacktestPoint `json:"points"`
	InvestedKop  int64           `json:"invested_kop"`
	ValueKop     int64           `json:"value_kop"`
	USDKop       int64           `json:"usd_kop"`
	DepositKop   int64           `json:"deposit_kop"`
	ProfitPct    float64         `json:"profit_pct"`
	MaxDrawdown  float64         `json:"max_drawdown"` // худшая просадка стоимости от пика, %
	Purchases    int             `json:"purchases"`
	From         string          `json:"from"`
	To           string          `json:"to"`
	Note         string          `json:"note"`
	RatesHistory bool            `json:"rates_history"`
}

func (e *Engine) Backtest(ctx context.Context, r BacktestRequest) (*BacktestResult, error) {
	if r.AmountKop < 100 || r.AmountKop > 10_000_000 {
		return nil, errors.New("Сумма пополнения от 1 до 100 000 BYN")
	}
	var sum int64
	for s, w := range r.Weights {
		if _, ok := market.Find(s); !ok || w < 0 {
			return nil, errors.New("неизвестный актив " + s)
		}
		sum += w
	}
	if sum != 10000 {
		return nil, errors.New("Сумма долей должна быть ровно 100%")
	}
	if r.Years <= 0 || r.Years > 9 {
		r.Years = 3
	}
	step := 7 * 24 * time.Hour
	switch r.Frequency {
	case "daily":
		step = 24 * time.Hour
	case "monthly":
		step = 0 // календарный месяц
	}
	now := time.Now().UTC().Truncate(24 * time.Hour)
	from := now.AddDate(0, -int(math.Round(r.Years*12)), 0)
	note := ""
	// начало истории ограничено самым молодым активом
	for s, w := range r.Weights {
		if w == 0 {
			continue
		}
		a, _ := market.Find(s)
		listed, _ := time.Parse("2006-01-02", a.ListedSince)
		if listed.After(from) {
			from = listed.AddDate(0, 0, 1)
			note = "История " + a.Name + " на бирже начинается с " + listed.Format("02.01.2006") + ", расчёт начат с этой даты."
		}
	}
	prices := map[string]map[int64]float64{}
	for s, w := range r.Weights {
		if w == 0 {
			continue
		}
		cs, err := e.Hub.DailyHistory(ctx, s, from.AddDate(0, 0, -3))
		if err != nil {
			return nil, ErrMarket
		}
		m := map[int64]float64{}
		for _, c := range cs {
			m[time.Unix(c.T, 0).UTC().Truncate(24*time.Hour).Unix()] = c.C
		}
		prices[s] = m
	}
	priceOn := func(s string, d time.Time) float64 {
		for i := 0; i < 5; i++ {
			if v, ok := prices[s][d.AddDate(0, 0, -i).Unix()]; ok {
				return v
			}
		}
		return 0
	}
	ratesOK := e.Hub.HistoryReady()
	rate := func(d time.Time) float64 { return e.Hub.USDRateOn(d) }
	if rate(now) <= 0 {
		return nil, ErrMarket
	}
	spread := float64(e.Cfg.SpreadBP) / 10000
	fee := float64(e.Cfg.FeeFreeBP) / 10000
	depDaily := math.Pow(1+r.DepositRate/100, 1.0/365) - 1

	qty := map[string]float64{}
	var invested int64
	var usd, deposit float64
	res := &BacktestResult{From: from.Format("2006-01-02"), To: now.Format("2006-01-02"), Note: note, RatesHistory: ratesOK}
	var peak float64
	nextBuy := from
	lastPoint := time.Time{}
	for d := from; !d.After(now); d = d.AddDate(0, 0, 1) {
		deposit *= 1 + depDaily
		if !d.Before(nextBuy) {
			rt := rate(d)
			net := float64(r.AmountKop) / 100 * (1 - fee)
			for s, w := range r.Weights {
				if w == 0 {
					continue
				}
				p := priceOn(s, d) * rt * (1 + spread)
				if p > 0 {
					qty[s] += net * float64(w) / 10000 / p
				}
			}
			invested += r.AmountKop
			usd += float64(r.AmountKop) / 100 / rt
			deposit += float64(r.AmountKop) / 100
			res.Purchases++
			if step == 0 {
				nextBuy = nextBuy.AddDate(0, 1, 0)
			} else {
				nextBuy = nextBuy.Add(step)
			}
		}
		rt := rate(d)
		var value float64
		for s, q := range qty {
			value += q * priceOn(s, d) * rt
		}
		if value > peak {
			peak = value
		}
		if peak > 0 {
			if dd := (value/peak - 1) * 100; dd < res.MaxDrawdown {
				res.MaxDrawdown = dd
			}
		}
		// точки графика: раз в неделю + последний день
		if d.Sub(lastPoint) >= 7*24*time.Hour || d.Equal(now) {
			res.Points = append(res.Points, BacktestPoint{T: d.Unix(), Invested: invested, Value: int64(math.Round(value * 100)),
				USD: int64(math.Round(usd * rt * 100)), Deposit: int64(math.Round(deposit * 100))})
			lastPoint = d
		}
	}
	sort.Slice(res.Points, func(i, j int) bool { return res.Points[i].T < res.Points[j].T })
	if n := len(res.Points); n > 0 {
		last := res.Points[n-1]
		res.InvestedKop, res.ValueKop, res.USDKop, res.DepositKop = last.Invested, last.Value, last.USD, last.Deposit
		if last.Invested > 0 {
			res.ProfitPct = float64(last.Value-last.Invested) / float64(last.Invested) * 100
		}
	}
	return res, nil
}
