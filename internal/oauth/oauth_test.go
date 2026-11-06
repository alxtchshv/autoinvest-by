package oauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGoogleExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			r.ParseForm()
			if r.Form.Get("code") != "c1" || r.Form.Get("client_secret") != "sec" {
				http.Error(w, "bad", 400)
				return
			}
			w.Write([]byte(`{"access_token":"at"}`))
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer at" {
				http.Error(w, "no", 401)
				return
			}
			w.Write([]byte(`{"sub":"g-1","email":"Ivan@Gmail.com","email_verified":true,"name":"Иван"}`))
		}
	}))
	defer srv.Close()
	g := &Google{ClientID: "id", ClientSecret: "sec", RedirectURL: "http://x/cb", TokenURL: srv.URL + "/token", UserInfoURL: srv.URL + "/userinfo"}
	p, err := g.Exchange(context.Background(), "c1")
	if err != nil || p.Subject != "g-1" || p.Email != "ivan@gmail.com" || !p.EmailVerified {
		t.Fatalf("%+v %v", p, err)
	}
	if u := g.AuthURL("st"); !strings.Contains(u, "state=st") || !strings.Contains(u, "scope=openid+email+profile") {
		t.Fatal(u)
	}
}

func TestAppleSecretAndExchange(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	now := time.Unix(1800000000, 0)
	claims, _ := json.Marshal(map[string]any{"iss": "https://appleid.apple.com", "aud": "by.autoinvest.web", "exp": now.Add(time.Hour).Unix(), "sub": "a-1", "email": "x@privaterelay.appleid.com", "email_verified": "true"})
	idTok := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"
	var secret string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		secret = r.Form.Get("client_secret")
		w.Write([]byte(`{"id_token":"` + idTok + `"}`))
	}))
	defer srv.Close()
	a := &Apple{ClientID: "by.autoinvest.web", TeamID: "TEAM", KeyID: "KID", PrivateKey: pemKey, RedirectURL: "http://x/cb", TokenURL: srv.URL, now: func() time.Time { return now }}
	p, err := a.Exchange(context.Background(), "code", `{"name":{"firstName":"Иван","lastName":"Петров"}}`)
	if err != nil || p.Subject != "a-1" || !p.EmailVerified || p.Name != "Иван Петров" {
		t.Fatalf("%+v %v", p, err)
	}
	if !verifyES256(&key.PublicKey, secret) {
		t.Fatal("подпись client_secret не проверяется публичным ключом")
	}
	// чужой aud не принимаем
	a.ClientID = "other"
	if _, err := a.Exchange(context.Background(), "code", ""); err == nil {
		t.Fatal("id_token для другого клиента принят")
	}
}
