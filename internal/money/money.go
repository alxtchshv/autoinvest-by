// Package money — работа с суммами в копейках и количеством актива в 1e-8 долях.
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// QtyScale — 1 единица актива = 1e8 «сатоши».
const QtyScale = 100_000_000

// FormatBYN: 123456 → "1 234,56 руб.". Знак рубля (U+20C5) пока не отображается в почте и Telegram,
// поэтому в серверных текстах пишем «руб.»; на сайте суммы форматирует фронтенд со знаком.
func FormatBYN(kop int64) string {
	return FormatKop(kop) + " руб."
}

// FormatKop: 123456 → "1 234,56".
func FormatKop(kop int64) string {
	neg := kop < 0
	if neg {
		kop = -kop
	}
	rub := kop / 100
	k := kop % 100
	s := strconv.FormatInt(rub, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteRune(' ')
		}
		b.WriteRune(c)
	}
	out := fmt.Sprintf("%s,%02d", b.String(), k)
	if neg {
		return "−" + out
	}
	return out
}

// ParseBYN принимает "15", "15.5", "15,50" и возвращает копейки.
func ParseBYN(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, errors.New("пустая сумма")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0, errors.New("некорректная сумма")
	}
	return int64(math.Round(f * 100)), nil
}

// QtyToFloat — количество в штуках.
func QtyToFloat(q int64) float64 { return float64(q) / QtyScale }

// QtyFromFloat — округление вниз до 1e-8.
func QtyFromFloat(f float64) int64 { return int64(math.Floor(f * QtyScale)) }

// ValueKop — стоимость qty при цене priceBYN за 1 единицу.
func ValueKop(qty int64, priceBYN float64) int64 {
	return int64(math.Round(QtyToFloat(qty) * priceBYN * 100))
}

// BP применяет базисные пункты: BP(1000, 100) = 10 (1%).
func BP(amount, bp int64) int64 {
	return int64(math.Round(float64(amount) * float64(bp) / 10000))
}
