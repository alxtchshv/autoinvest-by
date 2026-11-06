package market

import (
	"math"
	"time"
)

// Stats — риск-метрики по дневным свечам (в USD) за последний год.
type Stats struct {
	Return1y    float64 `json:"return_1y"`    // %
	Return30d   float64 `json:"return_30d"`   // %
	Volatility  float64 `json:"volatility"`   // годовая, %
	MaxDrawdown float64 `json:"max_drawdown"` // %, отрицательное
	High1y      float64 `json:"high_1y"`
	Low1y       float64 `json:"low_1y"`
	Days        int     `json:"days"`
	WorstDay    float64 `json:"worst_day"` // %
	BestDay     float64 `json:"best_day"`  // %
}

// ComputeStats считает метрики по дневным закрытиям.
func ComputeStats(daily []Candle) Stats {
	cut := time.Now().AddDate(-1, 0, 0).Unix()
	var cs []Candle
	for _, c := range daily {
		if c.T >= cut && c.C > 0 {
			cs = append(cs, c)
		}
	}
	s := Stats{Days: len(cs)}
	if len(cs) < 2 {
		return s
	}
	first, last := cs[0].C, cs[len(cs)-1].C
	s.Return1y = (last/first - 1) * 100
	if len(cs) > 30 {
		s.Return30d = (last/cs[len(cs)-31].C - 1) * 100
	}
	var sum, sq float64
	peak := cs[0].C
	s.High1y, s.Low1y = cs[0].H, cs[0].L
	s.BestDay, s.WorstDay = -math.MaxFloat64, math.MaxFloat64
	for i := 1; i < len(cs); i++ {
		r := math.Log(cs[i].C / cs[i-1].C)
		sum += r
		sq += r * r
		pct := (cs[i].C/cs[i-1].C - 1) * 100
		s.BestDay = math.Max(s.BestDay, pct)
		s.WorstDay = math.Min(s.WorstDay, pct)
		if cs[i].C > peak {
			peak = cs[i].C
		}
		dd := (cs[i].C/peak - 1) * 100
		if dd < s.MaxDrawdown {
			s.MaxDrawdown = dd
		}
		s.High1y = math.Max(s.High1y, cs[i].H)
		if cs[i].L > 0 {
			s.Low1y = math.Min(s.Low1y, cs[i].L)
		}
	}
	n := float64(len(cs) - 1)
	mean := sum / n
	variance := sq/n - mean*mean
	if variance > 0 {
		s.Volatility = math.Sqrt(variance) * math.Sqrt(365) * 100
	}
	return s
}
