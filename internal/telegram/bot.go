// Package telegram — бот AutoInvest через официальный Bot API (long polling).
//
// Что умеет:
//   - привязка аккаунта по одноразовой ссылке с сайта (t.me/<бот>?start=<код>);
//   - меню кнопками: баланс, копилки (с паузой и включением), последние операции, настройки уведомлений, поддержка;
//   - вопрос в поддержку одним сообщением — становится обращением на сайте;
//   - сотрудник поддержки отвечает прямо из чата поддержки: ответом (reply) на уведомление о новом обращении;
//   - уведомления о пополнениях, выводах и ответах поддержки (notify.Messenger).
//
// Без токена при разработке бот работает с встроенной имитацией Telegram (sandbox.go).
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"autoinvest/internal/engine"
	"autoinvest/internal/money"
	"autoinvest/internal/notify"
	"autoinvest/internal/store"
)

type Bot struct {
	Token         string
	Username      string
	PublicURL     string
	API           string
	SupportChatID int64 // чат сотрудников: туда приходят новые обращения, ответом на них можно ответить клиенту
	St            *store.Store
	En            *engine.Engine
	Log           *slog.Logger
	http          *http.Client

	mu      sync.Mutex
	waiting map[int64]time.Time // чаты, от которых ждём текст вопроса в поддержку
}

func New(token, username, publicURL string, st *store.Store, en *engine.Engine, log *slog.Logger) *Bot {
	return &Bot{Token: token, Username: username, PublicURL: publicURL, API: "https://api.telegram.org", St: st, En: en, Log: log,
		http: &http.Client{Timeout: 40 * time.Second}, waiting: map[int64]time.Time{}}
}

// UseTransport направляет запросы бота в другой транспорт (тестовый Telegram).
func (b *Bot) UseTransport(t http.RoundTripper) {
	b.http = &http.Client{Timeout: 40 * time.Second, Transport: t}
}

