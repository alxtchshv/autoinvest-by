package engine

import (
	"time"
	_ "time/tzdata" // часовой пояс Минска встроен в бинарник (на Windows системной базы зон нет)

	"autoinvest/internal/store"
)

var Minsk = mustLoc("Europe/Minsk")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("MSK", 3*3600)
	}
	return l
}

// NextRun — следующее время пополнения строго после after.
// afterRun=true означает, что after — момент только что прошедшего пополнения (важно для «раз в 2 недели»).
func NextRun(p *store.Plan, after time.Time, afterRun bool) *int64 {
	if p.Frequency == "manual" {
		return nil
	}
	a := after.In(Minsk)
	h, m := p.RunMinute/60, p.RunMinute%60
	at := func(d time.Time) time.Time { return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, Minsk) }
	var t time.Time
	switch p.Frequency {
	case "daily":
		t = at(a)
		if !t.After(a) {
			t = at(a.AddDate(0, 0, 1))
		}
	case "weekly", "biweekly":
		wd := p.Weekday % 7 // 1..7 → Go: 0=вс
		t = at(a)
		for i := 0; i < 8 && (int(t.Weekday()) != wd || !t.After(a)); i++ {
			t = at(t.AddDate(0, 0, 1))
		}
		if p.Frequency == "biweekly" && afterRun {
			t = t.AddDate(0, 0, 7)
		}
	case "monthly":
		t = monthAt(a.Year(), a.Month(), p.Monthday, h, m)
		if !t.After(a) {
			n := a.AddDate(0, 0, 1-a.Day()).AddDate(0, 1, 0)
			t = monthAt(n.Year(), n.Month(), p.Monthday, h, m)
		}
	default:
		return nil
	}
	u := t.Unix()
	return &u
}

func monthAt(y int, mo time.Month, day, h, m int) time.Time {
	last := time.Date(y, mo+1, 0, 0, 0, 0, 0, Minsk).Day()
	if day > last {
		day = last
	}
	if day < 1 {
		day = 1
	}
	return time.Date(y, mo, day, h, m, 0, 0, Minsk)
}

// PeriodsPerMonth — сколько пополнений в среднем за месяц.
func PeriodsPerMonth(freq string) float64 {
	switch freq {
	case "daily":
		return 30.4
	case "weekly":
		return 52.0 / 12
	case "biweekly":
		return 26.0 / 12
	case "monthly":
		return 1
	}
	return 0
}
