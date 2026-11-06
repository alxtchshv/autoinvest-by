package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autoinvest/internal/auth"
	"autoinvest/internal/notify"
	"autoinvest/internal/payments"
	"autoinvest/internal/store"
)

func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	cards, err := s.St.Cards(r.Context(), currentUser(r).ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if cards == nil {
		cards = []store.Card{}
	}
	writeJSON(w, 200, map[string]any{"cards": cards, "provider": s.Pay.Name(), "test": s.Pay.Test()})
}

func (s *Server) startBinding(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if !s.limiter.Allow("bind:"+strconv.FormatInt(u.ID, 10), 10, time.Hour) {
		writeErr(w, http.StatusTooManyRequests, "Слишком много попыток привязки. Попробуйте через час")
		return
	}
	cards, _ := s.St.Cards(r.Context(), u.ID)
	if len(cards) >= 5 {
		writeErr(w, http.StatusConflict, "Можно привязать не больше 5 карт")
		return
	}
	ref := strings.ToLower(auth.RandomCode(20, ""))
	if err := s.St.CreateBinding(r.Context(), store.Binding{Ref: ref, UserID: u.ID, Provider: s.Pay.Name()}); err != nil {
		s.internal(w, r, err)
		return
	}
	url, pref, err := s.Pay.StartBinding(r.Context(), payments.BindRequest{Ref: ref, Email: u.Email,
		ReturnURL: s.Cfg.PublicURL + "/pay/return?ref=" + ref, NotifyURL: s.Cfg.PublicURL + "/api/v1/webhooks/bepaid"})
	if err != nil {
		s.Log.Error("start binding", "err", err)
		s.St.FinishBinding(r.Context(), ref, nil, "Платёжный шлюз недоступен")
		writeErr(w, http.StatusBadGateway, "Платёжный шлюз сейчас недоступен. Попробуйте через несколько минут")
		return
	}
	s.St.SetBindingProviderRef(r.Context(), ref, pref)
	writeJSON(w, 200, map[string]string{"ref": ref, "redirect_url": url})
}

// bindingStatus — фронтенд опрашивает после возврата со страницы оплаты.
func (s *Server) bindingStatus(w http.ResponseWriter, r *http.Request) {
	b, err := s.St.GetBinding(r.Context(), r.PathValue("ref"))
	if err != nil || b.UserID != currentUser(r).ID {
		writeErr(w, http.StatusNotFound, "Привязка не найдена")
		return
	}
	if b.Status == "pending" && b.Provider == "bepaid" && b.ProviderRef != "" {
		s.pollBinding(r, b)
		b, _ = s.St.GetBinding(r.Context(), b.Ref)
	}
	var card *store.Card
	if b.CardID != nil {
		card, _ = s.St.Card(r.Context(), b.UserID, *b.CardID)
	}
	writeJSON(w, 200, map[string]any{"status": b.Status, "error": b.Error, "card": card})
}

func (s *Server) pollBinding(r *http.Request, b *store.Binding) {
	res, err := s.Pay.BindingStatus(r.Context(), b.ProviderRef)
	if err != nil {
		return
	}
	switch res.Status {
	case "done":
		s.completeBinding(r, b.Ref, res.Card)
	case "failed":
		s.St.FinishBinding(r.Context(), b.Ref, nil, res.Error)
	}
}

func (s *Server) completeBinding(r *http.Request, ref string, c payments.Card) {
	if c.Issuer == "" {
		c.Issuer = s.issuerByBIN(r.Context(), c.BIN)
	}
	card, err := s.St.FinishBinding(r.Context(), ref, &store.Card{Provider: s.Pay.Name(), Token: c.Token, Brand: c.Brand, Last4: c.Last4,
		ExpMonth: c.ExpMonth, ExpYear: c.ExpYear, Holder: c.Holder, Issuer: c.Issuer, BIN: c.BIN}, "")
	if err == nil && card != nil {
		s.St.Audit(r.Context(), card.UserID, card.UserID, "card.added", card.Brand+" •• "+card.Last4, clientIP(r))
		s.N.Send(r.Context(), card.UserID, notify.Message{Kind: "security", Title: "Карта •• " + card.Last4 + " привязана", Body: "С неё будут пополняться копилки.", Link: "/app/cards", Email: true})
	}
}

// payReturn — сюда bePaid возвращает пользователя после страницы привязки.
func (s *Server) payReturn(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("ref")
	if b, err := s.St.GetBinding(r.Context(), ref); err == nil && b.Status == "pending" && b.ProviderRef != "" {
		s.pollBinding(r, b)
	}
	http.Redirect(w, r, "/app/cards?bind="+ref, http.StatusSeeOther)
}