func (b *Bot) call(ctx context.Context, method string, payload any, out any) error {
	buf, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.API+"/bot"+b.Token+"/"+method, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	if !env.OK {
		return errors.New("telegram " + method + ": " + env.Description)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// ---------------------------------------------------------------- журнал переписки

// Каждое сообщение человека и каждый ответ бота пишутся в telegram_log с привязкой к пользователю —
// поддержка видит переписку в админке, человек получает её в выгрузке своих данных.
// Коды для входа и одноразовые ссылки привязки в журнал не попадают.
var (
	codeRe  = regexp.MustCompile(`\b\d{6}\b`)
	tagRe   = regexp.MustCompile(`<[^>]+>`)
	unescHT = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")
)

func maskSecrets(text string) string {
	if strings.HasPrefix(text, "/start ") {
		return "/start •••"
	}
	if strings.Contains(strings.ToLower(text), "код") {
		text = codeRe.ReplaceAllString(text, "••••••")
	}
	return text
}

func plainText(html string) string { return unescHT.Replace(tagRe.ReplaceAllString(html, "")) }

func (b *Bot) userIDByChat(ctx context.Context, chat int64) *int64 {
	if b.St == nil {
		return nil
	}
	if u, err := b.St.UserByTelegram(ctx, chat); err == nil {
		return &u.ID
	}
	return nil
}

func (b *Bot) logOut(chat int64, kind, text string) {
	if b.St == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b.St.LogTelegram(ctx, chat, b.userIDByChat(ctx, chat), "out", kind, maskSecrets(plainText(text)))
}

// buttonLabels — что означают кнопки, чтобы журнал читался без расшифровки кодов.
var buttonLabels = map[string]string{
	"m": "Меню", "b": "Баланс", "p": "Копилки", "pl": "Открыть копилку", "pp": "Поставить копилку на паузу", "pr": "Включить копилку",
	"o": "Операции", "n": "Уведомления", "nt": "Переключить: сообщения в Telegram", "nm": "Переключить: движения рынка",
	"ne": "Переключить: письма на почту", "s": "Написать в поддержку", "sx": "Отмена",
}

func (b *Bot) logIn(ctx context.Context, u update) {
	if b.St == nil {
		return
	}
	switch {
	case u.Callback != nil && u.Callback.Message != nil:
		cmd, arg, _ := strings.Cut(u.Callback.Data, ":")
		label := buttonLabels[cmd]
		if label == "" {
			label = u.Callback.Data
		}
		if arg != "" {
			label += " #" + arg
		}
		chat := u.Callback.Message.Chat.ID
		b.St.LogTelegram(ctx, chat, b.userIDByChat(ctx, chat), "in", "button", "[кнопка] "+label)
	case u.Message != nil:
		chat := u.Message.Chat.ID
		uid := b.userIDByChat(ctx, chat)
		if u.Message.Chat.Type != "private" {
			uid = b.userIDByChat(ctx, u.Message.From.ID) // в группе поддержки — сотрудник, который написал
		}
		kind, text := "text", u.Message.Text
		switch {
		case u.Message.Contact != nil:
			kind, text = "contact", "[поделился номером] "+u.Message.Contact.PhoneNumber
		case strings.HasPrefix(text, "/"):
			kind = "command"
		}
		b.St.LogTelegram(ctx, chat, uid, "in", kind, maskSecrets(text))
	}
}

// ---------------------------------------------------------------- клавиатуры

type btn map[string]string

func cb(text, data string) btn  { return btn{"text": text, "callback_data": data} }
func link(text, url string) btn { return btn{"text": text, "url": url} }
func kb(rows ...[]btn) map[string]any {
	return map[string]any{"inline_keyboard": rows}
}
func row(b ...btn) []btn { return b }

func (b *Bot) site(path string) string { return b.PublicURL + path }

// кнопка со ссылкой: Telegram принимает только http(s), на localhost без домена ссылку отдаём текстом
func (b *Bot) siteBtn(text, path string) []btn {
	if strings.HasPrefix(b.PublicURL, "https://") {
		return row(link(text, b.site(path)))
	}
	return nil
}

func rowsOf(rows ...[]btn) [][]btn {
	out := [][]btn{}
	for _, r := range rows {
		if len(r) > 0 {
			out = append(out, r)
		}
	}
	return out
}

func (b *Bot) mainMenu() map[string]any {
	return map[string]any{"inline_keyboard": rowsOf(
		row(cb("Баланс", "b"), cb("Копилки", "p")),
		row(cb("Операции", "o"), cb("Уведомления", "n")),
		row(cb("Написать в поддержку", "s")),
		b.siteBtn("Открыть AutoInvest", "/app"),
	)}
}

// ---------------------------------------------------------------- отправка

// Send — реализует notify.Messenger.
func (b *Bot) Send(chatID int64, text string) error {
	return b.send(chatID, text, nil)
}

func (b *Bot) send(chatID int64, text string, markup map[string]any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p := map[string]any{"chat_id": chatID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true}
	if markup != nil {
		p["reply_markup"] = markup
	}
	err := b.call(ctx, "sendMessage", p, nil)
	kind := "message"
	if err != nil {
		kind = "error"
	}
	b.logOut(chatID, kind, text)
	return err
}

// edit — меняем сообщение с кнопками на месте (так меню не засоряет чат); если не вышло — новое сообщение.
func (b *Bot) edit(chatID, msgID int64, text string, markup map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p := map[string]any{"chat_id": chatID, "message_id": msgID, "text": text, "parse_mode": "HTML", "disable_web_page_preview": true}
	if markup != nil {
		p["reply_markup"] = markup
	}
	if err := b.call(ctx, "editMessageText", p, nil); err != nil && !strings.Contains(err.Error(), "not modified") {
		b.send(chatID, text, markup)
		return
	}
	b.logOut(chatID, "edit", text)
}

// Check проверяет токен, узнаёт имя бота и ставит список команд.
func (b *Bot) Check(ctx context.Context) error {
	var me struct {
		Username string `json:"username"`
	}
	if err := b.call(ctx, "getMe", map[string]any{}, &me); err != nil {
		return err
	}
	if b.Username == "" {
		b.Username = me.Username
	}
	// профиль бота: описание в пустом чате и короткое «о боте» (аватарку Telegram разрешает ставить только через @BotFather)
	b.call(ctx, "setMyDescription", map[string]any{"description": botDescription, "language_code": ""}, nil)
	b.call(ctx, "setMyShortDescription", map[string]any{"short_description": botAbout, "language_code": ""}, nil)
	b.call(ctx, "setMyCommands", map[string]any{"commands": []map[string]string{
		{"command": "menu", "description": "Главное меню"},
		{"command": "balance", "description": "Баланс и доходность"},
		{"command": "plans", "description": "Мои копилки"},
		{"command": "history", "description": "Последние операции"},
		{"command": "support", "description": "Написать в поддержку"},
		{"command": "phone", "description": "Подтвердить номер для входа"},
		{"command": "stop", "description": "Отключить бота от аккаунта"},
	}}, nil)
	return nil
}

const botAbout = "Копилки AutoInvest в Telegram: баланс, чеки о пополнениях, пауза одной кнопкой и коды для входа."

const botDescription = "Бот сервиса AutoInvest — регулярные накопления в долларе, золоте и криптовалюте.\n\n" +
	"• Баланс и доходность всех копилок\n" +
	"• Чек о каждом пополнении и выводе\n" +
	"• Пауза и включение копилки одной кнопкой\n" +
	"• Коды для входа по номеру телефона\n" +
	"• Вопрос в поддержку прямо из чата\n\n" +
	"Чтобы начать, откройте на сайте Профиль → Уведомления → «Подключить Telegram»."

// ---------------------------------------------------------------- приём апдейтов

type tgUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type tgMessage struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From    tgUser     `json:"from"`
	ReplyTo *tgMessage `json:"reply_to_message"`
	Contact *struct {
		PhoneNumber string `json:"phone_number"`
		UserID      int64  `json:"user_id"`
	} `json:"contact"`
}

type update struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
	Callback *struct {
		ID      string     `json:"id"`
		Data    string     `json:"data"`
		From    tgUser     `json:"from"`
		Message *tgMessage `json:"message"`
	} `json:"callback_query"`
}

