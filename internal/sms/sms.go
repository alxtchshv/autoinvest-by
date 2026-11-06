// Package sms — доставка одноразовых кодов на телефон.
//
// Провайдеры:
//   - sandbox — ничего не отправляет; последний код по номеру виден в тестовом режиме (стенд, разработка);
//   - telegram — Telegram Gateway (https://core.telegram.org/gateway): код приходит в Telegram владельцу номера,
//     дешевле SMS и не требует договора с белорусским SMS-агрегатором;
//   - http — любой SMS-шлюз: POST JSON {"phone": "+375…", "text": "…"} с заголовком Authorization: Bearer <token>.
//     Для конкретного агрегатора (sms-assistent.by, rocketsms.by и т. п.) достаточно поменять формат запроса в httpSender.
package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Sender отправляет код подтверждения. text — готовый текст для SMS, code — сами цифры (нужны Telegram Gateway).
type Sender interface {
	SendCode(ctx context.Context, phone, code, text string) error
	Name() string
}

// ---------------------------------------------------------------- sandbox

// Sandbox запоминает последний код по номеру — его показывает тестовая страница входа.
type Sandbox struct {
	Log  *slog.Logger
	mu   sync.Mutex
	last map[string]string
}

func NewSandbox(log *slog.Logger) *Sandbox { return &Sandbox{Log: log, last: map[string]string{}} }

func (s *Sandbox) Name() string { return "sandbox" }

func (s *Sandbox) SendCode(_ context.Context, phone, code, _ string) error {
	s.mu.Lock()
	s.last[phone] = code
	s.mu.Unlock()
	if s.Log != nil {
		s.Log.Info("sms (тестовый режим)", "phone", phone, "code", code)
	}
	return nil
}

// Last — последний «отправленный» код (только для тестового режима).
func (s *Sandbox) Last(phone string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last[phone]
}

// ---------------------------------------------------------------- Telegram Gateway

type TelegramGateway struct {
	Token  string
	Base   string // https://gatewayapi.telegram.org
	Client *http.Client
}

func (t *TelegramGateway) Name() string { return "telegram" }

func (t *TelegramGateway) SendCode(ctx context.Context, phone, code, _ string) error {
	base := t.Base
	if base == "" {
		base = "https://gatewayapi.telegram.org"
	}
	body, _ := json.Marshal(map[string]any{"phone_number": phone, "code": code, "ttl": 600})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/sendVerificationMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("Content-Type", "application/json")
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := doJSON(t.Client, req, &out); err != nil {
		return err
	}
	if !out.OK {
		return fmt.Errorf("telegram gateway: %s", out.Error)
	}
	return nil
}

// ---------------------------------------------------------------- произвольный SMS-шлюз

type HTTPSender struct {
	URL    string
	Token  string
	Client *http.Client
}

func (h *HTTPSender) Name() string { return "http" }

func (h *HTTPSender) SendCode(ctx context.Context, phone, _ string, text string) error {
	body, _ := json.Marshal(map[string]string{"phone": phone, "text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.Token != "" {
		req.Header.Set("Authorization", "Bearer "+h.Token)
	}
	return doJSON(h.Client, req, nil)
}

func doJSON(c *http.Client, req *http.Request, out any) error {
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("sms %s: %d %s", req.URL.Host, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
