package engine

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"autoinvest/internal/config"
	"autoinvest/internal/db"
	"autoinvest/internal/market"
	"autoinvest/internal/notify"
	"autoinvest/internal/payments"
	"autoinvest/internal/store"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, Minsk)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNextRun(t *testing.T) {
	cases := []struct {
		name     string
		p        store.Plan
		after    string
		afterRun bool
		want     string
	}{
		{"daily later today", store.Plan{Frequency: "daily", RunMinute: 600}, "2026-11-05 09:00", false, "2026-11-05 10:00"},
		{"daily tomorrow", store.Plan{Frequency: "daily", RunMinute: 600}, "2026-11-05 10:00", true, "2026-11-06 10:00"},
		{"weekly friday", store.Plan{Frequency: "weekly", Weekday: 5, RunMinute: 600}, "2026-11-05 12:00", false, "2026-11-06 10:00"}, // 05.11.2026 — четверг
		{"weekly same day passed", store.Plan{Frequency: "weekly", Weekday: 4, RunMinute: 600}, "2026-11-05 12:00", false, "2026-11-12 10:00"},
		{"weekly sunday", store.Plan{Frequency: "weekly", Weekday: 7, RunMinute: 600}, "2026-11-05 12:00", false, "2026-11-08 10:00"},
		{"biweekly after run", store.Plan{Frequency: "biweekly", Weekday: 5, RunMinute: 600}, "2026-11-06 10:00", true, "2026-11-20 10:00"},
		{"monthly 31 in february", store.Plan{Frequency: "monthly", Monthday: 31, RunMinute: 600}, "2027-02-01 00:00", false, "2027-02-28 10:00"},
		{"monthly next month", store.Plan{Frequency: "monthly", Monthday: 10, RunMinute: 720}, "2026-11-10 12:00", true, "2026-12-10 12:00"},
	}
	for _, c := range cases {
		got := NextRun(&c.p, at(c.after), c.afterRun)
		if got == nil {
			t.Fatalf("%s: nil", c.name)
		}
		if g := time.Unix(*got, 0).In(Minsk).Format("2006-01-02 15:04"); g != c.want {
			t.Errorf("%s: got %s want %s", c.name, g, c.want)
		}
	}
	if NextRun(&store.Plan{Frequency: "manual"}, time.Now(), false) != nil {
		t.Error("manual must not schedule")
	}
}

type env struct {
	en  *Engine
	st  *store.Store
	uid int64
}