// Run — цикл long polling.
func (b *Bot) Run(ctx context.Context) {
	var offset int64
	backoff := time.Second
	var purged time.Time
	for ctx.Err() == nil {
		var ups []update
		err := b.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message", "callback_query"}}, &ups)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.Log.Warn("telegram getUpdates", "err", err)
			time.Sleep(backoff)
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = time.Second
		if time.Since(purged) > 24*time.Hour && b.St != nil {
			b.St.PurgeTelegramLog(ctx, 365) // переписку храним год
			purged = time.Now()
		}
		for _, u := range ups {
			offset = u.UpdateID + 1
			b.Handle(ctx, u)
		}
	}
}

// Handle обрабатывает один апдейт (экспортирован для тестов).
func (b *Bot) Handle(ctx context.Context, u update) {
	defer func() {
		if v := recover(); v != nil {
			b.Log.Error("telegram: паника в обработчике", "err", v)
		}
	}()
	b.logIn(ctx, u)
	switch {
	case u.Callback != nil && u.Callback.Message != nil:
		b.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": u.Callback.ID}, nil)
		b.press(ctx, u.Callback.Message.Chat.ID, u.Callback.Message.MessageID, u.Callback.Data)
	case u.Message != nil && u.Message.Chat.Type == "private" && u.Message.Contact != nil:
		b.onContact(ctx, u.Message)
	case u.Message != nil && u.Message.Chat.Type == "private":
		if b.clientReply(ctx, u.Message) {
			return
		}
		b.handle(ctx, u.Message.Chat.ID, u.Message.From.Username, strings.TrimSpace(u.Message.Text))
	case u.Message != nil && u.Message.Chat.Type != "private" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(u.Message.Text)), "/chatid"):
		// помощь при настройке: узнать ID группы поддержки прямо в Telegram
		note := "Впишите его в .env как TELEGRAM_SUPPORT_CHAT_ID и перезапустите сервис."
		if u.Message.Chat.ID == b.SupportChatID {
			note = "Эта группа уже подключена как чат поддержки."
		}
		b.send(u.Message.Chat.ID, fmt.Sprintf("ID этой группы: <code>%d</code>\n%s", u.Message.Chat.ID, note), nil)
	case u.Message != nil && b.SupportChatID != 0 && u.Message.Chat.ID == b.SupportChatID:
		b.staffReply(ctx, u.Message)
	}
}

