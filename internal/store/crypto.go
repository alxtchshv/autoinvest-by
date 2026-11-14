package store

// Шифрование чувствительных полей в базе (AES-256-GCM). Ключ — DATA_KEY из окружения сервера, в базе его нет:
// утёкший файл базы или резервная копия без ключа не раскрывают секреты Google Authenticator, токены карт
// и паспортные данные. Пароли, коды и ссылки по-прежнему хранятся только в виде хэшей (их не нужно расшифровывать).

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
)

const encPrefix = "enc:v1:"

var (
	keyMu    sync.RWMutex
	fieldKey []byte
)

// SetDataKey — ключ шифрования (32 байта). Вызывается при старте до работы с базой.
func SetDataKey(k []byte) error {
	if len(k) != 32 {
		return errors.New("DATA_KEY должен быть 32 байта (base64 от 32 случайных байт: openssl rand -base64 32)")
	}
	keyMu.Lock()
	fieldKey = append([]byte(nil), k...)
	keyMu.Unlock()
	return nil
}

func dataKey() []byte {
	keyMu.RLock()
	k := fieldKey
	keyMu.RUnlock()
	if k != nil {
		return k
	}
	// ключ не задан (тесты) — случайный на время работы процесса
	keyMu.Lock()
	defer keyMu.Unlock()
	if fieldKey == nil {
		fieldKey = make([]byte, 32)
		rand.Read(fieldKey)
	}
	return fieldKey
}

// seal шифрует значение; пустое и уже зашифрованное возвращает как есть.
func seal(plain string) string {
	if plain == "" || strings.HasPrefix(plain, encPrefix) {
		return plain
	}
	block, _ := aes.NewCipher(dataKey())
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	return encPrefix + base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil))
}

// unseal расшифровывает; старые незашифрованные значения возвращает как есть (их перешифрует EncryptLegacy).
func unseal(v string) string {
	if !strings.HasPrefix(v, encPrefix) {
		return v
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(v, encPrefix))
	if err != nil {
		return ""
	}
	block, _ := aes.NewCipher(dataKey())
	gcm, _ := cipher.NewGCM(block)
	if len(raw) < gcm.NonceSize() {
		return ""
	}
	out, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "" // другой ключ — значение не читается (лучше пусто, чем мусор)
	}
	return string(out)
}

// blindIndex — постоянный «отпечаток» значения для поиска и уникальности без хранения самого значения.
func blindIndex(purpose, v string) string {
	m := hmac.New(sha256.New, dataKey())
	m.Write([]byte(purpose + ":" + v))
	return "h:" + hex.EncodeToString(m.Sum(nil))
}

func sha(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}

// EncryptLegacy — один раз после обновления: зашифровать то, что лежит в базе открытым текстом.
func (s *Store) EncryptLegacy(ctx context.Context) error {
	type row struct {
		id int64
		v  string
	}
	pick := func(q string) []row {
		rows, err := s.DB.QueryContext(ctx, q)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var out []row
		for rows.Next() {
			var r row
			if rows.Scan(&r.id, &r.v) == nil {
				out = append(out, r)
			}
		}
		return out
	}
	for _, r := range pick(`SELECT id, totp_secret FROM users WHERE totp_secret!='' AND totp_secret NOT LIKE 'enc:v1:%'`) {
		s.DB.ExecContext(ctx, `UPDATE users SET totp_secret=? WHERE id=?`, seal(r.v), r.id)
	}
	for _, r := range pick(`SELECT id, token FROM cards WHERE token!='' AND token NOT LIKE 'enc:v1:%'`) {
		s.DB.ExecContext(ctx, `UPDATE cards SET token=? WHERE id=?`, seal(r.v), r.id)
	}
	// старые ссылки привязки Telegram хранились открытым текстом — они живут 15 минут, проще удалить
	s.DB.ExecContext(ctx, `DELETE FROM telegram_links WHERE length(code) != 64`)
	// KYC: идентификационный номер → отпечаток (для уникальности) + шифр; документ, адрес, дата рождения — шифр
	rows, err := s.DB.QueryContext(ctx, `SELECT user_id, personal_number, doc_number, address, birth_date FROM kyc
		WHERE personal_number NOT LIKE 'h:%' OR doc_number NOT LIKE 'enc:v1:%' OR address NOT LIKE 'enc:v1:%' OR birth_date NOT LIKE 'enc:v1:%'`)
	if err != nil {
		return err
	}
	type k struct {
		uid                  int64
		pn, doc, addr, birth string
	}
	var list []k
	for rows.Next() {
		var x k
		if rows.Scan(&x.uid, &x.pn, &x.doc, &x.addr, &x.birth) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	for _, x := range list {
		pn, pnEnc := x.pn, ""
		if !strings.HasPrefix(pn, "h:") && !strings.HasPrefix(pn, "pending-") {
			pn, pnEnc = blindIndex("pn", x.pn), seal(x.pn)
		}
		if pnEnc != "" {
			s.DB.ExecContext(ctx, `UPDATE kyc SET personal_number=?, personal_number_enc=? WHERE user_id=?`, pn, pnEnc, x.uid)
		}
		s.DB.ExecContext(ctx, `UPDATE kyc SET doc_number=?, address=?, birth_date=? WHERE user_id=?`, seal(x.doc), seal(x.addr), seal(x.birth), x.uid)
	}
	return nil
}
