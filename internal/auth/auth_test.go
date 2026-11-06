package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPassword(t *testing.T) {
	h, err := HashPassword("секрет123")
	if err != nil || !strings.HasPrefix(h, "pbkdf2-sha256$") {
		t.Fatal(h, err)
	}
	if !CheckPassword(h, "секрет123") || CheckPassword(h, "секрет124") || CheckPassword("garbage", "x") {
		t.Fatal("check failed")
	}
	for _, bad := range []string{"short1", "onlyletters", "12345678"} {
		if ValidatePassword(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if ValidatePassword("пароль2026") != nil {
		t.Error("cyrillic password rejected")
	}
}

func TestTOTP(t *testing.T) {
	// RFC 6238, SHA1, секрет "12345678901234567890"
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	if c := TOTPCode(secret, time.Unix(59, 0)); c != "287082" {
		t.Fatalf("rfc vector: %s", c)
	}
	now := time.Now()
	if !VerifyTOTP(secret, TOTPCode(secret, now.Add(-30*time.Second)), now) {
		t.Error("drift -30s rejected")
	}
	if VerifyTOTP(secret, TOTPCode(secret, now.Add(-120*time.Second)), now) {
		t.Error("old code accepted")
	}
}

func TestTOTPReplay(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Now()
	code := TOTPCode(secret, now)
	step, ok := VerifyTOTPStep(secret, code, now, 0)
	if !ok {
		t.Fatal("valid code rejected")
	}
	if _, ok := VerifyTOTPStep(secret, code, now, step); ok {
		t.Fatal("replayed code accepted")
	}
}