func (b *Bot) handle(ctx context.Context, chat int64, username, text string) {
	if text == "" {
		return
	}
	cmd, arg, _ := strings.Cut(text, " ")
	cmd = strings.ToLower(strings.Split(cmd, "@")[0])
	if !strings.HasPrefix(cmd, "/") {
		if b.takeWaiting(chat) {
			b.createTicket(ctx, chat, text)
			return
		}
		cmd = "/menu"
	} else {
		b.takeWaiting(chat) // любая команда отменяет ожидание вопроса
	}
	switch cmd {
	case "/start":
		if arg = strings.TrimSpace(arg); arg != "" {
			b.bind(ctx, chat, username, arg)
			return
		}
		if u, err := b.St.UserByTelegram(ctx, chat); err == nil {
			b.send(chat, "Здравствуйте, "+escape(firstName(u.Name))+"! Что посмотрим?", b.mainMenu())
			return
		}
		b.send(chat, "Это бот AutoInvest — регулярные накопления в долларе, золоте и криптовалюте.\n\n"+
			"Чтобы видеть баланс и получать чеки о пополнениях, подключите аккаунт: на сайте откройте <b>Профиль → Уведомления</b> "+
			"и нажмите «Подключить Telegram».\n"+b.site("/app/profile/notifications"), map[string]any{"inline_keyboard": rowsOf(b.siteBtn("Открыть профиль", "/app/profile/notifications"))})
	case "/menu", "/help":
		if _, ok := b.user(ctx, chat); ok {
			b.send(chat, "Что посмотрим?", b.mainMenu())
		}
	case "/balance":
		if u, ok := b.user(ctx, chat); ok {
			b.send(chat, b.balanceText(ctx, u), kb(row(cb("Копилки", "p"), cb("Меню", "m"))))
		}
	case "/plans":
		if u, ok := b.user(ctx, chat); ok {
			t, m := b.plansView(ctx, u)
			b.send(chat, t, m)
		}
	case "/history":
		if u, ok := b.user(ctx, chat); ok {
			b.send(chat, b.historyText(ctx, u), kb(row(cb("Меню", "m"))))
		}
	case "/phone":
		if _, ok := b.user(ctx, chat); ok {
			b.AskPhone(chat)
		}
	case "/support":
		if _, ok := b.user(ctx, chat); ok {
			b.askSupport(chat, 0)
		}
	case "/stop":
		if u, err := b.St.UserByTelegram(ctx, chat); err == nil {
			b.St.UpdateUserFields(ctx, u.ID, map[string]any{"telegram_chat_id": nil, "telegram_username": ""})
			b.St.Audit(ctx, u.ID, u.ID, "telegram.unlink", "из бота", "")
			b.send(chat, "Telegram отключён от аккаунта. Подключить снова можно в профиле на сайте.", nil)
		}
	default:
		b.send(chat, "Не знаю такой команды. Вот меню:", b.mainMenu())
	}
}

// press — нажатие кнопки. Экран меняется в том же сообщении.
func (b *Bot) press(ctx context.Context, chat, msg int64, data string) {
	u, ok := b.user(ctx, chat)
	if !ok {
		return
	}
	cmd, arg, _ := strings.Cut(data, ":")
	id, _ := strconv.ParseInt(arg, 10, 64)
	switch cmd {
	case "m":
		b.takeWaiting(chat)
		b.edit(chat, msg, "Что посмотрим?", b.mainMenu())
	case "b":
		b.edit(chat, msg, b.balanceText(ctx, u), kb(row(cb("Копилки", "p"), cb("Меню", "m"))))
	case "p":
		t, m := b.plansView(ctx, u)
		b.edit(chat, msg, t, m)
	case "pl":
		t, m := b.planView(ctx, u, id)
		b.edit(chat, msg, t, m)
	case "pp", "pr":
		p, err := b.St.Plan(ctx, u.ID, id)
		if err != nil {
			b.edit(chat, msg, "Копилка не найдена.", kb(row(cb("К копилкам", "p"))))
			return
		}
		if cmd == "pp" {
			err = b.En.PausePlan(ctx, p, "telegram")
		} else {
			err = b.En.ResumePlan(ctx, p, "telegram")
		}
		t, m := b.planView(ctx, u, id)
		if err != nil {
			t = "⚠️ " + escape(err.Error()) + "\n\n" + t
		}
		b.edit(chat, msg, t, m)
	case "o":
		b.edit(chat, msg, b.historyText(ctx, u), kb(row(cb("Меню", "m"))))
	case "n", "nt", "nm", "ne":
		field := map[string]string{"nt": "notify_telegram", "nm": "notify_market", "ne": "notify_email"}[cmd]
		if field != "" {
			cur := map[string]bool{"notify_telegram": u.NotifyTelegram, "notify_market": u.NotifyMarket, "notify_email": u.NotifyEmail}[field]
			b.St.UpdateUserFields(ctx, u.ID, map[string]any{field: !cur})
			u, _ = b.St.UserByID(ctx, u.ID)
		}
		b.edit(chat, msg, "<b>Уведомления</b>\nНажмите, чтобы включить или выключить.", b.notifyMenu(u))
	case "s":
		b.askSupport(chat, msg)
	case "sx":
		b.takeWaiting(chat)
		b.edit(chat, msg, "Хорошо, не отправляем. Что посмотрим?", b.mainMenu())
	default:
		b.edit(chat, msg, "Что посмотрим?", b.mainMenu())
	}
}

