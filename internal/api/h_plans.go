package api

import (
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"autoinvest/internal/auth"
	"autoinvest/internal/engine"
	"autoinvest/internal/market"
	"autoinvest/internal/money"
	"autoinvest/internal/store"
)

type planView struct {
	*store.Plan
	Valuation      engine.Valuation `json:"valuation"`
	Card           *store.Card      `json:"card"`
	Risk           int              `json:"risk"`
	MonthlyKop     int64            `json:"monthly_kop"`
	GoalProgressBP int64            `json:"goal_progress_bp"`
	GoalETA        *string          `json:"goal_eta"`      // прогноз даты достижения цели при текущем темпе (без учёта роста)
	GoalNeedKop    *int64           `json:"goal_need_kop"` // какое пополнение нужно, чтобы успеть к дате цели
	NextRuns       []int64          `json:"next_runs"`
}

func planRisk(allocs []store.Alloc) int {
	var r float64
	for _, a := range allocs {
		if as, ok := market.Find(a.Asset); ok {
			r += float64(as.Risk) * float64(a.WeightBP) / 10000
		}
	}
	return int(math.Max(1, math.Round(r)))
}

func (s *Server) viewPlan(r *http.Request, p *store.Plan, cards map[int64]*store.Card) planView {
	v := planView{Plan: p, Valuation: s.En.Value(p.Positions), Risk: planRisk(p.Allocations), NextRuns: []int64{}}
	if p.CardID != nil {
		v.Card = cards[*p.CardID]
	}
	v.MonthlyKop = int64(math.Round(float64(p.AmountKop) * engine.PeriodsPerMonth(p.Frequency)))
	if p.Status == "active" && p.NextRunAt != nil {
		t := *p.NextRunAt
		v.NextRuns = append(v.NextRuns, t)
		for i := 0; i < 2; i++ {
			n := engine.NextRun(p, time.Unix(t, 0), true)
			if n == nil {
				break
			}
			t = *n
			v.NextRuns = append(v.NextRuns, t)
		}
	}
	if p.GoalKop != nil && *p.GoalKop > 0 {
		v.GoalProgressBP = min(v.Valuation.ValueKop*10000 / *p.GoalKop, 10000)
		left := *p.GoalKop - v.Valuation.ValueKop
		if left > 0 && v.MonthlyKop > 0 && p.Status == "active" {
			months := float64(left) / float64(v.MonthlyKop)
			eta := time.Now().AddDate(0, int(math.Ceil(months)), 0).Format("2006-01-02")
			v.GoalETA = &eta
		}
		if p.GoalDate != nil && left > 0 {
			if d, err := time.Parse("2006-01-02", *p.GoalDate); err == nil {
				months := d.Sub(time.Now()).Hours() / 24 / 30.4
				if months >= 0.5 && engine.PeriodsPerMonth(p.Frequency) > 0 {
					need := int64(math.Ceil(float64(left) / (months * engine.PeriodsPerMonth(p.Frequency))))
					v.GoalNeedKop = &need
				}
			}
		}
	}
	return v
}

func (s *Server) cardMap(r *http.Request, uid int64) map[int64]*store.Card {
	cards, _ := s.St.Cards(r.Context(), uid)
	m := map[int64]*store.Card{}
	for i := range cards {
		m[cards[i].ID] = &cards[i]
	}
	return m
}

func (s *Server) listPlans(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	plans, err := s.St.Plans(r.Context(), u.ID, r.URL.Query().Get("closed") == "1")
	if err != nil {
		s.internal(w, r, err)
		return
	}
	cm := s.cardMap(r, u.ID)
	out := make([]planView, 0, len(plans))
	for _, p := range plans {
		out = append(out, s.viewPlan(r, p, cm))
	}
	limit := s.Cfg.FreePlanLimit
	if u.IsPro() {
		limit = s.Cfg.ProPlanLimit
	}
	writeJSON(w, 200, map[string]any{"plans": out, "limit": limit})
}

