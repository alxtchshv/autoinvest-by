package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDotEnvStripsInlineComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "TEST_DOTENV_A=http://localhost:8080      # внешний адрес\nTEST_DOTENV_B=\"pass # with hash\"\nTEST_DOTENV_C=plain\n# комментарий\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"TEST_DOTENV_A", "TEST_DOTENV_B", "TEST_DOTENV_C"} {
		os.Unsetenv(k)
		t.Cleanup(func() { os.Unsetenv(k) })
	}
	loadDotEnv(path)
	want := map[string]string{"TEST_DOTENV_A": "http://localhost:8080", "TEST_DOTENV_B": "pass # with hash", "TEST_DOTENV_C": "plain"}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

// Боевой сервер не поднимется с опасной настройкой: http-адрес, тестовая платёжка без листа ожидания, короткий пароль админа.
func TestProductionGuards(t *testing.T) {
	base := func() Config {
		return Config{Env: "production", PublicURL: "https://autoinvest.by", Payments: PaymentsSandbox, Broker: BrokerPaper,
			KYCDocs: "manual", MSIMode: "off", OAuthGoogle: "off", OAuthApple: "off", Market: "live", LaunchMode: "waitlist",
			AdminPassword: "dlinnyi-parol-2026"}
	}
	ok := base()
	if err := ok.validate(); err != nil {
		t.Fatalf("правильная боевая настройка не прошла: %v", err)
	}
	for name, bad := range map[string]func(*Config){
		"http":         func(c *Config) { c.PublicURL = "http://autoinvest.by" },
		"sandbox open": func(c *Config) { c.LaunchMode = "open" },
		"short admin":  func(c *Config) { c.AdminPassword = "short-pass1" },
	} {
		c := base()
		bad(&c)
		if c.validate() == nil {
			t.Errorf("%s: опасная настройка принята", name)
		}
	}
}
