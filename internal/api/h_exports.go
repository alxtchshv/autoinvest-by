package api

// Выгрузки для админки файлами (CSV для Excel): по пользователю — операции, переписка в Telegram, журнал действий;
// и все операции сервиса за период. Длинные списки на экране не держим: смотреть удобнее в Excel, а каждая
// выгрузка сотрудником пишется в журнал пользователя.

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"autoinvest/internal/engine"
	"autoinvest/internal/money"
	"autoinvest/internal/store"
)

func csvStart(w http.ResponseWriter, name string) *csv.Writer {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write([]byte("\xEF\xBB\xBF")) // BOM, чтобы Excel открыл кириллицу
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	return cw
}

func minskTime(ts int64) string {
	return time.Unix(ts, 0).In(engine.Minsk).Format("02.01.2006 15:04:05")
}

func writeOrdersCSV(cw *csv.Writer, list []*store.Order, withUser bool) {
	head := []string{"№", "Дата", "Копилка", "Операция", "Статус", "Сумма, руб.", "Комиссия, руб.", "Активы", "Курс USD НБРБ", "Карта", "Исполнение", "Ошибка"}
	if withUser {
		head = append([]string{"Клиент"}, head...)
	}
	cw.Write(head)
	side := map[string]string{"buy": "Пополнение", "sell": "Вывод"}
	status := map[string]string{"filled": "Исполнено", "failed": "Отклонено", "pending": "В обработке", "payout_pending": "Ожидает выплаты", "manual": "На проверке"}
	for _, o := range list {
		var parts []string
		for _, f := range o.Fills {
			parts = append(parts, fmt.Sprintf("%s %.8f по %.2f руб.", f.Asset, money.QtyToFloat(f.Qty), f.PriceBYN))
		}
		row := []string{strconv.FormatInt(o.ID, 10), minskTime(o.CreatedAt), csvSafe(o.PlanName), side[o.Side], status[o.Status],
			strings.ReplaceAll(money.FormatKop(o.AmountKop), " ", ""), strings.ReplaceAll(money.FormatKop(o.FeeKop), " ", ""), strings.Join(parts, ", "),
			strconv.FormatFloat(o.USDBYN, 'f', 4, 64), o.CardLast4, venueLabel(o.Broker), csvSafe(o.Error)}
		if withUser {
			row = append([]string{o.UserEmail}, row...)
		}
		cw.Write(row)
	}
	cw.Flush()
}

func writeAuditCSV(cw *csv.Writer, list []store.AuditEntry) {
	cw.Write([]string{"Дата и время (Минск)", "Событие", "Код события", "Подробности", "IP-адрес", "Устройство"})
	for _, e := range list {
		label := auditLabels[e.Action]
		if label == "" {
			label = e.Action
		}
		ua := ""
		if e.UserAgent != "" {
			ua = shortUA(e.UserAgent)
		}
		cw.Write([]string{minskTime(e.CreatedAt), label, e.Action, csvSafe(e.Details), e.IP, ua})
	}
	cw.Flush()
}

func writeTelegramCSV(cw *csv.Writer, list []store.TelegramLogEntry) {
	cw.Write([]string{"Дата и время (Минск)", "Кто", "Тип", "Текст", "Чат"})
	kind := map[string]string{"text": "сообщение", "command": "команда", "button": "кнопка", "contact": "номер телефона", "message": "сообщение", "edit": "обновил сообщение", "error": "не доставлено"}
	// в файле — по порядку, как читается переписка
	for i := len(list) - 1; i >= 0; i-- {
		e := list[i]
		who := "клиент"
		if e.Dir == "out" {
			who = "бот"
		}
		k := kind[e.Kind]
		if k == "" {
			k = e.Kind
		}
		cw.Write([]string{minskTime(e.CreatedAt), who, k, csvSafe(e.Text), strconv.FormatInt(e.ChatID, 10)})
	}
	cw.Flush()
}

// GET /admin/users/{id}/export/{kind} — kind: operations | telegram | audit
func (s *Server) adminUserExport(w http.ResponseWriter, r *http.Request) {
	u, err := s.St.UserByID(r.Context(), pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	kind := r.PathValue("kind")
	base := fmt.Sprintf("user-%d-", u.ID)
	switch kind {
	case "operations":
		list, _, err := s.St.Orders(r.Context(), store.OrderFilter{UserID: u.ID, Limit: 100000})
		if err != nil {
			s.internal(w, r, err)
			return
		}
		writeOrdersCSV(csvStart(w, base+"operations.csv"), list, false)
	case "telegram":
		list, err := s.St.TelegramLog(r.Context(), u.ID, 100000)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		writeTelegramCSV(csvStart(w, base+"telegram.csv"), list)
	case "audit":
		list, err := s.St.AuditRange(r.Context(), u.ID, 0, 0, 100000)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		writeAuditCSV(csvStart(w, base+"journal.csv"), list)
	default:
		writeErr(w, http.StatusNotFound, "Нет такой выгрузки")
		return
	}
	// кто из сотрудников и что скачал — в журнал пользователя
	s.St.Audit(r.Context(), u.ID, currentUser(r).ID, "admin.export", kind+" ("+currentUser(r).Email+")", clientIP(r))
}

// GET /admin/orders/export.csv?from&to&status — все операции сервиса за период.
func (s *Server) adminOrdersExport(w http.ResponseWriter, r *http.Request) {
	from, to, ok := periodFromQuery(w, r)
	if !ok {
		return
	}
	list, _, err := s.St.Orders(r.Context(), store.OrderFilter{Status: r.URL.Query().Get("status"), From: from, To: to, Limit: 100000})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), currentUser(r).ID, currentUser(r).ID, "admin.export", "all operations", clientIP(r))
	writeOrdersCSV(csvStart(w, "autoinvest-all-operations.csv"), list, true)
}
