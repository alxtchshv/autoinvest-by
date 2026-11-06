package payments

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BePaid — интеграция с белорусским эквайрингом bePaid.
//  1. Привязка: hosted checkout (transaction_type=tokenization, contract=recurring).
//     Пользователь вводит карту и проходит 3-D Secure на странице bePaid.
//  2. Списания: POST /transactions/payments с credit_card.token (MIT, card_on_file).
//  3. Вебхуки: проверка Basic-авторизации shop_id:secret и (если задан ключ) RSA Content-Signature.
type BePaid struct {
	ShopID       string
	Secret       string
	IsTest       bool
	PublicKeyPEM string
	GatewayURL   string
	CheckoutURL  string
	HTTP         *http.Client
}

func NewBePaid(shopID, secret string, test bool, publicKey string) *BePaid {
	return &BePaid{ShopID: shopID, Secret: secret, IsTest: test, PublicKeyPEM: publicKey,
		GatewayURL: "https://gateway.bepaid.by", CheckoutURL: "https://checkout.bepaid.by",
		HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (b *BePaid) Name() string { return "bepaid" }
func (b *BePaid) Test() bool   { return b.IsTest }

func (b *BePaid) authHeader() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(b.ShopID+":"+b.Secret))
}

type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string { return fmt.Sprintf("bePaid HTTP %d: %s", e.Status, e.Message) }

func (b *BePaid) do(ctx context.Context, method, url, version string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", b.authHeader())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if version != "" {
		req.Header.Set("X-API-Version", version)
	}
	resp, err := b.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode >= 400 {
		var e struct {
			Message string `json:"message"`
		}
		json.Unmarshal(data, &e)
		if e.Message == "" {
			e.Message = strings.TrimSpace(string(data))
		}
		return &apiError{resp.StatusCode, e.Message}
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (b *BePaid) StartBinding(ctx context.Context, r BindRequest) (string, string, error) {
	body := map[string]any{"checkout": map[string]any{
		"test":             b.IsTest,
		"transaction_type": "tokenization",
		"attempts":         3,
		"settings": map[string]any{
			"return_url":       r.ReturnURL,
			"notification_url": r.NotifyURL,
			"language":         "ru",
			"customer_fields":  map[string]any{"visible": []string{}},
		},
		"payment_method": map[string]any{"types": []string{"credit_card"}},
		"order": map[string]any{
			"currency":        "BYN",
			"amount":          100,
			"description":     "Привязка карты для автопополнения копилок AutoInvest",
			"tracking_id":     "bind_" + r.Ref,
			"additional_data": map[string]any{"contract": []string{"recurring", "card_on_file"}},
		},
		"customer": map[string]any{"email": r.Email},
	}}
	var resp struct {
		Checkout struct {
			Token       string `json:"token"`
			RedirectURL string `json:"redirect_url"`
		} `json:"checkout"`
	}
	if err := b.do(ctx, http.MethodPost, b.CheckoutURL+"/ctp/api/checkouts", "2", body, &resp); err != nil {
		return "", "", err
	}
	if resp.Checkout.Token == "" || resp.Checkout.RedirectURL == "" {
		return "", "", errors.New("bePaid не вернул ссылку на платёжную страницу")
	}
	return resp.Checkout.RedirectURL, resp.Checkout.Token, nil
}

// BindingStatus опрашивает статус checkout — на случай, если вебхук не дошёл (например, локальный запуск).
func (b *BePaid) BindingStatus(ctx context.Context, token string) (BindResult, error) {
	var raw map[string]any
	if err := b.do(ctx, http.MethodGet, b.CheckoutURL+"/ctp/api/checkouts/"+token, "2", nil, &raw); err != nil {
		return BindResult{}, err
	}
	co, _ := raw["checkout"].(map[string]any)
	status, _ := co["status"].(string)
	card, ok := findCard(co)
	switch {
	case ok && card.Token != "" && (status == "successful" || status == ""):
		return BindResult{Status: "done", Card: card}, nil
	case status == "failed" || status == "expired" || co["expired"] == true:
		msg, _ := co["message"].(string)
		if msg == "" {
			msg = "Карта не привязана"
		}
		return BindResult{Status: "failed", Error: msg}, nil
	}
	return BindResult{Status: "pending"}, nil
}

// findCard ищет в ответе объект credit_card с токеном (структура checkout различается по версиям API).
func findCard(v any) (Card, bool) {
	switch x := v.(type) {
	case map[string]any:
		if cc, ok := x["credit_card"].(map[string]any); ok {
			if tok, _ := cc["token"].(string); tok != "" {
				return cardFrom(cc), true
			}
		}
		for _, child := range x {
			if c, ok := findCard(child); ok {
				return c, true
			}
		}
	case []any:
		for _, child := range x {
			if c, ok := findCard(child); ok {
				return c, true
			}
		}
	}
	return Card{}, false
}

func cardFrom(cc map[string]any) Card {
	s := func(k string) string { v, _ := cc[k].(string); return v }
	n := func(k string) int {
		switch v := cc[k].(type) {
		case float64:
			return int(v)
		case string:
			var i int
			fmt.Sscan(v, &i)
			return i
		}
		return 0
	}
	return Card{Token: s("token"), Brand: s("brand"), Last4: s("last_4"), Holder: s("holder"),
		Issuer: s("issuer_name"), BIN: binFrom(cc), ExpMonth: n("exp_month"), ExpYear: n("exp_year")}
}

// binFrom достаёт первые цифры номера, если bePaid их прислал (поле bin или first_1).
func binFrom(cc map[string]any) string {
	for _, k := range []string{"bin", "first_1"} {
		switch v := cc[k].(type) {
		case string:
			return v
		case float64:
			return fmt.Sprintf("%.0f", v)
		}
	}
	return ""
}

// ParseWebhook проверяет подлинность уведомления и нормализует его.
func (b *BePaid) ParseWebhook(r *http.Request, body []byte) (WebhookEvent, error) {
	authOK := subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(b.authHeader())) == 1
	if b.PublicKeyPEM != "" {
		if err := verifySignature(b.PublicKeyPEM, r.Header.Get("Content-Signature"), body); err != nil {
			return WebhookEvent{}, err
		}
	} else if !authOK {
		return WebhookEvent{}, errors.New("bePaid webhook: неверная авторизация")
	}
	var n struct {
		Transaction struct {
			UID        string         `json:"uid"`
			Type       string         `json:"type"`
			Status     string         `json:"status"`
			Message    string         `json:"message"`
			TrackingID string         `json:"tracking_id"`
			Amount     int64          `json:"amount"`
			CreditCard map[string]any `json:"credit_card"`
		} `json:"transaction"`
	}
	if err := json.Unmarshal(body, &n); err != nil {
		return WebhookEvent{}, err
	}
	t := n.Transaction
	ev := WebhookEvent{Kind: t.Type, TrackingID: t.TrackingID, UID: t.UID, Status: t.Status, Message: t.Message, AmountKop: t.Amount}
	if t.CreditCard != nil {
		ev.Card = cardFrom(t.CreditCard)
	}
	return ev, nil
}

