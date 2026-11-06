// Package auth — хеширование паролей, токены сессий и TOTP (Google Authenticator).
package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Параметры PBKDF2-SHA256 по рекомендации OWASP (2023+).
const (
	pbkdf2Iter = 600_000
	saltLen    = 16
	keyLen     = 32
)

// HashPassword возвращает строку вида pbkdf2-sha256$iter$salt$hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iter, keyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iter,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckPassword сравнивает пароль с хешем за постоянное время.
func CheckPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1000 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ValidatePassword — минимальные требования к паролю.
func ValidatePassword(p string) error {
	if len([]rune(p)) < 8 {
		return errors.New("Пароль должен быть не короче 8 символов")
	}
	if len(p) > 128 {
		return errors.New("Пароль слишком длинный")
	}
	hasLetter, hasDigit := false, false
	for _, r := range p {
		switch {
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsLetter(r):
			hasLetter = true
		}
	}
	if !hasLetter || !hasDigit {
		return errors.New("Пароль должен содержать буквы и цифры")
	}
	return nil
}

// NewToken — криптостойкий токен (URL-safe) и его sha256 для хранения в БД.
func NewToken() (token, hash string) {
	b := make([]byte, 32)
	rand.Read(b)
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token)
}

func HashToken(token string) string {
	s := sha256.Sum256([]byte(token))
	return hex.EncodeToString(s[:])
}

// RandomCode — короткий код из заданного алфавита (для рефералок, привязки Telegram и т.п.).
func RandomCode(n int, alphabet string) string {
	if alphabet == "" {
		alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	}
	b := make([]byte, n)
	rand.Read(b)
	out := make([]byte, n)
	for i := range b {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out)
}

// --- TOTP (RFC 6238) ---

func NewTOTPSecret() string {
	b := make([]byte, 20)
	rand.Read(b)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

// TOTPURL — otpauth:// ссылка для QR-кода.
func TOTPURL(issuer, account, secret string) string {
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}

func totpAt(secret string, counter uint64) (string, error) {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff) % 1_000_000
	return fmt.Sprintf("%06d", code), nil
}

// TOTPCode — текущий код (используется в тестах).
func TOTPCode(secret string, t time.Time) string {
	c, _ := totpAt(secret, uint64(t.Unix()/30))
	return c
}

// VerifyTOTP допускает расхождение часов ±1 шаг (30 секунд).
func VerifyTOTP(secret, code string, now time.Time) bool {
	_, ok := VerifyTOTPStep(secret, code, now, 0)
	return ok
}

// VerifyTOTPStep проверяет код и возвращает его временной шаг; коды с шагом <= lastStep отклоняются (защита от повтора).
func VerifyTOTPStep(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0, false
	}
	step := now.Unix() / 30
	for d := int64(-1); d <= 1; d++ {
		c, err := totpAt(secret, uint64(step+d))
		if err == nil && subtle.ConstantTimeCompare([]byte(c), []byte(code)) == 1 && step+d > lastStep {
			return step + d, true
		}
	}
	return 0, false
}

// NewRecoveryCodes — 8 одноразовых кодов восстановления и их хеши.
func NewRecoveryCodes() (plain []string, hashes []string) {
	for i := 0; i < 8; i++ {
		c := strings.ToLower(RandomCode(4, "") + "-" + RandomCode(4, ""))
		plain = append(plain, c)
		hashes = append(hashes, HashToken(c))
	}
	return
}
