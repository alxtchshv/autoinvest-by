package payments

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBePaidCharge(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transactions/payments" || r.Header.Get("X-API-Version") != "3" {
			t.Errorf("bad request %s %s", r.URL.Path, r.Header.Get("X-API-Version"))
		}
		if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("361:secret")) {
			t.Error("auth")
		}
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"transaction":{"uid":"u-1","status":"successful","message":"ok"}}`)
	}))
	defer srv.Close()
	b := NewBePaid("361", "secret", true, "")
	b.GatewayURL = srv.URL
	res, err := b.Charge(context.Background(), ChargeRequest{Token: "tok", AmountKop: 1500, Description: "d", TrackingID: "ord_1"})
	if err != nil || res.Status != "successful" || res.UID != "u-1" {
		t.Fatal(err, res)
	}
	req := got["request"].(map[string]any)
	if req["amount"].(float64) != 1500 || req["currency"] != "BYN" || req["credit_card"].(map[string]any)["token"] != "tok" {
		t.Fatalf("%+v", req)
	}
	if c := req["additional_data"].(map[string]any)["contract"].([]any); c[0] != "recurring" {
		t.Fatal("contract")
	}
}

func TestBePaidDeclineIsNotError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		io.WriteString(w, `{"message":"Insufficient funds"}`)
	}))
	defer srv.Close()
	b := NewBePaid("1", "s", true, "")
	b.GatewayURL = srv.URL
	res, err := b.Charge(context.Background(), ChargeRequest{Token: "t", AmountKop: 1, TrackingID: "x"})
	if err != nil || res.Status != "failed" || !strings.Contains(res.Message, "Insufficient") {
		t.Fatal(err, res)
	}
}

func TestBePaidWebhook(t *testing.T) {
	b := NewBePaid("361", "secret", true, "")
	body := []byte(`{"transaction":{"uid":"u","type":"tokenization","status":"successful","tracking_id":"bind_abc","credit_card":{"token":"T","last_4":"1111","brand":"visa","exp_month":5,"exp_year":2030,"holder":"IVAN"}}}`)
	r := httptest.NewRequest("POST", "/", nil)
	if _, err := b.ParseWebhook(r, body); err == nil {
		t.Fatal("unauthenticated webhook accepted")
	}
	r.SetBasicAuth("361", "secret")
	ev, err := b.ParseWebhook(r, body)
	if err != nil || ev.TrackingID != "bind_abc" || ev.Card.Token != "T" || ev.Card.Last4 != "1111" || ev.Card.ExpYear != 2030 {
		t.Fatal(err, ev)
	}
}

func TestSandboxRejectsRealCards(t *testing.T) {
	s := NewSandbox("http://x")
	if _, err := s.Tokenize("4111 1111 1111 1111", "A", 12, 2030); err == nil {
		t.Fatal("real card accepted")
	}
	c, err := s.Tokenize("4200 0000 0000 0000", "A", 12, 2099)
	if err != nil || c.Last4 != "0000" {
		t.Fatal(err)
	}
	r1, _ := s.Charge(context.Background(), ChargeRequest{Token: c.Token, AmountKop: 100, TrackingID: "t"})
	r2, _ := s.Charge(context.Background(), ChargeRequest{Token: c.Token, AmountKop: 100, TrackingID: "t"})
	if r1.UID != r2.UID {
		t.Fatal("sandbox not idempotent")
	}
}