func verifySignature(pemKey, sigB64 string, body []byte) error {
	blk, _ := pem.Decode([]byte(pemKey))
	if blk == nil {
		// ключ в кабинете bePaid часто выдают без PEM-обёртки
		der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pemKey))
		if err != nil {
			return errors.New("bePaid: некорректный публичный ключ")
		}
		blk = &pem.Block{Bytes: der}
	}
	pub, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		return err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return errors.New("bePaid: ключ не RSA")
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return errors.New("bePaid: нет подписи")
	}
	h := sha256.Sum256(body)
	return rsa.VerifyPKCS1v15(rsaPub, crypto.SHA256, h[:], sig)
}

type txResp struct {
	Transaction struct {
		UID     string `json:"uid"`
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"transaction"`
}

func normStatus(s string) string {
	switch s {
	case "successful":
		return "successful"
	case "failed", "expired", "error":
		return "failed"
	}
	return "pending" // incomplete / pending
}

// Charge — списание по токену без участия клиента (merchant-initiated, recurring).
func (b *BePaid) Charge(ctx context.Context, r ChargeRequest) (ChargeResult, error) {
	body := map[string]any{"request": map[string]any{
		"amount":          r.AmountKop,
		"currency":        "BYN",
		"description":     r.Description,
		"tracking_id":     r.TrackingID,
		"test":            b.IsTest,
		"duplicate_check": true,
		"credit_card":     map[string]any{"token": r.Token},
		"customer":        map[string]any{"email": r.Email},
		"additional_data": map[string]any{
			"contract":     []string{"recurring"},
			"card_on_file": map[string]any{"initiator": "merchant", "type": "delayed_charge"},
		},
	}}
	var resp txResp
	if err := b.do(ctx, http.MethodPost, b.GatewayURL+"/transactions/payments", "3", body, &resp); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status < 500 {
			return ChargeResult{Status: "failed", Message: ae.Message}, nil
		}
		return ChargeResult{}, err // сетевой сбой: статус уточняется по tracking_id
	}
	return ChargeResult{UID: resp.Transaction.UID, Status: normStatus(resp.Transaction.Status), Message: resp.Transaction.Message}, nil
}

func (b *BePaid) ChargeStatus(ctx context.Context, trackingID string) (ChargeResult, error) {
	var resp struct {
		Transactions []struct {
			UID     string `json:"uid"`
			Status  string `json:"status"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"transactions"`
	}
	if err := b.do(ctx, http.MethodGet, b.GatewayURL+"/v2/transactions/tracking_id/"+trackingID, "", nil, &resp); err != nil {
		return ChargeResult{}, err
	}
	for _, t := range resp.Transactions {
		if t.Type == "payment" || t.Type == "" {
			return ChargeResult{UID: t.UID, Status: normStatus(t.Status), Message: t.Message}, nil
		}
	}
	return ChargeResult{}, errors.New("транзакция не найдена")
}

// Payout: выплаты на карту требуют отдельного договора (contract=credit) — до его подписания
// вывод оформляется вручную в админке.
func (b *BePaid) Payout(ctx context.Context, r ChargeRequest) (ChargeResult, error) {
	return ChargeResult{}, ErrPayoutUnsupported
}

// Refund — возврат по успешной транзакции.
func (b *BePaid) Refund(ctx context.Context, uid string, amountKop int64, reason string) (ChargeResult, error) {
	body := map[string]any{"request": map[string]any{"parent_uid": uid, "amount": amountKop, "reason": reason}}
	var resp txResp
	if err := b.do(ctx, http.MethodPost, b.GatewayURL+"/transactions/refunds", "3", body, &resp); err != nil {
		return ChargeResult{}, err
	}
	st := normStatus(resp.Transaction.Status)
	if st == "failed" {
		return ChargeResult{}, errors.New(resp.Transaction.Message)
	}
	return ChargeResult{UID: resp.Transaction.UID, Status: st, Message: resp.Transaction.Message}, nil
}
