// AutoInvest — сервис регулярных автоинвестиций для Беларуси.
//
//	autoinvest                         запустить сервер
//	autoinvest admin <email> <пароль>  создать администратора (или выдать права существующему)
//	autoinvest demo                    создать демо-пользователя (только тестовый режим платежей)
//	autoinvest backup <файл>           согласованная копия базы без остановки сервиса
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"autoinvest/internal/api"
	"autoinvest/internal/auth"
	"autoinvest/internal/config"
	"autoinvest/internal/db"
	"autoinvest/internal/engine"
	"autoinvest/internal/market"
	"autoinvest/internal/notify"
	"autoinvest/internal/payments"
	"autoinvest/internal/store"
	"autoinvest/internal/telegram"
	"autoinvest/web"
)

var version = "1.0.0"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка конфигурации:", err)
		os.Exit(2)
	}
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	var log *slog.Logger
	if cfg.Env == "production" {
		log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	} else {
		log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	}

	d, err := db.Open(cfg.DataDir)
	if err != nil {
		log.Error("база данных", "err", err)
		os.Exit(1)
	}
	defer d.Close()
	st := store.New(d)
	if err := loadDataKey(cfg); err != nil {
		log.Error("ключ шифрования", "err", err)
		os.Exit(1)
	}
	if err := st.EncryptLegacy(context.Background()); err != nil {
		log.Error("шифрование старых данных", "err", err)
	}

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "admin":
			if len(os.Args) != 4 {
				fmt.Println("использование: autoinvest admin <email> <пароль>")
				os.Exit(2)
			}
			if err := ensureAdmin(st, os.Args[2], os.Args[3]); err != nil {
				fmt.Println("Ошибка:", err)
				os.Exit(1)
			}
			fmt.Println("Администратор готов:", os.Args[2])
			return
		case "backup":
			if len(os.Args) != 3 {
				fmt.Println("использование: autoinvest backup <файл>")
				os.Exit(2)
			}
			if err := db.Backup(context.Background(), d, os.Args[2]); err != nil {
				fmt.Println("Ошибка:", err)
				os.Exit(1)
			}
			fmt.Println("Копия базы:", os.Args[2])
			return
		case "demo":
			// обрабатывается ниже, когда поднят рынок
		default:
			fmt.Println("неизвестная команда:", os.Args[1])
			os.Exit(2)
		}
	}

	if dups := st.BackfillEmailCanon(context.Background()); len(dups) > 0 {
		log.Warn("один ящик — несколько старых аккаунтов (оставлены как есть, разберите в админке)", "emails", dups)
	}
	if cfg.AdminEmail != "" && cfg.AdminPassword != "" {
		// из переменных окружения администратор только создаётся; существующий аккаунт не повышается,
		// иначе любой, кто первым зарегистрировал этот email, стал бы админом
		if u, err := st.UserByEmail(context.Background(), cfg.AdminEmail); err == nil {
			if u.Role != "admin" {
				log.Error("ADMIN_EMAIL занят обычным пользователем — права не выданы. Используйте: autoinvest admin <email> <пароль>", "email", cfg.AdminEmail)
			}
		} else if err := ensureAdmin(st, cfg.AdminEmail, cfg.AdminPassword); err != nil {
			log.Error("ADMIN_EMAIL: администратор не создан", "err", err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var hub *market.Hub
	if cfg.Market == config.MarketFixture {
		log.Warn("MARKET=fixture: ДЕМО-котировки для разработки без интернета. Не используйте с реальными пользователями")
		hub = market.NewFixtureHub(cfg.SpreadBP, log)
	} else {
		hub = market.NewLiveHub(cfg.SpreadBP, log)
		hub.SetRatePersister(st)
	}
	hub.Start(ctx)

	var pay payments.Provider
	var sandbox *payments.Sandbox
	if cfg.Payments == config.PaymentsBePaid {
		pay = payments.NewBePaid(cfg.BePaidShopID, cfg.BePaidSecret, cfg.BePaidTest, cfg.BePaidPublicKey)
	} else {
		sandbox = payments.NewSandbox(cfg.PublicURL)
		pay = sandbox
	}
	var broker engine.Broker = engine.PaperBroker{Hub: hub}
	if cfg.Broker == config.BrokerWhitebird {
		broker = engine.WhitebirdBroker{URL: cfg.WhitebirdURL, Key: cfg.WhitebirdKey, Secret: cfg.WhitebirdSecret}
	}

	mailer := notify.NewMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom, log)
	n := notify.New(st, mailer, cfg.PublicURL, log)
	en := &engine.Engine{St: st, DB: d, Hub: hub, Pay: pay, Broker: broker, N: n, Cfg: cfg, Log: log}

	if len(os.Args) > 1 && os.Args[1] == "demo" {
		if err := seedDemo(ctx, cfg, st, en, sandbox); err != nil {
			fmt.Println("Ошибка:", err)
			os.Exit(1)
		}
		return
	}

	var bot *telegram.Bot
	var tgSandbox *telegram.Sandbox
	switch {
	case cfg.TelegramToken != "":
		bot = telegram.New(cfg.TelegramToken, cfg.TelegramUsername, cfg.PublicURL, st, en, log)
	case cfg.TelegramSandbox:
		// без токена при разработке — встроенный тестовый Telegram: бот настоящий, «сервер Telegram» имитируется
		tgSandbox = telegram.NewSandbox("autoinvest_test_bot")
		bot = telegram.New("sandbox", tgSandbox.Username, cfg.PublicURL, st, en, log)
		bot.API = "http://telegram.sandbox"
		bot.UseTransport(tgSandbox.Transport())
	}
	if bot != nil {
		bot.SupportChatID = cfg.SupportChatID
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := bot.Check(cctx)
		cancel()
		switch {
		case err == nil:
			n.SetMessenger(bot)
			go bot.Run(ctx)
			log.Info("Telegram-бот запущен", "username", bot.Username, "test", tgSandbox != nil)
		case tgSandbox == nil && bot.Username != "":
			// Telegram на минуту недоступен (сеть, VPN) — не выключаем бота до перезапуска, а пробуем снова каждые 30 секунд.
			// Ссылки «Подключить Telegram» работают сразу: Telegram сохранит /start и отдаст боту, когда тот подключится.
			log.Warn("Telegram недоступен, повторю подключение каждые 30 с", "err", err)
			go func(b *telegram.Bot) {
				for {
					select {
					case <-ctx.Done():
						return
					case <-time.After(30 * time.Second):
					}
					c, cancel := context.WithTimeout(ctx, 10*time.Second)
					err := b.Check(c)
					cancel()
					if err == nil {
						n.SetMessenger(b)
						go b.Run(ctx)
						log.Info("Telegram-бот запущен (со второй попытки)", "username", b.Username)
						return
					}
				}
			}(bot)
		default:
			log.Warn("Telegram-бот не запущен", "err", err, "hint", "укажите TELEGRAM_BOT_USERNAME в .env — тогда бот будет переподключаться сам")
			bot, tgSandbox = nil, nil
		}
	}

	go en.Run(ctx)

	srv := &api.Server{Cfg: cfg, St: st, DB: d, Hub: hub, En: en, Pay: pay, Sandbox: sandbox, N: n, Bot: bot, TG: tgSandbox, Log: log,
		Static: web.Dist(), Version: version, Started: time.Now()}
	go srv.WarmMarket(ctx)
	if bot != nil {
		bot.OnSupport = srv.NotifyStaff // обращения из бота — в колокольчик админу и поддержке
	}
	if cfg.Market != "fixture" {
		go srv.WarmShowcase(ctx)
	}
	httpSrv := &http.Server{Addr: cfg.Addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}

	// Порт занимаем до сообщения «запущен»: если он занят (обычно — уже работающей копией), говорим об этом прямо.
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			fmt.Fprintf(os.Stderr, "\nПорт %s уже занят. Скорее всего, AutoInvest уже запущен — откройте %s.\n"+
				"Остановить прежнюю копию: закройте её окно или Ctrl+C (тестовый стенд: make test-stand-stop). Другой порт: ADDR=:8091 PUBLIC_URL=http://localhost:8091\n\n",
				cfg.Addr, cfg.PublicURL)
			os.Exit(1)
		}
		log.Error("http", "err", err)
		os.Exit(1)
	}
	go func() {
		log.Info("AutoInvest запущен", "addr", cfg.Addr, "url", cfg.PublicURL, "payments", cfg.Mode(), "broker", broker.Name(), "market", cfg.Market)
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http", "err", err)
			stop()
		}
	}()
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpSrv.Shutdown(sctx)
	log.Info("остановлен")
}