type planInput struct {
	Name             *string       `json:"name"`
	Icon             *string       `json:"icon"`
	GoalKop          *int64        `json:"goal_kop"`
	GoalDate         *string       `json:"goal_date"`
	AmountKop        *int64        `json:"amount_kop"`
	Frequency        *string       `json:"frequency"`
	Weekday          *int          `json:"weekday"`
	Monthday         *int          `json:"monthday"`
	RunMinute        *int          `json:"run_minute"`
	Strategy         *string       `json:"strategy"`
	Allocations      []store.Alloc `json:"allocations"`
	CardID           *int64        `json:"card_id"`
	DipEnabled       *bool         `json:"dip_enabled"`
	DipThresholdBP   *int64        `json:"dip_threshold_bp"`
	DipMultiplierPct *int64        `json:"dip_multiplier_pct"`
	FirstTopup       bool          `json:"first_topup"`
	RiskAck          bool          `json:"risk_ack"`
	ClearGoal        bool          `json:"clear_goal"`
}

var icons = map[string]bool{"piggy": true, "shield": true, "plane": true, "home": true, "laptop": true, "car": true, "gift": true, "graduation": true, "heart": true, "rocket": true}

// applyPlanInput валидирует и применяет поля. Возвращает (поле, сообщение) при ошибке.
func (s *Server) applyPlanInput(r *http.Request, u *store.User, p *store.Plan, in planInput) (string, string) {
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if l := len([]rune(n)); l < 1 || l > 40 {
			return "name", "Название от 1 до 40 символов"
		}
		p.Name = n
	}
	if in.Icon != nil {
		if !icons[*in.Icon] {
			return "icon", "Неизвестная иконка"
		}
		p.Icon = *in.Icon
	}
	if in.ClearGoal {
		p.GoalKop, p.GoalDate = nil, nil
	}
	if in.GoalKop != nil {
		if *in.GoalKop <= 0 {
			p.GoalKop = nil
		} else if *in.GoalKop < 1000 || *in.GoalKop > 100_000_000 {
			return "goal_kop", "Цель от 10 до 1 000 000 BYN"
		} else {
			p.GoalKop = in.GoalKop
		}
	}
	if in.GoalDate != nil {
		if *in.GoalDate == "" {
			p.GoalDate = nil
		} else if d, err := time.Parse("2006-01-02", *in.GoalDate); err != nil || d.Before(time.Now()) || d.After(time.Now().AddDate(30, 0, 0)) {
			return "goal_date", "Дата цели должна быть в будущем"
		} else {
			p.GoalDate = in.GoalDate
		}
	}
	if in.AmountKop != nil {
		a := *in.AmountKop
		if a < s.Cfg.MinContribKop || a > s.Cfg.MaxContribKop {
			return "amount_kop", fmt.Sprintf("Сумма пополнения от %s до %s", money.FormatBYN(s.Cfg.MinContribKop), money.FormatBYN(s.Cfg.MaxContribKop))
		}
		p.AmountKop = a
	}
	if in.Frequency != nil {
		switch *in.Frequency {
		case "daily", "weekly", "biweekly", "monthly", "manual":
			p.Frequency = *in.Frequency
		default:
			return "frequency", "Выберите периодичность"
		}
	}
	if in.Weekday != nil {
		if *in.Weekday < 1 || *in.Weekday > 7 {
			return "weekday", "Выберите день недели"
		}
		p.Weekday = *in.Weekday
	}
	if in.Monthday != nil {
		if *in.Monthday < 1 || *in.Monthday > 31 {
			return "monthday", "Выберите число месяца"
		}
		p.Monthday = *in.Monthday
	}
	if in.RunMinute != nil {
		if *in.RunMinute < 0 || *in.RunMinute >= 1440 {
			return "run_minute", "Некорректное время"
		}
		p.RunMinute = *in.RunMinute / 15 * 15
	}
	if in.Strategy != nil {
		if *in.Strategy == "custom" {
			if !u.IsPro() {
				return "strategy", "Свой состав копилки доступен в Pro"
			}
			if len(in.Allocations) == 0 || len(in.Allocations) > len(market.Catalog) {
				return "allocations", "Выберите активы"
			}
			var sum int64
			seen := map[string]bool{}
			var allocs []store.Alloc
			for _, a := range in.Allocations {
				if _, ok := market.Find(a.Asset); !ok || seen[a.Asset] {
					return "allocations", "Неизвестный или повторяющийся актив"
				}
				if a.WeightBP < 500 || a.WeightBP > 10000 || a.WeightBP%100 != 0 {
					return "allocations", "Доля каждого актива — от 5% до 100%, шаг 1%"
				}
				seen[a.Asset] = true
				sum += a.WeightBP
				allocs = append(allocs, a)
			}
			if sum != 10000 {
				return "allocations", fmt.Sprintf("Сумма долей %d%% — должно быть ровно 100%%", sum/100)
			}
			sort.Slice(allocs, func(i, j int) bool { return allocs[i].WeightBP > allocs[j].WeightBP })
			p.Allocations = allocs
		} else {
			st, ok := market.FindStrategy(*in.Strategy)
			if !ok {
				return "strategy", "Выберите стратегию"
			}
			var allocs []store.Alloc
			for a, wbp := range st.Weights {
				allocs = append(allocs, store.Alloc{Asset: a, WeightBP: wbp})
			}
			sort.Slice(allocs, func(i, j int) bool { return allocs[i].WeightBP > allocs[j].WeightBP })
			p.Allocations = allocs
		}
		p.Strategy = *in.Strategy
		if u.RiskProfile != "" && planRisk(p.Allocations) > maxRisk(u.RiskProfile) && !in.RiskAck {
			return "risk_ack", "risk"
		}
	}
	if in.CardID != nil {
		if _, err := s.St.Card(r.Context(), u.ID, *in.CardID); err != nil {
			return "card_id", "Карта не найдена"
		}
		p.CardID = in.CardID
	}
	if in.DipEnabled != nil {
		if *in.DipEnabled && !u.IsPro() {
			return "dip_enabled", "Докупка на просадке доступна в Pro"
		}
		p.DipEnabled = *in.DipEnabled
	}
	if in.DipThresholdBP != nil {
		if *in.DipThresholdBP < 300 || *in.DipThresholdBP > 3000 {
			return "dip_threshold_bp", "Порог просадки от 3% до 30%"
		}
		p.DipThresholdBP = *in.DipThresholdBP
	}
	if in.DipMultiplierPct != nil {
		if *in.DipMultiplierPct < 50 || *in.DipMultiplierPct > 500 {
			return "dip_multiplier_pct", "Размер докупки от 50% до 500% обычного пополнения"
		}
		p.DipMultiplierPct = *in.DipMultiplierPct
	}
	return "", ""
}

