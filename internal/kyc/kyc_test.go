package kyc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSumsubSignsRequests(t *testing.T) {
	var got *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		body, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"token":"_act-123","userId":"ai-7"}`))
	}))
	defer srv.Close()
	s := &Sumsub{Base: srv.URL, AppToken: "tok", Secret: "sec", Level: "basic-kyc-level", now: func() time.Time { return time.Unix(1700000000, 0) }}
	tok, err := s.AccessToken(context.Background(), "ai-7", "")
	if err != nil || tok != "_act-123" {
		t.Fatalf("token %q %v", tok, err)
	}
	if got.URL.Path != "/resources/accessTokens/sdk" || got.Header.Get("X-App-Token") != "tok" || got.Header.Get("X-App-Access-Ts") != "1700000000" {
		t.Fatalf("заголовки: %s %v", got.URL.Path, got.Header)
	}
	mac := hmac.New(sha256.New, []byte("sec"))
	mac.Write([]byte("1700000000POST/resources/accessTokens/sdk"))
	mac.Write(body)
	if want := hex.EncodeToString(mac.Sum(nil)); got.Header.Get("X-App-Access-Sig") != want {
		t.Fatalf("подпись %s, ждали %s", got.Header.Get("X-App-Access-Sig"), want)
	}
}

func TestSumsubWebhookDigest(t *testing.T) {
	s := &Sumsub{WebhookSecret: "hook"}
	body := []byte(`{"type":"applicantReviewed"}`)
	mac := hmac.New(sha256.New, []byte("hook"))
	mac.Write(body)
	d := hex.EncodeToString(mac.Sum(nil))
	if !s.VerifyWebhook(body, d, "HMAC_SHA256_HEX") || !s.VerifyWebhook(body, d, "") {
		t.Fatal("верная подпись отклонена")
	}
	if s.VerifyWebhook(append(body, ' '), d, "") || s.VerifyWebhook(body, d, "MD5") || s.VerifyWebhook(body, "", "") {
		t.Fatal("неверная подпись принята")
	}
}

func TestBirthFromPersonal(t *testing.T) {
	b, ok := BirthFromPersonal("3010195A001PB7")
	if !ok || b.Format("2006-01-02") != "1995-01-01" {
		t.Fatalf("%v %v", b, ok)
	}
	if _, ok := BirthFromPersonal("3320195A001PB7"); ok {
		t.Fatal("32-е число принято")
	}
}

func TestMSIClaimsMapping(t *testing.T) {
	p := fromClaims(map[string]any{"family_name": "Петров", "given_name": "Иван", "personal_number": "3010195a001pb7", "birthdate": "1995-01-01"})
	if p.LastName != "Петров" || p.FirstName != "Иван" || p.PersonalNumber != "3010195A001PB7" || p.BirthDate != "1995-01-01" {
		t.Fatalf("%+v", p)
	}
}