func onOff(v bool) string {
	if v {
		return "✅ "
	}
	return "⬜️ "
}

func (b *Bot) notifyMenu(u *store.User) map[string]any {
	return kb(
		row(cb(onOff(u.NotifyTelegram)+"Сообщения в Telegram", "nt")),
		row(cb(onOff(u.NotifyEmail)+"Письма на почту", "ne")),
		row(cb(onOff(u.NotifyMarket)+"Резкие движения рынка", "nm")),
		row(cb("Меню", "m")),
	)
}

// ---------------------------------------------------------------- экраны

func firstName(n string) string {
	if f := strings.Fields(n); len(f) > 0 {
		return f[0]
	}
	return n
}

func signed(k int64) string {
	if k > 0 {
		return "+" + money.FormatBYN(k)
	}
	return money.FormatBYN(k)
}

func (b *Bot) balanceText(ctx context.Context, u *store.User) string {
	plans, _ := b.St.Plans(ctx, u.ID, false)
	var all []store.Position
	for _, p := range plans {
		all = append(all, p.Positions...)
	}
	v := b.En.Value(all)
	if v.InvestedKop == 0 && v.ValueKop == 0 {
		return "<b>Баланс</b>\nПока пусто: первое пополнение появится здесь.\n" + b.site("/app/plans/new")
	}
	return fmt.Sprintf("<b>Баланс: %s</b>\nВложено: %s\nДоход: %s (%+.2f%%)\nЗа сутки: %s",
		money.FormatBYN(v.ValueKop), money.FormatBYN(v.InvestedKop), signed(v.PnLKop), v.PnLPct, signed(v.DayPnLKop))
}

func (b *Bot) plansView(ctx context.Context, u *store.User) (string, map[string]any) {
	plans, _ := b.St.Plans(ctx, u.ID, false)
	if len(plans) == 0 {
		return "Копилок пока нет. Создайте первую на сайте:\n" + b.site("/app/plans/new"), kb(row(cb("Меню", "m")))
	}
	var sb strings.Builder
	sb.WriteString("<b>Копилки</b>\n")
	rows := [][]btn{}
	for _, p := range plans {
		v := b.En.Value(p.Positions)
		state := ""
		if p.Status == "paused" {
			state = " · на паузе"
		}
		fmt.Fprintf(&sb, "\n<b>%s</b> — %s%s", escape(p.Name), money.FormatBYN(v.ValueKop), state)
		rows = append(rows, row(cb(p.Name, "pl:"+strconv.FormatInt(p.ID, 10))))
	}
	rows = append(rows, row(cb("Меню", "m")))
	return sb.String(), map[string]any{"inline_keyboard": rows}
}

