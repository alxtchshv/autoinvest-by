package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"autoinvest/internal/config"
	"autoinvest/internal/engine"
	"autoinvest/internal/market"
	"autoinvest/internal/money"
	"autoinvest/internal/store"
)

// LegalVersion — редакция пользовательских документов (оферта, политика, уведомление о рисках).
// При существенном изменении текста повысьте версию: согласия хранятся с номером редакции.
const LegalVersion = "2026-11-06"

// ---------------------------------------------------------------- сессии и входы

// revokeAll завершает все сеансы, включая текущий («Выйти на всех устройствах»).
func (s *Server) revokeAll(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	s.St.DeleteOtherSessions(r.Context(), u.ID, "")
	s.St.Audit(r.Context(), u.ID, u.ID, "security.sessions_revoked_all", "", clientIP(r))
	s.clearSession(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// loginHistory — входы, неудачные попытки и выходы с адресом и устройством.
func (s *Server) loginHistory(w http.ResponseWriter, r *http.Request) {
	list, err := s.St.AuditFor(r.Context(), currentUser(r).ID, 100, "auth.")
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if list == nil {
		list = []store.AuditEntry{}
	}
	for i := range list {
		list[i].UserAgent = shortUA(list[i].UserAgent)
	}
	writeJSON(w, 200, map[string]any{"events": list})
}

func (s *Server) listConsents(w http.ResponseWriter, r *http.Request) {
	list, err := s.St.Consents(r.Context(), currentUser(r).ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"consents": list, "current_version": LegalVersion})
}

// exportMyData отдаёт все персональные данные пользователя одним файлом:
// право субъекта на получение информации об обработке (ст. 11 Закона № 99-З).
func (s *Server) exportMyData(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	ctx := r.Context()
	if !s.limiter.Allow(fmt.Sprintf("export:%d", u.ID), 5, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Выгрузку можно делать не чаще 5 раз в час")
		return
	}
	kyc, _ := s.St.KYC(ctx, u.ID)
	cards, _ := s.St.Cards(ctx, u.ID)
	plans, _ := s.St.Plans(ctx, u.ID, true)
	orders, _, _ := s.St.Orders(ctx, store.OrderFilter{UserID: u.ID, Limit: 500})
	consents, _ := s.St.Consents(ctx, u.ID)
	audit, _ := s.St.AuditFor(ctx, u.ID, 500)
	sessions, _ := s.St.ListSessions(ctx, u.ID)
	for i := range sessions {
		sessions[i].ID = ""
	}
	for _, o := range orders {
		o.UserEmail = ""
	}
	s.St.Audit(ctx, u.ID, u.ID, "privacy.data_export", "", clientIP(r))
	w.Header().Set("Content-Disposition", `attachment; filename="autoinvest-my-data.json"`)
	writeJSON(w, 200, map[string]any{
		"exported_at": time.Now().UTC().Format(time.RFC3339),
		"operator":    "AutoInvest",
		"note":        "Выгрузка персональных данных по запросу субъекта (Закон Республики Беларусь № 99-З). Токены карт и хеши паролей не включаются.",
		"profile":     u, "kyc": kyc, "cards": cards, "plans": plans, "operations": orders,
		"consents": consents, "audit_log": audit, "sessions": sessions,
	})
}

// ---------------------------------------------------------------- KYC

var regionLetters = "ABCKEMH" // Брестская, Витебская, Гомельская, Гродненская, Могилёвская, Минская обл., г. Минск

// checkPersonalNumber проверяет идентификационный номер гражданина РБ: формат,
// код пола и века в первой цифре, совпадение даты рождения и букву области.
// Пустая строка — номер корректен.
func checkPersonalNumber(pn string, birth time.Time) string {
	if !personalRe.MatchString(pn) {
		return "Идентификационный номер — 14 символов латиницей, например 3010195A001PB7"
	}
	century := map[byte]int{'1': 1800, '2': 1800, '3': 1900, '4': 1900, '5': 2000, '6': 2000}[pn[0]]
	if century == 0 {
		return "Первая цифра номера должна быть от 1 до 6 — проверьте номер в паспорте"
	}
	if !strings.ContainsRune(regionLetters, rune(pn[7])) {
		return "Восьмой символ номера — латинская буква области (A, B, C, K, E, M или H)"
	}
	day, month, year := pn[1:3], pn[3:5], pn[5:7]
	encoded, err := time.Parse("02012006", fmt.Sprintf("%s%s%d", day, month, century+atoi2(year)))
	if err != nil {
		return "Цифры 2–7 номера — дата рождения (ДДММГГ). Проверьте номер"
	}
	if !birth.IsZero() && !encoded.Equal(time.Date(birth.Year(), birth.Month(), birth.Day(), 0, 0, 0, 0, time.UTC)) {
		return "Дата рождения в номере (" + encoded.Format("02.01.2006") + ") не совпадает с указанной"
	}
	return ""
}

func atoi2(s string) int { return int(s[0]-'0')*10 + int(s[1]-'0') }

// ---------------------------------------------------------------- онбординг

// Recommendation — результат экспресс-анкеты.
type Recommendation struct {
	Strategy    string   `json:"strategy"`
	Profile     string   `json:"risk_profile"`
	AmountKop   int64    `json:"amount_kop"`
	Frequency   string   `json:"frequency"`
	Name        string   `json:"name"`
	Icon        string   `json:"icon"`
	Reasons     []string `json:"reasons"`
	MonthlyKop  int64    `json:"monthly_kop"`
	GoalComment string   `json:"goal_comment"`
}

// Recommend подбирает копилку по четырём ответам (каждый от 1 до 3):
// сумма в месяц, горизонт, реакция на просадку 15%, цель.
func Recommend(a [4]int) Recommendation {
	amount := map[int]int64{1: 1000, 2: 2500, 3: 5000}[a[0]] // в неделю: ≈43, ≈108, ≈217 BYN в месяц
	rec := Recommendation{AmountKop: amount, Frequency: "weekly", MonthlyKop: amount * 52 / 12}
	switch {
	case a[1] == 1:
		rec.Strategy = "reserve"
		rec.Reasons = append(rec.Reasons, "Деньги понадобятся меньше чем через полгода — за такой срок рискованные активы могут не успеть восстановиться после падения.")
	case a[2] == 1:
		rec.Strategy = "reserve"
		rec.Reasons = append(rec.Reasons, "Просадка на 15% для вас некомфортна — выбираем активы, которые почти не падают.")
	case a[3] == 1:
		rec.Strategy = "reserve"
		rec.Reasons = append(rec.Reasons, "Подушка безопасности должна быть доступна в любой момент и не зависеть от рынка.")
	// Крипто-рост — только тем, кто копит дольше трёх лет и на падении готов докупать, а не ждать.
	// Цель «крупная покупка» привязана к сроку, поэтому для неё потолок — сбалансированная копилка.
	case a[1] == 3 && a[2] == 3 && a[3] != 2:
		rec.Strategy = "crypto"
		rec.Reasons = append(rec.Reasons, "Длинный горизонт и готовность докупать на падениях позволяют взять крупные криптовалюты.",
			"Будьте готовы к просадкам на 50% и больше — это нормально для такой стратегии.")
	default:
		rec.Strategy = "balanced"
		rec.Reasons = append(rec.Reasons, "Основа в долларе и золоте сглаживает колебания, а доля биткоина даёт шанс на рост.")
	}
	switch rec.Strategy {
	case "reserve":
		rec.Profile, rec.Name, rec.Icon = "conservative", "Подушка безопасности", "shield"
	case "balanced":
		rec.Profile, rec.Name, rec.Icon = "moderate", "Сбалансированная", "piggy"
	default:
		rec.Profile, rec.Name, rec.Icon = "aggressive", "Крипто-рост", "rocket"
	}
	switch a[3] {
	case 2:
		rec.Name, rec.Icon = "На крупную покупку", "car"
		rec.GoalComment = "Укажите сумму и дату цели в настройках копилки — мы покажем, успеваете ли вы."
	case 3:
		rec.Name, rec.Icon = "Капитал на пенсию", "home"
	}
	return rec
}

func (s *Server) submitOnboarding(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in struct {
		Answers []int `json:"answers"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.Answers) != 4 {
		writeErr(w, http.StatusBadRequest, "Ответьте на все четыре вопроса")
		return
	}
	var a [4]int
	for i, v := range in.Answers {
		if v < 1 || v > 3 {
			writeErr(w, http.StatusBadRequest, "Некорректный ответ")
			return
		}
		a[i] = v
	}
	rec := Recommend(a)
	s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"risk_profile": rec.Profile, "onboarded_at": store.Now()})
	s.St.Audit(r.Context(), u.ID, u.ID, "profile.onboarding", rec.Strategy, clientIP(r))
	strat, _ := market.FindStrategy(rec.Strategy)
	writeJSON(w, 200, map[string]any{"recommendation": rec, "strategy": strat})
}

func (s *Server) skipOnboarding(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.OnboardedAt == nil {
		s.St.UpdateUserFields(r.Context(), u.ID, map[string]any{"onboarded_at": store.Now()})
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- выписка

// venueLabel — где исполнена сделка. На учебном счёте честно пишем, что партнёр её не исполнял.
func venueLabel(broker string) string {
	if broker == config.BrokerWhitebird {
		return "Исполнено через оператора криптоплатформы — резидента ПВТ"
	}
	return "Учебный счёт: учтено по рыночной цене, у партнёра не исполнялось"
}

// periodFromQuery читает ?from=YYYY-MM-DD&to=YYYY-MM-DD (даты по Минску, to включительно).
func periodFromQuery(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	var from, to int64
	if v := r.URL.Query().Get("from"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, engine.Minsk)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "Дата начала в формате ГГГГ-ММ-ДД")
			return 0, 0, false
		}
		from = t.Unix()
	}
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.ParseInLocation("2006-01-02", v, engine.Minsk)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "Дата окончания в формате ГГГГ-ММ-ДД")
			return 0, 0, false
		}
		to = t.AddDate(0, 0, 1).Unix()
	}
	if from > 0 && to > 0 && from >= to {
		writeErr(w, http.StatusBadRequest, "Дата начала позже даты окончания")
		return 0, 0, false
	}
	return from, to, true
}

// statement — данные для печатной выписки об операциях за период.
func (s *Server) statement(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	from, to, ok := periodFromQuery(w, r)
	if !ok {
		return
	}
	list, total, err := s.St.Orders(r.Context(), store.OrderFilter{UserID: u.ID, From: from, To: to, Limit: 500})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	var in, out, fees int64
	type row struct {
		*store.Order
		Venue string `json:"venue"`
	}
	rows := make([]row, 0, len(list))
	for _, o := range list {
		o.UserEmail = ""
		if o.Status == "filled" || o.Status == "payout_pending" {
			if o.Side == "buy" {
				in += o.AmountKop
				fees += o.FeeKop
			} else {
				out += o.AmountKop
			}
		}
		rows = append(rows, row{o, venueLabel(o.Broker)})
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "statement.export", "print", clientIP(r))
	writeJSON(w, 200, map[string]any{
		"holder": map[string]string{"name": u.Name, "email": u.Email},
		"from":   from, "to": to, "generated_at": store.Now(),
		"operations": rows, "total": total, "truncated": total > len(rows),
		"totals":      map[string]string{"deposited": money.FormatBYN(in), "withdrawn": money.FormatBYN(out), "fees": money.FormatBYN(fees)},
		"live_broker": s.Cfg.Broker == config.BrokerWhitebird,
		"test_money":  !s.Cfg.LiveMoney(),
	})
}

// sandboxRunSchedule — кнопка тестового стенда «выполнить плановое пополнение сейчас».
// Работает только с тестовой платёжкой и учебным исполнением: с настоящими деньгами её нет.
func (s *Server) sandboxRunSchedule(w http.ResponseWriter, r *http.Request) {
	if s.Sandbox == nil || s.Cfg.Broker != config.BrokerPaper {
		writeErr(w, http.StatusNotFound, "Доступно только на тестовом стенде")
		return
	}
	p := s.loadPlan(w, r)
	if p == nil {
		return
	}
	if !s.limiter.Allow(fmt.Sprintf("sbxrun:%d", p.UserID), 30, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много запусков за час")
		return
	}
	if err := s.En.RunScheduleNow(r.Context(), p.ID); err != nil {
		if errors.Is(err, engine.ErrNotScheduled) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		s.internal(w, r, err)
		return
	}
	s.getPlan(w, r)
}