func setup(t *testing.T) *env {
	t.Helper()
	d, err := db.OpenPath(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(d)
	hub := market.NewFixtureHub(100, log)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	cfg := &config.Config{FeeFreeBP: 100, FeeProBP: 50, SpreadBP: 100, MinContribKop: 500, MaxContribKop: 500000, ProMonthKop: 990, ProYearKop: 8900}
	sb := payments.NewSandbox("http://x")
	en := &Engine{St: st, DB: d, Hub: hub, Pay: sb, Broker: PaperBroker{Hub: hub}, N: notify.New(st, notify.NewMailer("", 0, "", "", "", log), "http://x", log), Cfg: cfg, Log: log}
	u, err := st.CreateUser(ctx, store.NewUser{Email: "a@b.by", Name: "Тест", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	return &env{en: en, st: st, uid: u.ID}
}

func (e *env) plan(t *testing.T, token string) *store.Plan {
	ctx := context.Background()
	c, err := e.st.AddCard(ctx, store.Card{UserID: e.uid, Provider: "sandbox", Token: token, Last4: "0000", Brand: "visa"})
	if err != nil {
		t.Fatal(err)
	}
	p := &store.Plan{UserID: e.uid, Name: "Тест", Icon: "piggy", Strategy: "balanced", AmountKop: 2000, Frequency: "weekly", Weekday: 5, RunMinute: 600,
		Status: "active", CardID: &c.ID, DipThresholdBP: 500, DipMultiplierPct: 100,
		Allocations: []store.Alloc{{Asset: "USDT", WeightBP: 5000}, {Asset: "BTC", WeightBP: 3000}, {Asset: "PAXG", WeightBP: 2000}}}
	if err := e.st.CreatePlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBuyIdempotentAndAccounting(t *testing.T) {
	e := setup(t)
	p := e.plan(t, "sbx_ok_1")
	ctx := context.Background()
	r1, err := e.en.Buy(ctx, p.ID, 10000, "manual", "k1")
	if err != nil || r1.Order.Status != "filled" {
		t.Fatalf("buy: %v %+v", err, r1.Order)
	}
	if r1.Order.FeeKop != 100 {
		t.Errorf("fee: %d", r1.Order.FeeKop)
	}
	var sum int64
	for _, f := range r1.Order.Fills {
		sum += f.AmountKop
	}
	if sum != 9900 {
		t.Errorf("net split %d, want 9900", sum)
	}
	// повтор того же ключа — та же заявка, без второго списания
	r2, _ := e.en.Buy(ctx, p.ID, 10000, "manual", "k1")
	if r2.Order.ID != r1.Order.ID {
		t.Fatal("idempotency broken")
	}
	var n int
	e.st.DB.QueryRow(`SELECT COUNT(*) FROM payments`).Scan(&n)
	if n != 1 {
		t.Errorf("payments=%d", n)
	}
	pl, _ := e.st.Plan(ctx, e.uid, p.ID)
	var cost int64
	for _, x := range pl.Positions {
		cost += x.CostKop
	}
	if cost != 9900 {
		t.Errorf("cost basis %d", cost)
	}
}

func TestConcurrentBuySameKey(t *testing.T) {
	e := setup(t)
	p := e.plan(t, "sbx_ok_2")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e.en.Buy(context.Background(), p.ID, 5000, "schedule", "same") }()
	}
	wg.Wait()
	var n int
	e.st.DB.QueryRow(`SELECT COUNT(*) FROM orders WHERE status='filled'`).Scan(&n)
	if n != 1 {
		t.Fatalf("filled orders=%d, want 1", n)
	}
}

func TestDeclineDoesNotBuy(t *testing.T) {
	e := setup(t)
	p := e.plan(t, "sbx_nofunds_1")
	r, err := e.en.Buy(context.Background(), p.ID, 5000, "manual", "d1")
	if err != nil || !r.Decline || r.Order.Status != "failed" {
		t.Fatalf("%v %+v", err, r)
	}
	pl, _ := e.st.Plan(context.Background(), e.uid, p.ID)
	if len(pl.Positions) != 0 {
		t.Fatal("positions after decline")
	}
}

func TestAmountLimits(t *testing.T) {
	e := setup(t)
	p := e.plan(t, "sbx_ok_3")
	if _, err := e.en.Buy(context.Background(), p.ID, 100, "manual", "s"); err == nil {
		t.Fatal("expected min amount error")
	}
}

func TestWithdrawPartialAndAll(t *testing.T) {
	e := setup(t)
	p := e.plan(t, "sbx_ok_4")
	ctx := context.Background()
	if _, err := e.en.Buy(ctx, p.ID, 20000, "manual", "b"); err != nil {
		t.Fatal(err)
	}
	pl, _ := e.st.Plan(ctx, e.uid, p.ID)
	before := e.en.Value(pl.Positions)
	o, err := e.en.Withdraw(ctx, e.uid, p.ID, 5000, "manual", "w1")
	if err != nil || o.Status != "filled" || o.AmountKop != 5000 {
		t.Fatalf("withdraw: %v %+v", err, o)
	}
	pl, _ = e.st.Plan(ctx, e.uid, p.ID)
	after := e.en.Value(pl.Positions)
	if after.ValueKop >= before.ValueKop || after.InvestedKop >= before.InvestedKop {
		t.Errorf("value not reduced: %d→%d", before.ValueKop, after.ValueKop)
	}
	if _, err := e.en.Withdraw(ctx, e.uid, p.ID, 10_000_000, "manual", "w2"); err == nil {
		t.Error("expected not enough")
	}
	if _, err := e.en.Withdraw(ctx, e.uid, p.ID, 0, "manual", "w3"); err != nil {
		t.Fatal(err)
	}
	pl, _ = e.st.Plan(ctx, e.uid, p.ID)
	for _, x := range pl.Positions {
		if x.Qty != 0 {
			t.Errorf("left %s %d", x.Asset, x.Qty)
		}
	}
}

func TestSchedulerRunsOnceAndAdvances(t *testing.T) {
	e := setup(t)
	p := e.plan(t, "sbx_ok_5")
	ctx := context.Background()
	past := time.Now().Add(-time.Hour).Unix()
	e.st.DB.Exec(`UPDATE plans SET next_run_at=? WHERE id=?`, past, p.ID)
	e.en.runDue(ctx)
	e.en.runDue(ctx)
	var n int
	e.st.DB.QueryRow(`SELECT COUNT(*) FROM orders WHERE trigger='schedule'`).Scan(&n)
	if n != 1 {
		t.Fatalf("scheduled orders=%d", n)
	}
	pl, _ := e.st.Plan(ctx, e.uid, p.ID)
	if pl.NextRunAt == nil || *pl.NextRunAt <= time.Now().Unix() {
		t.Fatal("next run not in future")
	}
}

func TestThreeDeclinesPause(t *testing.T) {
	e := setup(t)
	p := e.plan(t, "sbx_nofunds_2")
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		e.st.DB.Exec(`UPDATE plans SET next_run_at=? WHERE id=?`, time.Now().Add(-time.Duration(i+1)*time.Minute).Unix(), p.ID)
		e.en.runDue(ctx)
	}
	pl, _ := e.st.Plan(ctx, e.uid, p.ID)
	if pl.Status != "paused" {
		t.Fatalf("status %s fails %d", pl.Status, pl.FailCount)
	}
}

func TestBacktest(t *testing.T) {
	e := setup(t)
	for i := 0; i < 50 && !e.en.Hub.HistoryReady(); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	r, err := e.en.Backtest(context.Background(), BacktestRequest{Weights: map[string]int64{"BTC": 5000, "USDT": 5000}, AmountKop: 2000, Frequency: "weekly", Years: 1, DepositRate: 9})
	if err != nil {
		t.Fatal(err)
	}
	if r.Purchases < 50 || r.Purchases > 54 || r.InvestedKop != int64(r.Purchases)*2000 {
		t.Errorf("purchases %d invested %d", r.Purchases, r.InvestedKop)
	}
	if _, err := e.en.Backtest(context.Background(), BacktestRequest{Weights: map[string]int64{"BTC": 5000}, AmountKop: 2000}); err == nil {
		t.Error("weights != 100% accepted")
	}
}

func TestLateSuccessAfterFailGoesManual(t *testing.T) {
	e := setup(t)
	p := e.plan(t, "sbx_nofunds_9")
	ctx := context.Background()
	r, _ := e.en.Buy(ctx, p.ID, 5000, "manual", "late")
	if r.Order.Status != "failed" {
		t.Fatal(r.Order.Status)
	}
	var pid int64
	e.st.DB.QueryRow(`SELECT payment_id FROM orders WHERE id=?`, r.Order.ID).Scan(&pid)
	res, _ := e.en.settleBuy(ctx, r.Order.ID, pid, payments.ChargeResult{Status: "successful", UID: "late-uid"})
	if res.Order.Status != "manual" {
		t.Fatalf("late success must go to manual, got %s", res.Order.Status)
	}
	if err := e.en.FillManual(ctx, r.Order.ID, 1); err != nil {
		t.Fatal(err)
	}
	o, _ := e.st.Order(ctx, 0, r.Order.ID)
	if o.Status != "filled" || len(o.Fills) == 0 {
		t.Fatalf("manual fill: %+v", o)
	}
}

func TestReconcileNeverInventsFailure(t *testing.T) {
	e := setup(t)
	p := e.plan(t, "sbx_ok_7")
	ctx := context.Background()
	// заявка «повисла»: платёж в pending, провайдер о ней не знает (ChargeStatus → ошибка), прошло больше суток
	old := time.Now().Add(-48 * time.Hour).Unix()
	res, _ := e.st.DB.Exec(`INSERT INTO payments(user_id, card_id, provider, kind, tracking_id, amount_kop, status, test, created_at, updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		e.uid, *p.CardID, "sandbox", "charge", "ord_999", 5000, "pending", 1, old, old)
	pid, _ := res.LastInsertId()
	e.st.DB.Exec(`INSERT INTO orders(id, user_id, plan_id, side, trigger, status, amount_kop, fee_kop, idem_key, payment_id, created_at) VALUES(999,?,?,?,?,?,?,?,?,?,?)`,
		e.uid, p.ID, "buy", "schedule", "pending", 5000, 50, "hang", pid, old)
	e.en.reconcile(ctx)
	var st string
	e.st.DB.QueryRow(`SELECT status FROM orders WHERE id=999`).Scan(&st)
	if st != "manual" {
		t.Fatalf("unconfirmed order must go to manual review, got %s", st)
	}
}

func TestChargeProOncePerClick(t *testing.T) {
	e := setup(t)
	e.plan(t, "sbx_ok_8")
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); e.en.ChargePro(ctx, e.uid, "month", false) }()
	}
	wg.Wait()
	var n int
	e.st.DB.QueryRow(`SELECT COUNT(*) FROM payments WHERE kind='subscription' AND status='successful'`).Scan(&n)
	if n != 1 {
		t.Fatalf("subscription charged %d times", n)
	}
	u, _ := e.st.UserByID(ctx, e.uid)
	if !u.IsPro() || *u.ProUntil-time.Now().Unix() > 31*86400 {
		t.Fatalf("pro until %v", u.ProUntil)
	}
}

func TestClosePlanWithdrawsAndBlocksBuys(t *testing.T) {
	e := setup(t)
	p := e.plan(t, "sbx_ok_10")
	ctx := context.Background()
	e.en.Buy(ctx, p.ID, 5000, "manual", "c1")
	o, err := e.en.ClosePlan(ctx, e.uid, p.ID)
	if err != nil || o == nil || o.Status != "filled" {
		t.Fatalf("%v %+v", err, o)
	}
	if _, err := e.en.Buy(ctx, p.ID, 5000, "manual", "c2"); err != ErrPlanClosed {
		t.Fatalf("buy into closed plan: %v", err)
	}
}

func TestShowcaseSortedAndComparable(t *testing.T) {
	e := setup(t)
	for i := 0; i < 50 && !e.en.Hub.HistoryReady(); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	items, err := e.en.Showcase(context.Background(), 2000, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 3 {
		t.Fatalf("too few items: %d", len(items))
	}
	for i, it := range items {
		if it.InvestedKop <= 0 || it.ValueKop <= 0 || len(it.Points) == 0 || it.Name == "" {
			t.Errorf("bad item %+v", it.ID)
		}
		if i > 0 && it.ProfitPct > items[i-1].ProfitPct {
			t.Errorf("not sorted at %d", i)
		}
		if it.Symbol == "USDT" {
			t.Error("USDT duplicates strategy «Доллар»")
		}
	}
}