func ensureAdmin(st *store.Store, email, password string) error {
	ctx := context.Background()
	email = strings.ToLower(strings.TrimSpace(email))
	if err := auth.ValidatePassword(password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	if u, err := st.UserByEmail(ctx, email); err == nil {
		if u.Role == "admin" {
			return nil
		}
		return st.UpdateUserFields(ctx, u.ID, map[string]any{"role": "admin"})
	}
	_, err = st.CreateUser(ctx, store.NewUser{Email: email, Name: "Администратор", PasswordHash: hash, Role: "admin", EmailVerified: true})
	return err
}

// seedDemo — демо-аккаунт с тестовой картой и двумя копилками, пополненными по текущим рыночным ценам.
func seedDemo(ctx context.Context, cfg *config.Config, st *store.Store, en *engine.Engine, sb *payments.Sandbox) error {
	if sb == nil {
		return errors.New("демо-данные создаются только в тестовом режиме платежей (PAYMENTS=sandbox)")
	}
	const email, pass = "demo@autoinvest.by", "demo12345"
	if _, err := st.UserByEmail(ctx, email); err == nil {
		fmt.Println("Демо-пользователь уже есть:", email, "/", pass)
		return nil
	}
	hash, _ := auth.HashPassword(pass)
	u, err := st.CreateUser(ctx, store.NewUser{Email: email, Name: "Алексей Демо", Phone: "+375291234567", PasswordHash: hash, EmailVerified: true})
	if err != nil {
		return err
	}
	c, _ := sb.Tokenize("4200000000000000", "ALEKSEI DEMO", 12, time.Now().Year()+3)
	card, err := st.AddCard(ctx, store.Card{UserID: u.ID, Provider: "sandbox", Token: c.Token, Brand: c.Brand, Last4: c.Last4, ExpMonth: c.ExpMonth, ExpYear: c.ExpYear, Holder: c.Holder, Issuer: c.Issuer, BIN: c.BIN})
	if err != nil {
		return err
	}
	// ждём первые котировки
	for i := 0; i < 30 && len(en.Hub.Snapshot().Quotes) < 3; i++ {
		time.Sleep(time.Second)
	}
	goal := int64(450000)
	gdate := time.Now().AddDate(1, 2, 0).Format("2006-01-02")
	plans := []*store.Plan{
		{UserID: u.ID, Name: "Подушка в долларах", Icon: "shield", Strategy: "reserve", AmountKop: 2000, Frequency: "weekly", Weekday: 5, RunMinute: 600, Status: "active", CardID: &card.ID, DipThresholdBP: 500, DipMultiplierPct: 100,
			Allocations: []store.Alloc{{Asset: "USDT", WeightBP: 6000}, {Asset: "PAXG", WeightBP: 4000}}},
		{UserID: u.ID, Name: "На ноутбук", Icon: "laptop", Strategy: "balanced", AmountKop: 5000, Frequency: "monthly", Monthday: 10, RunMinute: 720, Status: "active", CardID: &card.ID, GoalKop: &goal, GoalDate: &gdate, DipThresholdBP: 500, DipMultiplierPct: 100,
			Allocations: []store.Alloc{{Asset: "USDT", WeightBP: 5000}, {Asset: "BTC", WeightBP: 3000}, {Asset: "PAXG", WeightBP: 2000}}},
	}
	for _, p := range plans {
		p.NextRunAt = engine.NextRun(p, time.Now(), false)
		if err := st.CreatePlan(ctx, p); err != nil {
			return err
		}
		if _, err := en.Buy(ctx, p.ID, p.AmountKop*3, "manual", fmt.Sprintf("demo_%d", p.ID)); err != nil {
			fmt.Println("Пополнение не прошло:", err)
		}
	}
	fmt.Println("Демо-пользователь создан:", email, "/", pass)
	_ = cfg
	return nil
}

// loadDataKey — ключ шифрования полей базы: DATA_KEY из окружения (на сервере обязателен),
// при разработке — файл data.key в DATA_DIR, создаётся сам. Ключ в базу не попадает.
func loadDataKey(cfg *config.Config) error {
	raw := strings.TrimSpace(cfg.DataKey)
	if raw == "" {
		path := filepath.Join(cfg.DataDir, "data.key")
		if b, err := os.ReadFile(path); err == nil {
			raw = strings.TrimSpace(string(b))
		} else {
			k := make([]byte, 32)
			if _, err := rand.Read(k); err != nil {
				return err
			}
			raw = base64.StdEncoding.EncodeToString(k)
			if err := os.WriteFile(path, []byte(raw+"\n"), 0o600); err != nil {
				return err
			}
		}
	}
	k, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return errors.New("DATA_KEY: не base64 (сгенерируйте: openssl rand -base64 32)")
	}
	return store.SetDataKey(k)
}