func (s *Server) planFieldErr(w http.ResponseWriter, field, msg string) {
	if field == "risk_ack" {
		writeJSON(w, http.StatusConflict, apiErr{Code: "risk_ack_required", Field: field,
			Error: "Эта стратегия рискованнее, чем вам подходит по анкете. Подтвердите, что понимаете риск сильной просадки"})
		return
	}
	writeFieldErr(w, field, msg)
}

func (s *Server) createPlan(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var in planInput
	if !readJSON(w, r, &in) {
		return
	}
	n, _ := s.St.CountOpenPlans(r.Context(), u.ID)
	limit := s.Cfg.FreePlanLimit
	if u.IsPro() {
		limit = s.Cfg.ProPlanLimit
	}
	if n >= limit {
		msg := fmt.Sprintf("На тарифе Старт можно вести до %d копилок. Pro — до %d", s.Cfg.FreePlanLimit, s.Cfg.ProPlanLimit)
		if u.IsPro() {
			msg = fmt.Sprintf("Достигнут лимит в %d копилок", limit)
		}
		writeJSON(w, http.StatusConflict, apiErr{Error: msg, Code: "plan_limit"})
		return
	}
	if !s.limiter.Allow("plan:"+strconv.FormatInt(u.ID, 10), 20, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много новых копилок за час")
		return
	}
	p := &store.Plan{UserID: u.ID, Icon: "piggy", Frequency: "weekly", Weekday: 5, Monthday: 1, RunMinute: 600, Status: "active",
		DipThresholdBP: 500, DipMultiplierPct: 100}
	if in.Name == nil || in.AmountKop == nil || in.Strategy == nil || in.Frequency == nil {
		writeErr(w, http.StatusBadRequest, "Заполните название, сумму, расписание и стратегию")
		return
	}
	if f, msg := s.applyPlanInput(r, u, p, in); f != "" {
		s.planFieldErr(w, f, msg)
		return
	}
	if p.CardID == nil {
		if c, err := s.St.DefaultCard(r.Context(), u.ID); err == nil {
			p.CardID = &c.ID
		}
	}
	if p.CardID == nil {
		p.Status = "paused" // без карты пополнять нечем — включится после привязки
	} else {
		p.NextRunAt = engine.NextRun(p, time.Now(), false)
	}
	if err := s.St.CreatePlan(r.Context(), p); err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "plan.created", p.Name, clientIP(r))
	var topupErr string
	if in.FirstTopup && p.CardID != nil {
		res, err := s.En.Buy(r.Context(), p.ID, p.AmountKop, "manual", fmt.Sprintf("first_%d", p.ID))
		if err != nil {
			topupErr = err.Error()
		} else if res.Order != nil && res.Order.Status == "failed" {
			topupErr = res.Order.Error
		}
	}
	p, _ = s.St.Plan(r.Context(), u.ID, p.ID)
	writeJSON(w, http.StatusCreated, map[string]any{"plan": s.viewPlan(r, p, s.cardMap(r, u.ID)), "topup_error": topupErr})
}