func (b *Bot) planView(ctx context.Context, u *store.User, id int64) (string, map[string]any) {
	p, err := b.St.Plan(ctx, u.ID, id)
	if err != nil {
		return "Копилка не найдена.", kb(row(cb("К копилкам", "p")))
	}
	v := b.En.Value(p.Positions)
	var sb strings.Builder
	status := map[string]string{"active": "работает", "paused": "на паузе", "closed": "закрыта"}[p.Status]
	fmt.Fprintf(&sb, "<b>%s</b> · %s\n\nСтоимость: <b>%s</b>\nВложено: %s\nДоход: %s (%+.2f%%)",
		escape(p.Name), status, money.FormatBYN(v.ValueKop), money.FormatBYN(v.InvestedKop), signed(v.PnLKop), v.PnLPct)
	if p.GoalKop != nil && *p.GoalKop > 0 {
		pct := float64(v.ValueKop) / float64(*p.GoalKop) * 100
		fmt.Fprintf(&sb, "\nЦель: %s — %.0f%%", money.FormatBYN(*p.GoalKop), min(pct, 100))
	}
	if p.Status == "active" && p.NextRunAt != nil {
		fmt.Fprintf(&sb, "\n\nСледующее пополнение: %s на %s", time.Unix(*p.NextRunAt, 0).In(engine.Minsk).Format("02.01 в 15:04"), money.FormatBYN(p.AmountKop))
	}
	sid := strconv.FormatInt(p.ID, 10)
	var toggle []btn
	switch p.Status {
	case "active":
		toggle = row(cb("Поставить на паузу", "pp:"+sid))
	case "paused":
		toggle = row(cb("Включить пополнения", "pr:"+sid))
	}
	return sb.String(), map[string]any{"inline_keyboard": rowsOf(toggle, b.siteBtn("Пополнить или вывести на сайте", "/app/plans/"+sid), row(cb("К копилкам", "p"), cb("Меню", "m")))}
}

func (b *Bot) historyText(ctx context.Context, u *store.User) string {
	list, _, _ := b.St.Orders(ctx, store.OrderFilter{UserID: u.ID, Limit: 7})
	if len(list) == 0 {
		return "<b>Операции</b>\nПока ни одной."
	}
	side := map[string]string{"buy": "Пополнение", "sell": "Вывод"}
	st := map[string]string{"filled": "", "failed": " — отклонено", "pending": " — в обработке", "payout_pending": " — ждёт выплаты", "manual": " — на проверке"}
	var sb strings.Builder
	sb.WriteString("<b>Последние операции</b>\n")
	for _, o := range list {
		fmt.Fprintf(&sb, "\n%s · %s «%s» · %s%s", time.Unix(o.CreatedAt, 0).In(engine.Minsk).Format("02.01 15:04"), side[o.Side], escape(o.PlanName), money.FormatBYN(o.AmountKop), st[o.Status])
	}
	return sb.String()
}

// ---------------------------------------------------------------- поддержка

func (b *Bot) askSupport(chat, msg int64) {
	b.mu.Lock()
	b.waiting[chat] = time.Now().Add(15 * time.Minute)
	b.mu.Unlock()
	text := "Напишите вопрос одним сообщением — передадим в поддержку. Ответ придёт сюда и на почту."
	if msg != 0 {
		b.edit(chat, msg, text, kb(row(cb("Отмена", "sx"))))
	} else {
		b.send(chat, text, kb(row(cb("Отмена", "sx"))))
	}
}

func (b *Bot) takeWaiting(chat int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.waiting[chat]
	delete(b.waiting, chat)
	return ok && time.Now().Before(t)
}

func (b *Bot) createTicket(ctx context.Context, chat int64, text string) {
	u, ok := b.user(ctx, chat)
	if !ok {
		return
	}
	if len([]rune(text)) < 10 {
		b.mu.Lock()
		b.waiting[chat] = time.Now().Add(15 * time.Minute)
		b.mu.Unlock()
		b.send(chat, "Опишите вопрос чуть подробнее — хотя бы одним предложением.", kb(row(cb("Отмена", "sx"))))
		return
	}
	if len([]rune(text)) > 4000 {
		text = string([]rune(text)[:4000])
	}
	subject := strings.Fields(text)
	subj := strings.Join(subject, " ")
	if r := []rune(subj); len(r) > 60 {
		subj = string(r[:60]) + "…"
	}
	id, err := b.St.CreateTicket(ctx, u.ID, subj, "other", text)
	if err != nil {
		b.Log.Error("telegram: обращение", "err", err)
		b.send(chat, "Не получилось создать обращение. Напишите на сайте: "+b.site("/app/support"), nil)
		return
	}
	b.St.Audit(ctx, u.ID, u.ID, "support.ticket", "telegram", "")
	b.send(chat, fmt.Sprintf("Обращение №%d отправлено. Ответим здесь же — в рабочие дни с 9:00 до 21:00, обычно в течение часа.\nЧтобы дополнить вопрос, ответьте (reply) на это сообщение.", id), b.mainMenu())
	if b.SupportChatID != 0 {
		b.send(b.SupportChatID, fmt.Sprintf("Новое обращение #%d из Telegram от %s\n<b>%s</b>\n%s\n\nОтветьте на это сообщение (reply), чтобы ответить клиенту.",
			id, escape(u.Email), escape(subj), escape(text)), nil)
	}
}

