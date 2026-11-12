// Package config читает настройки сервиса из переменных окружения (и файла .env, если он есть).
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Режимы работы платёжного контура.
const (
	PaymentsSandbox = "sandbox" // встроенная тестовая платёжка, деньги не списываются
	PaymentsBePaid  = "bepaid"  // реальный эквайринг bePaid (test=true или боевой)
)

// Режимы исполнения сделок.
const (
	BrokerPaper     = "paper"     // учебный счёт: сделки по реальным рыночным ценам, без реальной покупки
	BrokerWhitebird = "whitebird" // партнёрский шлюз (нужен договор и ключи)
)

// Режимы рыночных данных.
const (
	MarketLive    = "live"    // Binance → Bybit → CoinGecko, курсы НБРБ
	MarketFixture = "fixture" // только для разработки без интернета
)

type Config struct {
	Addr          string
	PublicURL     string // внешний адрес сайта, например https://autoinvest.by
	DataDir       string
	Env           string // development | production
	SecureCookies bool

	Payments        string
	BePaidShopID    string
	BePaidSecret    string
	BePaidTest      bool
	BePaidPublicKey string // PEM для проверки Content-Signature вебхуков (необязательно)

	Broker          string
	WhitebirdURL    string
	WhitebirdKey    string
	WhitebirdSecret string

	Market     string
	FinnhubKey string

	TelegramToken    string
	BinLookup        string // binlist | off — запасной справочник банка по BIN, если эквайринг его не прислал
	TelegramUsername string
	SupportChatID    int64 // чат, куда бот пересылает новые обращения

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	SMTPFrom string

	AdminEmail    string
	AdminPassword string

	RequireKYC   bool
	LaunchMode   string // open | waitlist — до запуска: копилку настроить можно, оплатить нельзя (лист ожидания + демо-счёт)
	KYCDocs      string // sandbox | sumsub | manual — проверка по паспорту и селфи
	MSIMode      string // sandbox | oauth | off — вход через межбанковскую систему идентификации
	SumsubBase   string
	SumsubToken  string
	SumsubSecret string
	SumsubHook   string // секрет вебхука (X-Payload-Digest)
	SumsubLevel  string
	MSIAuthURL   string
	MSITokenURL  string
	MSIUserURL   string
	MSIClientID  string
	MSISecret    string
	MSIScope     string

	OAuthGoogle        string // sandbox | on | off
	GoogleClientID     string
	GoogleClientSecret string
	OAuthApple         string // sandbox | on | off
	AppleClientID      string // Services ID
	AppleTeamID        string
	AppleKeyID         string
	ApplePrivateKey    string // содержимое .p8

	TelegramSandbox bool  // встроенный тестовый Telegram (без токена бота) — только не в продакшене
	FeeFreeBP       int64 // комиссия сервиса за покупку, базисные пункты (100 = 1%)
	FeeProBP        int64
	SpreadBP        int64 // спред партнёра к рыночной цене
	ProMonthKop     int64
	ProYearKop      int64
	MinContribKop   int64
	MaxContribKop   int64
	FreePlanLimit   int
	ProPlanLimit    int
	SchedulerEvery  time.Duration
}