func (s *Server) loadPlan(w http.ResponseWriter, r *http.Request) *store.Plan {
	p, err := s.St.Plan(r.Context(), currentUser(r).ID, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return nil
	}
	return p
}

func (s *Server) getPlan(w http.ResponseWriter, r *http.Request) {
	p := s.loadPlan(w, r)
	if p == nil {
		return
	}
	orders, _, _ := s.St.Orders(r.Context(), store.OrderFilter{UserID: p.UserID, PlanID: p.ID, Limit: 20})
	if orders == nil {
		orders = []*store.Order{}
	}
	for _, o := range orders {
		o.UserEmail = ""
	}
	writeJSON(w, 200, map[string]any{"plan": s.viewPlan(r, p, s.cardMap(r, p.UserID)), "operations": orders})
}

func (s *Server) updatePlan(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	p := s.loadPlan(w, r)
	if p == nil {
		return
	}
	if p.Status == "closed" {
		writeErr(w, http.StatusConflict, "Копилка закрыта")
		return
	}
	var in planInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.Strategy == nil {
		p.Allocations = nil // состав не меняется
	}
	if f, msg := s.applyPlanInput(r, u, p, in); f != "" {
		s.planFieldErr(w, f, msg)
		return
	}
	if p.Status == "active" && (in.Frequency != nil || in.Weekday != nil || in.Monthday != nil || in.RunMinute != nil) {
		p.NextRunAt = engine.NextRun(p, time.Now(), false)
	}
	if err := s.St.UpdatePlan(r.Context(), p); err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "plan.updated", p.Name, clientIP(r))
	s.getPlan(w, r)
}

func (s *Server) pausePlan(w http.ResponseWriter, r *http.Request) {
	p := s.loadPlan(w, r)
	if p == nil {
		return
	}
	if p.Status == "active" {
		p.Status, p.NextRunAt, p.Allocations = "paused", nil, nil
		s.St.UpdatePlan(r.Context(), p)
		s.St.Audit(r.Context(), p.UserID, p.UserID, "plan.paused", p.Name, clientIP(r))
	}
	s.getPlan(w, r)
}

