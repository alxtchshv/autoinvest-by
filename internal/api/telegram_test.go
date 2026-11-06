package api

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"autoinvest/internal/store"
	"autoinvest/internal/telegram"
)

type tgView struct {
	Messages []telegram.SandboxMsg `json:"messages"`
	Rev      int64                 `json:"rev"`
}

// waitBot ждёт сообщение бота, в тексте которого есть want; возвращает его.
func waitBot(t *testing.T, c *client, want string) telegram.SandboxMsg {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var v tgView
		c.do("GET", "/api/v1/telegram/sandbox?after=0", nil, &v)
		for i := len(v.Messages) - 1; i >= 0; i-- {
			if m := v.Messages[i]; m.FromBot && strings.Contains(m.Text, want) {
				return m
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("бот не ответил «%s»", want)
	return telegram.SandboxMsg{}
}

func TestTelegramBotInSandbox(t *testing.T) {
	srv, s := newServer(t)
	tg := telegram.NewSandbox("t_bot")
	bot := telegram.New("sandbox", "t_bot", srv.URL, s.St, s.En, slog.New(slog.NewTextHandler(io.Discard, nil)))
	bot.API = "http://telegram.sandbox"
	bot.UseTransport(tg.Transport())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := bot.Check(ctx); err != nil {
		t.Fatal(err)
	}
	go bot.Run(ctx)
	s.Bot, s.TG = bot, tg

	c := newClient(t, srv.URL)
	c.register("tg@test.by")
	c.bindCard()
	planID := c.plan("На море", true)

	var l struct{ URL string }
	c.do("POST", "/api/v1/telegram/link", map[string]any{}, &l)
	code := strings.TrimPrefix(l.URL, "/telegram-sandbox?start=")
	if code == "" || code == l.URL {
		t.Fatalf("ссылка: %q", l.URL)
	}
	c.do("POST", "/api/v1/telegram/sandbox/send", map[string]any{"text": "/start " + code}, nil)
	menu := waitBot(t, c, "Аккаунт подключён")
	var me struct {
		TelegramLinked bool `json:"telegram_linked"`
	}
	c.do("GET", "/api/v1/auth/me", nil, &me)
	if !me.TelegramLinked {
		t.Fatal("аккаунт не привязался")
	}

	// Копилки → карточка копилки → пауза
	c.do("POST", "/api/v1/telegram/sandbox/press", map[string]any{"message_id": menu.ID, "data": "p"}, nil)
	waitBot(t, c, "На море")
	id := strconv.FormatInt(planID, 10)
	c.do("POST", "/api/v1/telegram/sandbox/press", map[string]any{"message_id": menu.ID, "data": "pl:" + id}, nil)
	waitBot(t, c, "Стоимость")
	if code := c.do("POST", "/api/v1/telegram/sandbox/press", map[string]any{"message_id": menu.ID, "data": "pp:" + id}, nil); code != 200 {
		t.Fatalf("кнопка паузы: %d", code)
	}
	waitBot(t, c, "на паузе")
	var p struct{ Plan store.Plan }
	c.do("GET", "/api/v1/plans/"+id, nil, &p)
	if p.Plan.Status != "paused" {
		t.Fatalf("копилка не на паузе: %s", p.Plan.Status)
	}
	// нажать кнопку, которой нет под сообщением, нельзя
	if code := c.do("POST", "/api/v1/telegram/sandbox/press", map[string]any{"message_id": menu.ID, "data": "pp:99999"}, nil); code != 400 {
		t.Fatalf("чужая кнопка: %d", code)
	}

	// вопрос в поддержку одним сообщением → обращение на сайте
	c.do("POST", "/api/v1/telegram/sandbox/send", map[string]any{"text": "/support"}, nil)
	waitBot(t, c, "Напишите вопрос")
	c.do("POST", "/api/v1/telegram/sandbox/send", map[string]any{"text": "Когда придут деньги после вывода на карту?"}, nil)
	waitBot(t, c, "отправлено")
	var tickets struct{ Tickets []store.Ticket }
	c.do("GET", "/api/v1/support/tickets", nil, &tickets)
	if len(tickets.Tickets) != 1 || !strings.Contains(tickets.Tickets[0].Subject, "Когда придут деньги") {
		t.Fatalf("обращение: %+v", tickets.Tickets)
	}
}
