package payments

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBinListIssuer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/420000" || r.Header.Get("Accept-Version") != "3" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		w.Write([]byte(`{"bank":{"name":"Test Bank"},"country":{"alpha2":"BY"}}`))
	}))
	defer srv.Close()
	got, err := BinList{Base: srv.URL}.Issuer(context.Background(), "420000")
	if err != nil || got != "Test Bank" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := (BinList{Base: srv.URL}).Issuer(context.Background(), "42ab"); err == nil {
		t.Error("non-digit BIN must be rejected before any request")
	}
}