func (s *Server) resumePlan(w http.ResponseWriter, r *http.Request) {
	p := s.loadPlan(w, r)
	if p == nil {
		return
	}
	if p.Status == "closed" {
		writeErr(w, http.StatusConflict, "Копилка закрыта")
		return
	}
	if p.CardID == nil {
		if c, err := s.St.DefaultCard(r.Context(), p.UserID); err == nil {
			p.CardID = &c.ID
		} else {
			writeJSON(w, http.StatusConflict, apiErr{Error: "Привяжите карту, чтобы включить копилку", Code: "no_card"})
			return
		}
	}
	p.Status, p.FailCount, p.Allocations = "active", 0, nil
	p.NextRunAt = engine.NextRun(p, time.Now(), false)
	s.St.UpdatePlan(r.Context(), p)
	s.St.Audit(r.Context(), p.UserID, p.UserID, "plan.resumed", p.Name, clientIP(r))
	s.getPlan(w, r)
}

func idemOrNew(key, prefix string, uid int64) string {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 64 {
		return fmt.Sprintf("%s_%d_%d", prefix, uid, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s_%d_%s", prefix, uid, key)
}

func (s *Server) topupPlan(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	p := s.loadPlan(w, r)
	if p == nil {
		return
	}
	var in struct {
		AmountKop int64  `json:"amount_kop"`
		Key       string `json:"idempotency_key"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !s.limiter.Allow("topup:"+strconv.FormatInt(u.ID, 10), 20, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много пополнений за час")
		return
	}
	res, err := s.En.Buy(r.Context(), p.ID, in.AmountKop, "manual", idemOrNew(in.Key, "man", u.ID))
	if err != nil {
		s.engineErr(w, r, err)
		return
	}
	if res.Order != nil {
		res.Order.UserEmail = ""
	}
	writeJSON(w, 200, map[string]any{"order": res.Order, "pending": res.Retry})
}

func (s *Server) engineErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, engine.ErrNoCard):
		writeJSON(w, http.StatusConflict, apiErr{Error: err.Error(), Code: "no_card"})
	case errors.Is(err, engine.ErrKYCRequired):
		writeJSON(w, http.StatusConflict, apiErr{Error: err.Error(), Code: "kyc_required"})
	case errors.Is(err, engine.ErrAmountTooSmall), errors.Is(err, engine.ErrAmountTooLarge), errors.Is(err, engine.ErrNotEnough):
		writeFieldErr(w, "amount_kop", err.Error())
	case errors.Is(err, engine.ErrMarket), errors.Is(err, engine.ErrBusy):
		writeErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, engine.ErrPlanClosed):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "Не найдено")
	default:
		if msg := err.Error(); msg != "" && !strings.Contains(msg, "sql") && []rune(msg)[0] >= 'А' {
			writeErr(w, http.StatusConflict, msg)
			return
		}
		s.internal(w, r, err)
	}
}

func (s *Server) withdrawPlan(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	p := s.loadPlan(w, r)
	if p == nil {
		return
	}
	var in struct {
		AmountKop int64  `json:"amount_kop"`
		All       bool   `json:"all"`
		Key       string `json:"idempotency_key"`
		Password  string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !s.confirmPassword(w, r, in.Password) {
		return
	}
	if !in.All && in.AmountKop <= 0 {
		writeFieldErr(w, "amount_kop", "Укажите сумму")
		return
	}
	amount := in.AmountKop
	if in.All {
		amount = 0
	}
	o, err := s.En.Withdraw(r.Context(), u.ID, p.ID, amount, "manual", idemOrNew(in.Key, "wd", u.ID))
	if err != nil {
		s.engineErr(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "plan.withdraw", fmt.Sprintf("%s: %s", p.Name, money.FormatBYN(o.AmountKop)), clientIP(r))
	o.UserEmail = ""
	writeJSON(w, 200, map[string]any{"order": o})
}

// confirmPassword — повторная проверка пароля перед выводом денег (защита при украденной сессии).
func (s *Server) confirmPassword(w http.ResponseWriter, r *http.Request, password string) bool {
	u := currentUser(r)
	if !s.limiter.Allow("confirm:"+strconv.FormatInt(u.ID, 10), 10, 15*time.Minute) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много попыток. Подождите 15 минут")
		return false
	}
	if !auth.CheckPassword(u.PasswordHash, password) {
		writeFieldErr(w, "password", "Неверный пароль")
		return false
	}
	return true
}

func (s *Server) closePlan(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	p := s.loadPlan(w, r)
	if p == nil {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	readJSON(w, r, &in)
	if p.Status != "closed" && s.En.Value(p.Positions).ValueKop > 0 && !s.confirmPassword(w, r, in.Password) {
		return
	}
	order, err := s.En.ClosePlan(r.Context(), u.ID, p.ID)
	if err != nil {
		s.engineErr(w, r, err)
		return
	}
	if order != nil {
		order.UserEmail = ""
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "plan.closed", p.Name, clientIP(r))
	p, _ = s.St.Plan(r.Context(), u.ID, p.ID)
	writeJSON(w, 200, map[string]any{"order": order, "plan": s.viewPlan(r, p, s.cardMap(r, u.ID))})
}

// csvSafe защищает от формул в Excel (=, +, -, @ в начале ячейки).
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func periodFrom(period string) int64 {
	now := time.Now()
	switch period {
	case "1d":
		return now.Add(-24 * time.Hour).Unix()
	case "7d":
		return now.AddDate(0, 0, -7).Unix()
	case "1m":
		return now.AddDate(0, -1, 0).Unix()
	case "3m":
		return now.AddDate(0, -3, 0).Unix()
	case "1y":
		return now.AddDate(-1, 0, 0).Unix()
	}
	return 0
}

// thin — прореживаем точки для длинных периодов (не больше ~400).
func thin(in []store.Snapshot) []store.Snapshot {
	if len(in) <= 400 {
		return in
	}
	step := len(in) / 400
	out := make([]store.Snapshot, 0, 410)
	for i := 0; i < len(in); i += step {
		out = append(out, in[i])
	}
	if out[len(out)-1].TS != in[len(in)-1].TS {
		out = append(out, in[len(in)-1])
	}
	return out
}

func (s *Server) planHistory(w http.ResponseWriter, r *http.Request) {
	p := s.loadPlan(w, r)
	if p == nil {
		return
	}
	snaps, err := s.St.Snapshots(r.Context(), p.UserID, p.ID, periodFrom(r.URL.Query().Get("period")))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"points": thin(s.liveTail(snaps, s.En.Value(p.Positions)))})
}

// liveTail дополняет историю текущей оценкой, чтобы график заканчивался «сейчас».
func (s *Server) liveTail(snaps []store.Snapshot, v engine.Valuation) []store.Snapshot {
	if v.Stale && v.ValueKop == 0 {
		return snaps
	}
	if len(snaps) == 0 && v.InvestedKop == 0 {
		return snaps
	}
	return append(snaps, store.Snapshot{TS: time.Now().Unix(), ValueKop: v.ValueKop, InvestedKop: v.InvestedKop})
}

func (s *Server) portfolio(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	plans, err := s.St.Plans(r.Context(), u.ID, false)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	cm := s.cardMap(r, u.ID)
	var all []store.Position
	views := make([]planView, 0, len(plans))
	type upcoming struct {
		PlanID    int64  `json:"plan_id"`
		PlanName  string `json:"plan_name"`
		At        int64  `json:"at"`
		AmountKop int64  `json:"amount_kop"`
		CardLast4 string `json:"card_last4"`
	}
	var next []upcoming
	var monthly int64
	for _, p := range plans {
		v := s.viewPlan(r, p, cm)
		views = append(views, v)
		all = append(all, p.Positions...)
		if p.Status == "active" {
			monthly += v.MonthlyKop
			if p.NextRunAt != nil {
				l4 := ""
				if v.Card != nil {
					l4 = v.Card.Last4
				}
				next = append(next, upcoming{p.ID, p.Name, *p.NextRunAt, p.AmountKop, l4})
			}
		}
	}
	sort.Slice(next, func(i, j int) bool { return next[i].At < next[j].At })
	if len(next) > 5 {
		next = next[:5]
	}
	if next == nil {
		next = []upcoming{}
	}
	// объединяем позиции по активу
	agg := map[string]store.Position{}
	for _, x := range all {
		a := agg[x.Asset]
		a.Asset = x.Asset
		a.Qty += x.Qty
		a.CostKop += x.CostKop
		agg[x.Asset] = a
	}
	var merged []store.Position
	for _, x := range agg {
		merged = append(merged, x)
	}
	val := s.En.Value(merged)
	sort.Slice(val.Positions, func(i, j int) bool { return val.Positions[i].ValueKop > val.Positions[j].ValueKop })
	var month int64
	s.DB.QueryRowContext(r.Context(), `SELECT COALESCE(SUM(amount_kop),0) FROM orders WHERE user_id=? AND side='buy' AND status='filled' AND created_at>=?`,
		u.ID, time.Now().In(engine.Minsk).AddDate(0, 0, 1-time.Now().In(engine.Minsk).Day()).Truncate(24*time.Hour).Unix()).Scan(&month)
	writeJSON(w, 200, map[string]any{"valuation": val, "plans": views, "upcoming": next, "monthly_plan_kop": monthly, "month_invested_kop": month,
		"rates": s.Hub.Rates()})
}

func (s *Server) portfolioHistory(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	snaps, err := s.St.Snapshots(r.Context(), u.ID, 0, periodFrom(r.URL.Query().Get("period")))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	plans, _ := s.St.Plans(r.Context(), u.ID, false)
	var all []store.Position
	for _, p := range plans {
		all = append(all, p.Positions...)
	}
	writeJSON(w, 200, map[string]any{"points": thin(s.liveTail(snaps, s.En.Value(all)))})
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	per := qInt(r, "per", 30)
	page := max(qInt(r, "page", 1), 1)
	planID, _ := strconv.ParseInt(r.URL.Query().Get("plan"), 10, 64)
	f := store.OrderFilter{UserID: u.ID, PlanID: planID, Side: r.URL.Query().Get("side"), Status: r.URL.Query().Get("status"), Limit: per, Offset: (page - 1) * per}
	list, total, err := s.St.Orders(r.Context(), f)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if list == nil {
		list = []*store.Order{}
	}
	for _, o := range list {
		o.UserEmail = ""
	}
	writeJSON(w, 200, map[string]any{"operations": list, "total": total, "page": page, "per": per})
}

func (s *Server) getOperation(w http.ResponseWriter, r *http.Request) {
	o, err := s.St.Order(r.Context(), currentUser(r).ID, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	o.UserEmail = ""
	writeJSON(w, 200, map[string]any{"operation": o})
}

func (s *Server) exportOperations(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	from, to, ok := periodFromQuery(w, r)
	if !ok {
		return
	}
	list, _, err := s.St.Orders(r.Context(), store.OrderFilter{UserID: u.ID, From: from, To: to, Limit: 500})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "statement.export", "csv", clientIP(r))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="autoinvest-operations.csv"`)
	w.Write([]byte("\xEF\xBB\xBF")) // BOM, чтобы Excel открыл кириллицу
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	cw.Write([]string{"№", "Дата", "Копилка", "Операция", "Статус", "Сумма, BYN", "Комиссия, BYN", "Активы", "Курс USD НБРБ", "Карта", "Исполнение"})
	side := map[string]string{"buy": "Пополнение", "sell": "Вывод"}
	status := map[string]string{"filled": "Исполнено", "failed": "Отклонено", "pending": "В обработке", "payout_pending": "Ожидает выплаты", "manual": "На проверке"}
	for _, o := range list {
		var parts []string
		for _, f := range o.Fills {
			parts = append(parts, fmt.Sprintf("%s %.8f по %.2f BYN", f.Asset, money.QtyToFloat(f.Qty), f.PriceBYN))
		}
		cw.Write([]string{strconv.FormatInt(o.ID, 10), time.Unix(o.CreatedAt, 0).In(engine.Minsk).Format("02.01.2006 15:04"), csvSafe(o.PlanName), side[o.Side], status[o.Status],
			strings.ReplaceAll(money.FormatKop(o.AmountKop), " ", ""), strings.ReplaceAll(money.FormatKop(o.FeeKop), " ", ""), strings.Join(parts, ", "),
			strconv.FormatFloat(o.USDBYN, 'f', 4, 64), o.CardLast4, venueLabel(o.Broker)})
	}
	cw.Flush()
}
