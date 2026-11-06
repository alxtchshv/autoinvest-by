// Package market — котировки и свечи с реальных источников (Binance, Bybit, CoinGecko)
// и официальные курсы НБРБ для пересчёта в белорусские рубли.
package market

import (
	"context"
	"errors"
	"time"
)

// Asset — актив из каталога сервиса.
type Asset struct {
	Symbol      string `json:"symbol"`
	Name        string `json:"name"`
	Kind        string `json:"kind"` // stable | gold | crypto | stock | bond
	Risk        int    `json:"risk"` // 1 (низкий) … 5 (очень высокий)
	Color       string `json:"color"`
	Short       string `json:"short"`
	Description string `json:"description"`
	Binance     string `json:"-"`
	Bybit       string `json:"-"`
	CoinGecko   string `json:"-"`
	Yahoo       string `json:"-"`            // тикер базовой акции/фонда для котировок
	ListedSince string `json:"listed_since"` // дата, с которой есть история на Binance
	ProOnly     bool   `json:"pro_only"`     // доступен в копилках только на тарифе Pro
	Indicative  bool   `json:"indicative"`   // цена расчётная (модель доходности), биржевых торгов нет
	// Для токенов с фиксированной доходностью: ставка (базисные пункты годовых) и валюта номинала.
	CouponBP     int64  `json:"coupon_bp,omitempty"`
	FaceCurrency string `json:"face_currency,omitempty"`
}

// accrualStart — дата, от которой считается расчётная цена токенов с фиксированной доходностью.
var accrualStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// AccruedPrice — цена токена с фиксированной доходностью на момент t в валюте номинала (номинал 1).
func (a Asset) AccruedPrice(t time.Time) float64 {
	days := t.Sub(accrualStart).Hours() / 24
	if days < 0 {
		days = 0
	}
	return 1 + float64(a.CouponBP)/10000*days/365
}

// Quote — текущая котировка.
type Quote struct {
	Symbol    string    `json:"symbol"`
	PriceUSD  float64   `json:"price_usd"`
	PriceBYN  float64   `json:"price_byn"`
	Change24h float64   `json:"change_24h"` // в процентах
	High24h   float64   `json:"high_24h"`
	Low24h    float64   `json:"low_24h"`
	VolumeUSD float64   `json:"volume_usd"`
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updated_at"`
	Stale     bool      `json:"stale"`
}

// Candle — OHLCV, T — время открытия (unix, сек).
type Candle struct {
	T int64   `json:"t"`
	O float64 `json:"o"`
	H float64 `json:"h"`
	L float64 `json:"l"`
	C float64 `json:"c"`
	V float64 `json:"v"`
}

// Rates — официальные курсы НБРБ (BYN за 1 единицу валюты).
type Rates struct {
	USD       float64   `json:"usd"`
	EUR       float64   `json:"eur"`
	RUB100    float64   `json:"rub100"` // за 100 RUB
	Date      string    `json:"date"`
	UpdatedAt time.Time `json:"updated_at"`
	Source    string    `json:"source"`
	Stale     bool      `json:"stale"`
}

// Интервалы свечей, которые поддерживает сервис.
var Intervals = map[string]time.Duration{
	"15m": 15 * time.Minute,
	"1h":  time.Hour,
	"4h":  4 * time.Hour,
	"1d":  24 * time.Hour,
	"1w":  7 * 24 * time.Hour,
}

// Source — поставщик биржевых данных.
type Source interface {
	Name() string
	Quotes(ctx context.Context, assets []Asset) (map[string]Quote, error)
	// Candles возвращает свечи в USD, отсортированные по времени. end=0 — до текущего момента.
	Candles(ctx context.Context, a Asset, interval string, limit int, start, end int64) ([]Candle, error)
}

// RateSource — поставщик курсов BYN.
type RateSource interface {
	Current(ctx context.Context) (Rates, error)
	// USDHistory: дата "2006-01-02" → BYN за 1 USD.
	USDHistory(ctx context.Context, from, to time.Time) (map[string]float64, error)
}

var ErrUnavailable = errors.New("рыночные данные временно недоступны")
var ErrUnknownAsset = errors.New("неизвестный актив")
