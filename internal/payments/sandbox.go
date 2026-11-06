package payments

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"autoinvest/internal/auth"
)

// TestCard — тестовая карта песочницы (номера совпадают с тестовыми картами bePaid,
// чтобы переход на bePaid test=true не требовал переучивания).
type TestCard struct {
	Number   string `json:"number"`
	Brand    string `json:"brand"`
	Behavior string `json:"behavior"` // ok | nofunds | decline
	Label    string `json:"label"`
}

var TestCards = []TestCard{
	{"4200000000000000", "visa", "ok", "Успешные операции"},
	{"5555555555554444", "mastercard", "ok", "Успешные операции"},
	{"9112000000000006", "belkart", "ok", "Белкарт, успешные операции"},
	{"4005550000000019", "visa", "nofunds", "Привязывается, но списания отклоняются: недостаточно средств"},
	{"4000000000000002", "visa", "decline", "Банк отклоняет привязку"},
}

// Sandbox — встроенная тестовая платёжка. Реальные карты не принимаются.
type Sandbox struct {
	PublicURL string
	mu        sync.Mutex
	charges   map[string]ChargeResult
}

func NewSandbox(publicURL string) *Sandbox {
	return &Sandbox{PublicURL: publicURL, charges: map[string]ChargeResult{}}
}

func (s *Sandbox) Name() string { return "sandbox" }
func (s *Sandbox) Test() bool   { return true }

func (s *Sandbox) StartBinding(ctx context.Context, r BindRequest) (string, string, error) {
	return s.PublicURL + "/pay/sandbox/" + r.Ref, r.Ref, nil
}

func (s *Sandbox) BindingStatus(ctx context.Context, ref string) (BindResult, error) {
	return BindResult{Status: "pending"}, nil
}

// Tokenize проверяет тестовую карту и выдаёт токен.
func (s *Sandbox) Tokenize(number, holder string, expMonth, expYear int) (Card, error) {
	number = strings.ReplaceAll(strings.ReplaceAll(number, " ", ""), "-", "")
	var tc *TestCard
	for i := range TestCards {
		if TestCards[i].Number == number {
			tc = &TestCards[i]
		}
	}
	if tc == nil {
		return Card{}, errors.New("В тестовом режиме принимаются только тестовые карты из списка. Не вводите данные настоящей карты")
	}
	now := time.Now()
	if expYear < now.Year() || (expYear == now.Year() && expMonth < int(now.Month())) || expMonth < 1 || expMonth > 12 {
		return Card{}, errors.New("Срок действия карты истёк или указан неверно")
	}
	if tc.Behavior == "decline" {
		return Card{}, ErrDeclined
	}
	return Card{
		Token: "sbx_" + tc.Behavior + "_" + strings.ToLower(auth.RandomCode(20, "")),
		Brand: tc.Brand, Last4: number[len(number)-4:], Holder: strings.ToUpper(strings.TrimSpace(holder)),
		Issuer: "Тестовый банк", BIN: number[:6], ExpMonth: expMonth, ExpYear: expYear,
	}, nil
}

func (s *Sandbox) ParseWebhook(r *http.Request, body []byte) (WebhookEvent, error) {
	return WebhookEvent{}, errors.New("sandbox: вебхуки не используются")
}

func (s *Sandbox) Charge(ctx context.Context, r ChargeRequest) (ChargeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.charges[r.TrackingID]; ok {
		return prev, nil // идемпотентность как у настоящего шлюза
	}
	res := ChargeResult{UID: "sbx-" + strings.ToLower(auth.RandomCode(12, "")), Status: "successful", Message: "Тестовое списание проведено"}
	switch {
	case r.AmountKop <= 0:
		res.Status, res.Message = "failed", "Некорректная сумма"
	case strings.HasPrefix(r.Token, "sbx_nofunds_"):
		res.Status, res.Message = "failed", "Недостаточно средств на карте"
	case !strings.HasPrefix(r.Token, "sbx_"):
		res.Status, res.Message = "failed", "Неизвестный токен карты"
	}
	s.charges[r.TrackingID] = res
	return res, nil
}

func (s *Sandbox) ChargeStatus(ctx context.Context, trackingID string) (ChargeResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.charges[trackingID]; ok {
		return r, nil
	}
	return ChargeResult{}, errors.New("не найдено")
}

func (s *Sandbox) Payout(ctx context.Context, r ChargeRequest) (ChargeResult, error) {
	if !strings.HasPrefix(r.Token, "sbx_") {
		return ChargeResult{Status: "failed", Message: "Неизвестный токен карты"}, nil
	}
	return ChargeResult{UID: "sbx-po-" + strings.ToLower(auth.RandomCode(10, "")), Status: "successful", Message: "Тестовая выплата проведена"}, nil
}

func (s *Sandbox) Refund(ctx context.Context, uid string, amountKop int64, reason string) (ChargeResult, error) {
	return ChargeResult{UID: "sbx-rf-" + strings.ToLower(auth.RandomCode(10, "")), Status: "successful", Message: "Тестовый возврат проведён"}, nil
}