// Load читает .env (если есть) и окружение.
func Load() (*Config, error) {
	loadDotEnv(".env")
	c := &Config{
		Addr:             env("ADDR", ":8080"),
		PublicURL:        strings.TrimRight(env("PUBLIC_URL", "http://localhost:8080"), "/"),
		DataDir:          env("DATA_DIR", "data"),
		Env:              env("APP_ENV", "development"),
		Payments:         env("PAYMENTS", PaymentsSandbox),
		BePaidShopID:     env("BEPAID_SHOP_ID", ""),
		BePaidSecret:     env("BEPAID_SECRET_KEY", ""),
		BePaidTest:       envBool("BEPAID_TEST", true),
		BePaidPublicKey:  env("BEPAID_PUBLIC_KEY", ""),
		Broker:           env("BROKER", BrokerPaper),
		WhitebirdURL:     env("WHITEBIRD_API_URL", ""),
		WhitebirdKey:     env("WHITEBIRD_API_KEY", ""),
		WhitebirdSecret:  env("WHITEBIRD_API_SECRET", ""),
		Market:           env("MARKET", MarketLive),
		FinnhubKey:       env("FINNHUB_API_KEY", ""),
		TelegramToken:    env("TELEGRAM_BOT_TOKEN", ""),
		BinLookup:        env("BIN_LOOKUP", "binlist"),
		TelegramUsername: strings.TrimPrefix(env("TELEGRAM_BOT_USERNAME", ""), "@"),
		SupportChatID:    envInt64("TELEGRAM_SUPPORT_CHAT_ID", 0),
		SMTPHost:         env("SMTP_HOST", ""),
		SMTPPort:         int(envInt64("SMTP_PORT", 587)),
		SMTPUser:         env("SMTP_USER", ""),
		SMTPPass:         env("SMTP_PASSWORD", ""),
		SMTPFrom:         env("SMTP_FROM", "AutoInvest <no-reply@autoinvest.by>"),
		AdminEmail:       env("ADMIN_EMAIL", ""),
		AdminPassword:    env("ADMIN_PASSWORD", ""),
		RequireKYC:       envBool("REQUIRE_KYC", false),
		FeeFreeBP:        envInt64("FEE_FREE_BP", 100),
		FeeProBP:         envInt64("FEE_PRO_BP", 50),
		SpreadBP:         envInt64("SPREAD_BP", 100),
		ProMonthKop:      envInt64("PRO_MONTH_KOP", 990),
		ProYearKop:       envInt64("PRO_YEAR_KOP", 8900),
		MinContribKop:    envInt64("MIN_CONTRIBUTION_KOP", 500),
		MaxContribKop:    envInt64("MAX_CONTRIBUTION_KOP", 500000),
		FreePlanLimit:    int(envInt64("FREE_PLAN_LIMIT", 3)),
		ProPlanLimit:     int(envInt64("PRO_PLAN_LIMIT", 20)),
		SchedulerEvery:   time.Duration(envInt64("SCHEDULER_SECONDS", 30)) * time.Second,
	}
	c.SumsubBase = strings.TrimRight(env("SUMSUB_BASE_URL", "https://api.sumsub.com"), "/")
	c.SumsubToken, c.SumsubSecret = env("SUMSUB_APP_TOKEN", ""), env("SUMSUB_SECRET_KEY", "")
	c.SumsubHook, c.SumsubLevel = env("SUMSUB_WEBHOOK_SECRET", ""), env("SUMSUB_LEVEL", "basic-kyc-level")
	c.MSIAuthURL, c.MSITokenURL, c.MSIUserURL = env("MSI_AUTH_URL", ""), env("MSI_TOKEN_URL", ""), env("MSI_USERINFO_URL", "")
	c.MSIClientID, c.MSISecret, c.MSIScope = env("MSI_CLIENT_ID", ""), env("MSI_CLIENT_SECRET", ""), env("MSI_SCOPE", "")
	// по умолчанию: есть ключи — боевой провайдер; нет ключей — тестовый режим при разработке и ручная проверка в продакшене
	docs, msi := "sandbox", "sandbox"
	if c.Env == "production" {
		docs, msi = "manual", "off"
	}
	if c.SumsubToken != "" {
		docs = "sumsub"
	}
	if c.MSIClientID != "" {
		msi = "oauth"
	}
	c.KYCDocs, c.MSIMode = env("KYC_DOCS", docs), env("MSI_MODE", msi)

	c.GoogleClientID, c.GoogleClientSecret = env("GOOGLE_CLIENT_ID", ""), env("GOOGLE_CLIENT_SECRET", "")
	c.AppleClientID, c.AppleTeamID, c.AppleKeyID = env("APPLE_CLIENT_ID", ""), env("APPLE_TEAM_ID", ""), env("APPLE_KEY_ID", "")
	c.ApplePrivateKey = strings.ReplaceAll(env("APPLE_PRIVATE_KEY", ""), `\n`, "\n")
	if f := env("APPLE_PRIVATE_KEY_FILE", ""); f != "" && c.ApplePrivateKey == "" {
		if b, err := os.ReadFile(f); err == nil {
			c.ApplePrivateKey = string(b)
		}
	}
	mode := func(key string, has bool) string {
		def := "sandbox"
		if c.Env == "production" {
			def = "off"
		}
		if has {
			def = "on"
		}
		return env(key, def)
	}
	c.OAuthGoogle = mode("OAUTH_GOOGLE", c.GoogleClientID != "")
	// Apple ID пока выключен (нужен платный аккаунт разработчика); включится сам, когда появятся ключи
	c.OAuthApple = env("OAUTH_APPLE", map[bool]string{true: "on", false: "off"}[c.AppleClientID != ""])
	c.TelegramSandbox = envBool("TELEGRAM_SANDBOX", c.TelegramToken == "" && c.Env != "production")
	c.LaunchMode = env("LAUNCH_MODE", "open")
	c.SecureCookies = strings.HasPrefix(c.PublicURL, "https://")
	return c, c.validate()
}

