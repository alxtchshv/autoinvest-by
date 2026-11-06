package payments

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"
)

// Банк-эмитент карты определяется так:
//  1. Эквайринг сам присылает название банка при привязке (bePaid — поле issuer_name). Это основной путь.
//  2. Если эквайринг банк не прислал, спрашиваем BIN-справочник по первым 6 цифрам (BIN не является
//     секретом по PCI DSS и не идентифицирует человека). Ответ кэшируется в базе, сервис можно выключить
//     через BIN_LOOKUP=off, а в бою заменить на платную базу или справочник процессинга.

var binRe = regexp.MustCompile(`^\d{6}$`)

// BinList — бесплатный справочник lookup.binlist.net (лимит — несколько запросов в час, поэтому только с кэшем).
type BinList struct {
	Base   string
	Client *http.Client
}

func (b BinList) Issuer(ctx context.Context, bin string) (string, error) {
	if !binRe.MatchString(bin) {
		return "", errors.New("bin: нужно 6 цифр")
	}
	base := b.Base
	if base == "" {
		base = "https://lookup.binlist.net"
	}
	c := b.Client
	if c == nil {
		c = &http.Client{Timeout: 4 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+bin, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept-Version", "3")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("bin: справочник ответил " + resp.Status)
	}
	var out struct {
		Bank struct {
			Name string `json:"name"`
		} `json:"bank"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return "", err
	}
	return out.Bank.Name, nil
}