var (
	ticketRe       = regexp.MustCompile(`обращени[еяюи] #(\d+)`)   // в чате поддержки: «Новое обращение #12»
	clientTicketRe = regexp.MustCompile(`обращени[еяю] №\s*(\d+)`) // у клиента: «Ответ поддержки · обращение №12»
)

// clientReply — клиент ответил (reply) на сообщение бота об обращении: текст добавляется в это обращение.
func (b *Bot) clientReply(ctx context.Context, m *tgMessage) bool {
	if m.ReplyTo == nil || strings.TrimSpace(m.Text) == "" || strings.HasPrefix(m.Text, "/") {
		return false
	}
	mm := clientTicketRe.FindStringSubmatch(strings.ToLower(m.ReplyTo.Text))
	if mm == nil {
		return false
	}
	u, ok := b.user(ctx, m.Chat.ID)
	if !ok {
		return true
	}
	id, _ := strconv.ParseInt(mm[1], 10, 64)
	t, err := b.St.Ticket(ctx, u.ID, id) // только своё обращение
	if err != nil {
		b.send(m.Chat.ID, fmt.Sprintf("Обращение №%d не найдено.", id), nil)
		return true
	}
	text := strings.TrimSpace(m.Text)
	if r := []rune(text); len(r) > 4000 {
		text = string(r[:4000])
	}
	if err := b.St.AddTicketMessage(ctx, t.ID, u.ID, false, text); err != nil {
		b.send(m.Chat.ID, "Не получилось добавить сообщение, попробуйте позже.", nil)
		return true
	}
	b.St.Audit(ctx, u.ID, u.ID, "support.message", fmt.Sprintf("ticket %d (telegram)", t.ID), "")
	b.send(m.Chat.ID, fmt.Sprintf("Добавили в обращение №%d. Ответ придёт сюда же.", t.ID), nil)
	if b.SupportChatID != 0 {
		b.send(b.SupportChatID, fmt.Sprintf("Клиент ответил по обращению #%d (%s)\n<b>%s</b>\n%s\n\nОтветьте на это сообщение (reply), чтобы ответить клиенту.",
			t.ID, escape(u.Email), escape(t.Subject), escape(text)), nil)
	}
	return true
}

// staffReply — сотрудник ответил (reply) на уведомление о новом обращении в чате поддержки.
func (b *Bot) staffReply(ctx context.Context, m *tgMessage) {
	if m.ReplyTo == nil || strings.TrimSpace(m.Text) == "" {
		return
	}
	mm := ticketRe.FindStringSubmatch(strings.ToLower(m.ReplyTo.Text))
	if mm == nil {
		return
	}
	staff, err := b.St.UserByTelegram(ctx, m.From.ID)
	if err != nil || !staff.IsStaff() {
		b.send(m.Chat.ID, "Отвечать клиентам могут только сотрудники, подключившие Telegram в своём профиле AutoInvest.", nil)
		return
	}
	id, _ := strconv.ParseInt(mm[1], 10, 64)
	t, err := b.St.Ticket(ctx, 0, id)
	if err != nil {
		b.send(m.Chat.ID, fmt.Sprintf("Обращение #%d не найдено.", id), nil)
		return
	}
	body := strings.TrimSpace(m.Text)
	if err := b.St.AddTicketMessage(ctx, t.ID, staff.ID, true, body); err != nil {
		b.send(m.Chat.ID, "Не удалось сохранить ответ: "+escape(err.Error()), nil)
		return
	}
	b.St.Audit(ctx, t.UserID, staff.ID, "support.reply", fmt.Sprintf("ticket %d (telegram)", t.ID), "")
	if b.En != nil && b.En.N != nil {
		if full, err := b.St.Ticket(ctx, 0, t.ID); err == nil {
			t = full
		}
		b.En.N.Send(ctx, t.UserID, notify.SupportReply(t, body))
	}
	b.send(m.Chat.ID, fmt.Sprintf("Ответ по обращению #%d отправлен клиенту.", t.ID), nil)
}

// ---------------------------------------------------------------- номер телефона