func (s *Server) bepaidWebhook(w http.ResponseWriter, r *http.Request) {
	if s.Pay.Name() != "bepaid" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	ev, err := s.Pay.ParseWebhook(r, body)
	if err != nil {
		s.Log.Warn("bepaid webhook rejected", "err", err, "ip", clientIP(r))
		writeErr(w, http.StatusUnauthorized, "bad signature")
		return
	}
	switch {
	case strings.HasPrefix(ev.TrackingID, "bind_"):
		ref := strings.TrimPrefix(ev.TrackingID, "bind_")
		if ev.Status == "successful" && ev.Card.Token != "" {
			s.completeBinding(r, ref, ev.Card)
		} else if ev.Status == "failed" {
			s.St.FinishBinding(r.Context(), ref, nil, ev.Message)
		}
	case strings.HasPrefix(ev.TrackingID, "ord_") && ev.Kind == "payment":
		// окончательный статус списания (например, после 3-D Secure) — доводим заявку, если совпадает сумма
		oid, _ := strconv.ParseInt(strings.TrimPrefix(ev.TrackingID, "ord_"), 10, 64)
		var pid, amount int64
		s.DB.QueryRowContext(r.Context(), `SELECT payment_id, amount_kop FROM orders WHERE id=?`, oid).Scan(&pid, &amount)
		if pid > 0 && ev.AmountKop == amount {
			s.En.SettleExternal(r.Context(), oid, pid, payments.ChargeResult{UID: ev.UID, Status: ev.Status, Message: ev.Message})
		} else if pid > 0 {
			s.Log.Error("bepaid webhook amount mismatch", "order", oid, "got", ev.AmountKop, "want", amount)
		}
	case strings.HasPrefix(ev.TrackingID, "pro_") && ev.Kind == "payment":
		var pid, amount int64
		s.DB.QueryRowContext(r.Context(), `SELECT id, amount_kop FROM payments WHERE tracking_id=? AND kind='subscription'`, ev.TrackingID).Scan(&pid, &amount)
		if pid > 0 && ev.AmountKop == amount && (ev.Status == "successful" || ev.Status == "failed") {
			s.En.SettlePro(r.Context(), pid, payments.ChargeResult{UID: ev.UID, Status: ev.Status, Message: ev.Message}, false)
		}
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *Server) defaultCard(w http.ResponseWriter, r *http.Request) {
	if err := s.St.SetDefaultCard(r.Context(), currentUser(r).ID, pathID(r)); err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), currentUser(r).ID, currentUser(r).ID, "card.default", fmt.Sprint(pathID(r)), clientIP(r))
	s.listCards(w, r)
}

func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	card, err := s.St.Card(r.Context(), u.ID, pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	paused, err := s.St.RemoveCard(r.Context(), u.ID, card.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), u.ID, u.ID, "card.removed", card.Brand+" •• "+card.Last4, clientIP(r))
	cards, _ := s.St.Cards(r.Context(), u.ID)
	if cards == nil {
		cards = []store.Card{}
	}
	writeJSON(w, 200, map[string]any{"cards": cards, "paused_plans": paused})
}

// ---------------------------------------------------------------- тестовая платёжная страница

func (s *Server) sandboxBinding(w http.ResponseWriter, r *http.Request) *store.Binding {
	if s.Sandbox == nil {
		writeErr(w, http.StatusNotFound, "Тестовая платёжка выключена")
		return nil
	}
	b, err := s.St.GetBinding(r.Context(), r.PathValue("ref"))
	if err != nil || b.UserID != currentUser(r).ID || b.Provider != "sandbox" {
		writeErr(w, http.StatusNotFound, "Платёж не найден")
		return nil
	}
	return b
}

func (s *Server) sandboxCheckout(w http.ResponseWriter, r *http.Request) {
	b := s.sandboxBinding(w, r)
	if b == nil {
		return
	}
	writeJSON(w, 200, map[string]any{"status": b.Status, "test_cards": payments.TestCards, "email": currentUser(r).Email,
		"expired": time.Since(time.Unix(b.CreatedAt, 0)) > 30*time.Minute})
}

func (s *Server) sandboxPay(w http.ResponseWriter, r *http.Request) {
	b := s.sandboxBinding(w, r)
	if b == nil {
		return
	}
	if b.Status != "pending" || time.Since(time.Unix(b.CreatedAt, 0)) > 30*time.Minute {
		writeErr(w, http.StatusGone, "Сессия оплаты истекла. Начните привязку заново")
		return
	}
	var in struct {
		Number   string `json:"number"`
		Holder   string `json:"holder"`
		ExpMonth int    `json:"exp_month"`
		ExpYear  int    `json:"exp_year"`
		CVC      string `json:"cvc"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if len(in.CVC) != 3 {
		writeFieldErr(w, "cvc", "Три цифры на обороте карты")
		return
	}
	if in.ExpYear < 100 {
		in.ExpYear += 2000
	}
	card, err := s.Sandbox.Tokenize(in.Number, in.Holder, in.ExpMonth, in.ExpYear)
	if errors.Is(err, payments.ErrDeclined) {
		s.St.FinishBinding(r.Context(), b.Ref, nil, "Банк отклонил привязку карты")
		writeJSON(w, 200, map[string]any{"status": "failed", "redirect": "/app/cards?bind=" + b.Ref})
		return
	}
	if err != nil {
		writeFieldErr(w, "number", err.Error())
		return
	}
	s.completeBinding(r, b.Ref, card)
	writeJSON(w, 200, map[string]any{"status": "done", "redirect": "/app/cards?bind=" + b.Ref})
}

// issuerByBIN — запасной способ узнать банк карты, если эквайринг не прислал issuer_name. Ответы кэшируются.
func (s *Server) issuerByBIN(ctx context.Context, bin string) string {
	if s.Cfg.BinLookup == "off" || len(bin) != 6 {
		return ""
	}
	key := "bin_issuer:" + bin
	if v := s.St.GetKV(ctx, key); v != "" {
		return v
	}
	c, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	name, err := payments.BinList{}.Issuer(c, bin)
	if err != nil || name == "" {
		s.Log.Debug("банк по BIN не определён", "bin", bin, "err", err)
		return ""
	}
	s.St.SetKV(ctx, key, name)
	return name
}
