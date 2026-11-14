package api

import (
	"context"
	"fmt"
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
	s.N.SetMessenger(bot)

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

	// телефон: сразу после подключения бот сам присылает кнопку «Поделиться номером» (номер ещё не подтверждён);
	// её же можно запросить из профиля. Номер подтверждается, коды входа приходят в бот
	waitBot(t, c, "Поделиться номером")
	if code := c.do("POST", "/api/v1/me/phone/telegram", map[string]any{}, nil); code != 200 {
		t.Fatalf("запрос номера: %d", code)
	}
	waitBot(t, c, "Поделиться номером")
	c.do("POST", "/api/v1/telegram/sandbox/contact", map[string]any{"phone": "+375 29 111-22-33"}, nil)
	waitBot(t, c, "подтверждён")
	anon := newClient(t, srv.URL)
	// опечатки в номере не замораживают вход: лимит считает только отправленные коды
	for i := 0; i < 5; i++ {
		if code := anon.do("POST", "/api/v1/auth/phone/code", map[string]any{"phone": "+375291110000"}, nil); code == 429 || code == 200 {
			t.Fatalf("неизвестный номер, попытка %d: %d", i+1, code)
		}
	}
	var sent struct {
		TestCode string `json:"test_code"`
	}
	if code := anon.do("POST", "/api/v1/auth/phone/code", map[string]any{"phone": "+375291112233"}, &sent); code != 200 || len(sent.TestCode) != 6 {
		t.Fatalf("код входа: %d %+v", code, sent)
	}
	waitBot(t, c, "Код для входа в AutoInvest: <b>"+sent.TestCode)
	if code := anon.do("POST", "/api/v1/auth/phone/login", map[string]any{"phone": "+375291112233", "code": sent.TestCode}, nil); code != 200 {
		t.Fatalf("вход по коду: %d", code)
	}
	if code := newClient(t, srv.URL).do("POST", "/api/v1/auth/phone/login", map[string]any{"phone": "+375291112233", "code": sent.TestCode}, nil); code == 200 {
		t.Fatal("код сработал дважды")
	}
	if code := newClient(t, srv.URL).do("POST", "/api/v1/auth/login", map[string]any{"email": "+375 (29) 111-22-33", "password": "parol2026"}, nil); code != 200 {
		t.Fatalf("вход по номеру и паролю: %d", code)
	}

	// журнал переписки: всё по пользователю, коды и ссылка привязки замаскированы
	me2, _ := s.St.UserByEmail(context.Background(), "tg@test.by")
	log, _ := s.St.TelegramLog(context.Background(), me2.ID, 500)
	var all strings.Builder
	for _, e := range log {
		all.WriteString(e.Dir + ":" + e.Text + "\n")
	}
	for _, want := range []string{"in:/start •••", "in:[кнопка] Копилки", "in:[кнопка] Поставить копилку на паузу #" + id, "out:Готово", "in:[поделился номером]"} {
		if !strings.Contains(all.String(), want) {
			t.Fatalf("в журнале нет %q:\n%s", want, all.String())
		}
	}
	if strings.Contains(all.String(), sent.TestCode) || strings.Contains(all.String(), code) {
		t.Fatal("код входа или ссылка привязки попали в журнал")
	}

	// поддержка в боте: тема (кнопки) → коротко → подробно → обращение; в группу — подробная карточка с кнопками
	const group = -100777
	bot.SupportChatID = group
	press := func(msg int64, data string) {
		t.Helper()
		if code := c.do("POST", "/api/v1/telegram/sandbox/press", map[string]any{"message_id": msg, "data": data}, nil); code != 200 {
			t.Fatalf("кнопка %s: %d", data, code)
		}
	}
	c.do("POST", "/api/v1/telegram/sandbox/send", map[string]any{"text": "/support"}, nil)
	sm := waitBot(t, c, "Поддержка AutoInvest")
	press(sm.ID, "sn")
	waitBot(t, c, "О чём вопрос")
	press(sm.ID, "sc:withdraw")
	waitBot(t, c, "Напишите тему")
	c.do("POST", "/api/v1/telegram/sandbox/send", map[string]any{"text": "Когда придут деньги"}, nil)
	waitBot(t, c, "опишите вопрос подробно")
	c.do("POST", "/api/v1/telegram/sandbox/send", map[string]any{"text": "Вывел вчера на карту, а денег пока нет. Сколько обычно ждать?"}, nil)
	waitBot(t, c, "отправлено")
	var tickets struct{ Tickets []store.Ticket }
	c.do("GET", "/api/v1/support/tickets", nil, &tickets)
	if len(tickets.Tickets) != 1 || tickets.Tickets[0].Subject != "Когда придут деньги" || tickets.Tickets[0].Category != "withdraw" || tickets.Tickets[0].Source != "telegram" {
		t.Fatalf("обращение: %+v", tickets.Tickets)
	}
	tid := tickets.Tickets[0].ID
	groupMsg := func(want string) telegram.SandboxMsg {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			msgs, _ := tg.Messages(group, 0)
			for i := len(msgs) - 1; i >= 0; i-- {
				if strings.Contains(msgs[i].Text, want) {
					return msgs[i]
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("в группе нет «%s»", want)
		return telegram.SandboxMsg{}
	}
	card := groupMsg(fmt.Sprintf("Новое обращение #%d", tid))
	for _, want := range []string{"из Telegram", "Когда придут деньги", "Вывод денег", "tg@test.by", "Обращений всего: 1", "Сколько обычно ждать"} {
		if !strings.Contains(card.Text, want) {
			t.Fatalf("в карточке группы нет %q:\n%s", want, card.Text)
		}
	}

	// ответ поддержки приходит подробно: номер, тема, вопрос, ответ
	staff := newClient(t, srv.URL)
	staff.register("staff@test.by")
	su, _ := s.St.UserByEmail(context.Background(), "staff@test.by")
	s.St.UpdateUserFields(context.Background(), su.ID, map[string]any{"role": "support"})
	if code := staff.do("POST", fmt.Sprintf("/api/v1/admin/tickets/%d/reply", tid), map[string]any{"body": "Обычно в течение дня"}, nil); code != 200 {
		t.Fatalf("ответ поддержки: %d", code)
	}
	reply := waitBot(t, c, fmt.Sprintf("обращение №%d", tid))
	for _, want := range []string{"Ваш вопрос", "Сколько обычно ждать", "Обычно в течение дня", "«Ответить»"} {
		if !strings.Contains(reply.Text, want) {
			t.Fatalf("в ответе нет %q:\n%s", want, reply.Text)
		}
	}
	if strings.Contains(reply.Text, "Открыть в AutoInvest") {
		t.Fatal("ссылка на localhost не должна показываться в Telegram")
	}
	// клиент отвечает на это сообщение — текст попадает в то же обращение
	if !tg.UserReplies(9_000_000_000+me2.ID, "x", "А если банк задержит?", reply.ID) {
		t.Fatal("не удалось ответить")
	}
	waitBot(t, c, fmt.Sprintf("Добавили в обращение №%d", tid))
	var full struct{ Ticket store.Ticket }
	c.do("GET", fmt.Sprintf("/api/v1/support/tickets/%d", tid), nil, &full)
	last := full.Ticket.Messages[len(full.Ticket.Messages)-1]
	if last.Body != "А если банк задержит?" || last.IsStaff || full.Ticket.Status != "open" {
		t.Fatalf("сообщение клиента: %+v статус %s", last, full.Ticket.Status)
	}
	groupMsg(fmt.Sprintf("Клиент ответил по обращению #%d", tid))

	// сотрудник в группе: «История клиента» и «Закрыть» (только с подключённым Telegram)
	card = groupMsg(fmt.Sprintf("Клиент ответил по обращению #%d", tid))
	const staffTG = 555000111
	s.St.UpdateUserFields(context.Background(), su.ID, map[string]any{"telegram_chat_id": staffTG})
	if !tg.StaffPress(group, card.ID, fmt.Sprintf("gh:%d", me2.ID), staffTG) {
		t.Fatal("нет кнопки «История клиента»")
	}
	hist := groupMsg("История:")
	for _, want := range []string{"Когда придут деньги", "Обычно в течение дня", "А если банк задержит?"} {
		if !strings.Contains(hist.Text, want) {
			t.Fatalf("в истории нет %q:\n%s", want, hist.Text)
		}
	}
	if !tg.StaffPress(group, card.ID, fmt.Sprintf("gz:%d", tid), staffTG) {
		t.Fatal("нет кнопки «Закрыть»")
	}
	groupMsg(fmt.Sprintf("Обращение #%d закрыто", tid))
	closed := waitBot(t, c, fmt.Sprintf("Обращение №%d закрыто", tid))
	c.do("GET", fmt.Sprintf("/api/v1/support/tickets/%d", tid), nil, &full)
	if full.Ticket.Status != "closed" || full.Ticket.ClosedBy != "staff" {
		t.Fatalf("после закрытия сотрудником: %s %s", full.Ticket.Status, full.Ticket.ClosedBy)
	}
	// клиент пишет снова кнопкой «Ответить» — обращение открывается
	press(closed.ID, fmt.Sprintf("sr:%d", tid))
	waitBot(t, c, fmt.Sprintf("Напишите сообщение по обращению №%d", tid))
	c.do("POST", "/api/v1/telegram/sandbox/send", map[string]any{"text": "Деньги пришли, спасибо!"}, nil)
	waitBot(t, c, "снова открыто")
	// и сам закрывает его из «Мои обращения»
	c.do("POST", "/api/v1/telegram/sandbox/send", map[string]any{"text": "/support"}, nil)
	sm = waitBot(t, c, "Поддержка AutoInvest")
	press(sm.ID, "sl")
	waitBot(t, c, "Мои обращения")
	press(sm.ID, fmt.Sprintf("st:%d", tid))
	waitBot(t, c, "Деньги пришли, спасибо!")
	press(sm.ID, fmt.Sprintf("tz:%d", tid))
	waitBot(t, c, "закрыто. Спасибо")
	groupMsg(fmt.Sprintf("Клиент закрыл обращение #%d", tid))
	c.do("GET", fmt.Sprintf("/api/v1/support/tickets/%d", tid), nil, &full)
	if full.Ticket.Status != "closed" || full.Ticket.ClosedBy != "client" {
		t.Fatalf("после закрытия клиентом: %s %s", full.Ticket.Status, full.Ticket.ClosedBy)
	}
	// админка видит всю переписку клиента
	var hs struct{ Tickets []store.Ticket }
	staff.do("GET", fmt.Sprintf("/api/v1/admin/users/%d/support", me2.ID), nil, &hs)
	if len(hs.Tickets) != 1 || len(hs.Tickets[0].Messages) != 4 {
		t.Fatalf("история в админке: %+v", hs.Tickets)
	}
}