// AskPhone — кнопка «Поделиться номером»: Telegram отдаёт номер, к которому привязан аккаунт, — подтверждение без SMS.
func (b *Bot) AskPhone(chat int64) error {
	return b.send(chat, "Чтобы входить в AutoInvest по номеру телефона, нажмите кнопку <b>«Поделиться номером»</b> внизу.\n"+
		"Telegram передаст номер, к которому привязан этот аккаунт. Коды для входа будут приходить сюда.",
		map[string]any{"keyboard": [][]map[string]any{{{"text": "📱 Поделиться номером", "request_contact": true}}}, "resize_keyboard": true, "one_time_keyboard": true})
}

func (b *Bot) onContact(ctx context.Context, m *tgMessage) {
	chat := m.Chat.ID
	u, ok := b.user(ctx, chat)
	if !ok {
		return
	}
	removeKB := map[string]any{"remove_keyboard": true}
	// только свой номер: чужой контакт из записной книжки не подтверждает владение
	if m.Contact.UserID != m.From.ID {
		b.send(chat, "Это чужой контакт. Нажмите кнопку «Поделиться номером» — Telegram отправит ваш собственный номер.", nil)
		return
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, m.Contact.PhoneNumber)
	if len(digits) < 10 || len(digits) > 15 {
		b.send(chat, "Не получилось прочитать номер. Попробуйте ещё раз: /phone", removeKB)
		return
	}
	phone := "+" + digits
	if err := b.St.SetVerifiedPhone(ctx, u.ID, phone); err != nil {
		if errors.Is(err, store.ErrPhoneTaken) {
			b.send(chat, "Этот номер уже подтверждён в другом аккаунте AutoInvest.", removeKB)
			return
		}
		b.Log.Error("telegram: номер", "err", err)
		b.send(chat, "Не удалось сохранить номер, попробуйте позже.", removeKB)
		return
	}
	b.St.Audit(ctx, u.ID, u.ID, "security.phone_verified", "telegram", "")
	b.send(chat, "Номер <b>"+phone+"</b> подтверждён. Теперь можно входить по номеру — код придёт сюда, в Telegram.", removeKB)
	b.send(chat, "Что посмотрим?", b.mainMenu())
}

// ---------------------------------------------------------------- привязка

func (b *Bot) user(ctx context.Context, chat int64) (*store.User, bool) {
	u, err := b.St.UserByTelegram(ctx, chat)
	if err != nil {
		b.send(chat, "Аккаунт не подключён. На сайте откройте Профиль → Уведомления и нажмите «Подключить Telegram».\n"+b.site("/app/profile/notifications"), nil)
		return nil, false
	}
	return u, true
}

func (b *Bot) bind(ctx context.Context, chat int64, username, code string) {
	uid, err := b.St.UseTelegramLink(ctx, code)
	if err != nil {
		b.send(chat, "Ссылка устарела. Нажмите «Подключить Telegram» на сайте ещё раз.", nil)
		return
	}
	// один чат — один аккаунт
	b.St.DB.ExecContext(ctx, `UPDATE users SET telegram_chat_id=NULL WHERE telegram_chat_id=? AND id!=?`, chat, uid)
	if len(username) > 64 {
		username = username[:64]
	}
	b.St.UpdateUserFields(ctx, uid, map[string]any{"telegram_chat_id": chat, "telegram_username": username, "notify_telegram": true})
	// сообщения до привязки (/start) тоже относим к этому пользователю
	b.St.DB.ExecContext(ctx, `UPDATE telegram_log SET user_id=? WHERE chat_id=? AND user_id IS NULL AND created_at>?`, uid, chat, store.Now()-600)
	b.St.Audit(ctx, uid, uid, "telegram.link", "@"+username, "")
	u, _ := b.St.UserByID(ctx, uid)
	name := ""
	if u != nil {
		name = ", " + escape(firstName(u.Name))
	}
	b.send(chat, "Готово"+name+"! Аккаунт подключён. Сюда будут приходить чеки о пополнениях и выводах, напоминания и ответы поддержки.", b.mainMenu())
	// номер ещё не подтверждён — сразу предлагаем кнопку: без неё вход по номеру не заработает.
	// Telegram передаёт номер по кнопке, даже если в настройках приватности он скрыт от всех.
	if u != nil && !u.PhoneVerified {
		b.AskPhone(chat)
	}
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
