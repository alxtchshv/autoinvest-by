// Package payments — привязка карт и списания. Реквизиты карты никогда не попадают
// на наш сервер в боевом режиме: пользователь вводит их на странице bePaid, мы получаем токен.
package payments

import (
	"context"
	"errors"
	"net/http"
)

// Card — данные привязанной карты, которые разрешено хранить (PCI DSS): токен, маска, срок.
type Card struct {
	Token    string
	Brand    string
	Last4    string
	Holder   string
	Issuer   string
	BIN      string // первые 6 цифр номера (если провайдер их сообщает)
	ExpMonth int
	ExpYear  int
}

type BindRequest struct {
	Ref       string // наш идентификатор привязки
	Email     string
	ReturnURL string
	NotifyURL string
}

type BindResult struct {
	Status string // pending | done | failed
	Card   Card
	Error  string
}

type ChargeRequest struct {
	Token       string
	AmountKop   int64
	Description string
	TrackingID  string // уникален, повтор с тем же ID не спишет дважды
	Email       string
}

type ChargeResult struct {
	UID     string
	Status  string // successful | failed | pending
	Message string
}

// WebhookEvent — нормализованное уведомление провайдера.
type WebhookEvent struct {
	Kind       string // tokenization | payment
	TrackingID string
	UID        string
	Status     string
	Message    string
	AmountKop  int64
	Card       Card
}

var (
	ErrPayoutUnsupported = errors.New("выплата на карту через этого провайдера недоступна — оформляется вручную")
	ErrDeclined          = errors.New("операция отклонена банком")
)

type Provider interface {
	Name() string
	Test() bool
	StartBinding(ctx context.Context, r BindRequest) (redirectURL, providerRef string, err error)
	BindingStatus(ctx context.Context, providerRef string) (BindResult, error)
	ParseWebhook(r *http.Request, body []byte) (WebhookEvent, error)
	Charge(ctx context.Context, r ChargeRequest) (ChargeResult, error)
	ChargeStatus(ctx context.Context, trackingID string) (ChargeResult, error)
	Payout(ctx context.Context, r ChargeRequest) (ChargeResult, error)
	// Refund возвращает деньги по успешному списанию (uid транзакции провайдера).
	Refund(ctx context.Context, uid string, amountKop int64, reason string) (ChargeResult, error)
}