func (c *Config) validate() error {
	switch c.Payments {
	case PaymentsSandbox:
	case PaymentsBePaid:
		if c.BePaidShopID == "" || c.BePaidSecret == "" {
			return errors.New("PAYMENTS=bepaid требует BEPAID_SHOP_ID и BEPAID_SECRET_KEY")
		}
	default:
		return fmt.Errorf("неизвестный PAYMENTS=%q (sandbox|bepaid)", c.Payments)
	}
	switch c.Broker {
	case BrokerPaper:
	case BrokerWhitebird:
		if c.WhitebirdURL == "" || c.WhitebirdKey == "" {
			return errors.New("BROKER=whitebird требует WHITEBIRD_API_URL и WHITEBIRD_API_KEY")
		}
	default:
		return fmt.Errorf("неизвестный BROKER=%q (paper|whitebird)", c.Broker)
	}
	// Защита от главной ошибки: брать реальные деньги и не покупать активы.
	if c.LiveMoney() && c.Broker == BrokerPaper {
		return errors.New("боевые платежи bePaid (BEPAID_TEST=false) нельзя включать с учебным исполнением BROKER=paper")
	}
	switch c.KYCDocs {
	case "sandbox", "manual":
	case "sumsub":
		if c.SumsubToken == "" || c.SumsubSecret == "" || c.SumsubHook == "" {
			return errors.New("KYC_DOCS=sumsub требует SUMSUB_APP_TOKEN, SUMSUB_SECRET_KEY и SUMSUB_WEBHOOK_SECRET")
		}
	default:
		return fmt.Errorf("неизвестный KYC_DOCS=%q (sandbox|sumsub|manual)", c.KYCDocs)
	}
	switch c.MSIMode {
	case "sandbox", "off":
	case "oauth":
		if c.MSIAuthURL == "" || c.MSITokenURL == "" || c.MSIUserURL == "" || c.MSIClientID == "" || c.MSISecret == "" {
			return errors.New("MSI_MODE=oauth требует MSI_AUTH_URL, MSI_TOKEN_URL, MSI_USERINFO_URL, MSI_CLIENT_ID и MSI_CLIENT_SECRET")
		}
	default:
		return fmt.Errorf("неизвестный MSI_MODE=%q (sandbox|oauth|off)", c.MSIMode)
	}
	for name, v := range map[string]string{"OAUTH_GOOGLE": c.OAuthGoogle, "OAUTH_APPLE": c.OAuthApple} {
		if v != "sandbox" && v != "on" && v != "off" {
			return fmt.Errorf("неизвестный %s=%q (sandbox|on|off)", name, v)
		}
		if c.Env == "production" && v == "sandbox" {
			return fmt.Errorf("%s=sandbox недоступен при APP_ENV=production", name)
		}
	}
	if c.OAuthGoogle == "on" && (c.GoogleClientID == "" || c.GoogleClientSecret == "") {
		return errors.New("OAUTH_GOOGLE=on требует GOOGLE_CLIENT_ID и GOOGLE_CLIENT_SECRET")
	}
	if c.OAuthApple == "on" && (c.AppleClientID == "" || c.AppleTeamID == "" || c.AppleKeyID == "" || c.ApplePrivateKey == "") {
		return errors.New("OAUTH_APPLE=on требует APPLE_CLIENT_ID, APPLE_TEAM_ID, APPLE_KEY_ID и APPLE_PRIVATE_KEY(_FILE)")
	}
	if c.Env == "production" && c.TelegramSandbox {
		return errors.New("тестовый Telegram недоступен при APP_ENV=production")
	}
	if c.Env == "production" && (c.KYCDocs == "sandbox" || c.MSIMode == "sandbox") {
		return errors.New("тестовая проверка личности (KYC_DOCS/MSI_MODE=sandbox) недоступна при APP_ENV=production")
	}
	switch c.LaunchMode {
	case "open":
	case "waitlist":
		// до запуска деньги не принимаются: демо-счёт работает на встроенной тестовой платёжке
		if c.Payments != PaymentsSandbox {
			return errors.New("LAUNCH_MODE=waitlist работает только с PAYMENTS=sandbox (демо-счёт на виртуальных деньгах)")
		}
	default:
		return fmt.Errorf("неизвестный LAUNCH_MODE=%q (open|waitlist)", c.LaunchMode)
	}
	if c.Market != MarketLive && c.Market != MarketFixture {
		return fmt.Errorf("неизвестный MARKET=%q (live|fixture)", c.Market)
	}
	if c.Env == "production" && c.Market == MarketFixture {
		return errors.New("MARKET=fixture запрещён в production")
	}
	return nil
}

// LiveMoney — списываются ли реальные деньги.
func (c *Config) LiveMoney() bool { return c.Payments == PaymentsBePaid && !c.BePaidTest }

// Mode — человекочитаемый режим для интерфейса.
func (c *Config) Mode() string {
	switch {
	case c.LiveMoney():
		return "live"
	case c.Payments == PaymentsBePaid:
		return "bepaid_test"
	default:
		return "sandbox"
	}
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envBool(k string, def bool) bool {
	v := strings.ToLower(env(k, ""))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func envInt64(k string, def int64) int64 {
	v := env(k, "")
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

// loadDotEnv выставляет переменные из файла, не перетирая уже заданные.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, `"`) || strings.HasPrefix(v, "'") {
			// значение в кавычках берём целиком, вместе с # внутри
			q := v[:1]
			if end := strings.Index(v[1:], q); end >= 0 {
				v = v[1 : end+1]
			}
		} else if i := strings.Index(v, " #"); i >= 0 {
			// комментарий после значения: PUBLIC_URL=http://localhost:8080  # пояснение
			v = strings.TrimSpace(v[:i])
		} else if i := strings.Index(v, "\t#"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
}
