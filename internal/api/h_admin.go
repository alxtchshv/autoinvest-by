package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"autoinvest/internal/money"
	"autoinvest/internal/notify"
	"autoinvest/internal/store"
)

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request) {
	o, err := s.St.Overview(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	series, _ := s.St.DailySeries(r.Context(), 30)
	writeJSON(w, 200, map[string]any{"overview": o, "series": series})
}

// adminMetrics — показатели для партнёров и инвесторов. Стоимость активов (AUM) считается
// по текущим ценам; если котировки нет, актив учитывается по себестоимости.
func (s *Server) adminMetrics(w http.ResponseWriter, r *http.Request) {
	m, err := s.St.PartnerMetrics(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	var aum, cost int64
	type holding struct {
		store.AssetQty
		ValueKop int64 `json:"value_kop"`
	}
	holdings := make([]holding, 0, len(m.Holdings))
	for _, h := range m.Holdings {
		v := h.CostKop
		if q, err := s.Hub.Quote(h.Asset); err == nil && q.PriceBYN > 0 {
			v = int64(math.Round(money.QtyToFloat(h.Qty) * q.PriceBYN * 100))
		}
		aum += v
		cost += h.CostKop
		holdings = append(holdings, holding{h, v})
	}
	writeJSON(w, 200, map[string]any{"metrics": m, "holdings": holdings, "aum_kop": aum, "cost_kop": cost})
}

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	per := min(max(qInt(r, "per", 50), 1), 200)
	page := max(qInt(r, "page", 1), 1)
	list, total, err := s.St.AdminUsers(r.Context(), r.URL.Query().Get("q"), per, (page-1)*per)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if list == nil {
		list = []store.AdminUser{}
	}
	writeJSON(w, 200, map[string]any{"users": list, "total": total, "page": page, "per": per})
}

func (s *Server) adminUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.St.UserByID(r.Context(), pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	plans, _ := s.St.Plans(r.Context(), u.ID, true)
	cm := s.cardMap(r, u.ID)
	views := make([]planView, 0, len(plans))
	for _, p := range plans {
		views = append(views, s.viewPlan(r, p, cm))
	}
	cards, _ := s.St.Cards(r.Context(), u.ID)
	orders, _, _ := s.St.Orders(r.Context(), store.OrderFilter{UserID: u.ID, Limit: 50})
	kyc, _ := s.St.KYC(r.Context(), u.ID)
	audit, _ := s.St.AuditFor(r.Context(), u.ID, 50)
	tickets, _ := s.St.Tickets(r.Context(), u.ID, "", 50)
	tg, _ := s.St.TelegramLog(r.Context(), u.ID, 300)
	writeJSON(w, 200, map[string]any{"user": u, "is_pro": u.IsPro(), "plans": views, "cards": nz(cards), "orders": nz(orders), "kyc": kyc, "audit": nz(audit), "tickets": tickets, "telegram": nz(tg)})
}

func nz[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// adminDeleteUser — POST /admin/users/{id}/delete: удалить аккаунт (тестовые, по просьбе человека).
// Как при самостоятельном удалении: деньги в копилках и операции в обработке не дают удалить.
func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := pathID(r)
	if id == currentUser(r).ID {
		writeErr(w, http.StatusBadRequest, "Нельзя удалить себя")
		return
	}
	u, err := s.St.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || err == nil && (u.Status == "deleted" || u.IsDemo()) {
		writeErr(w, http.StatusNotFound, "Пользователь не найден")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if u.Role != "user" {
		writeErr(w, http.StatusConflict, "Сначала смените роль на «Пользователь»")
		return
	}
	if msg := s.eraseAccount(r.Context(), u); msg != "" {
		writeErr(w, http.StatusConflict, "У пользователя: "+strings.ToLower(msg[:1])+msg[1:])
		return
	}
	s.St.Audit(r.Context(), u.ID, currentUser(r).ID, "admin.delete", "", clientIP(r))
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminUserStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Status != "active" && in.Status != "blocked" {
		writeErr(w, http.StatusBadRequest, "Некорректный статус")
		return
	}
	id := pathID(r)
	if id == currentUser(r).ID {
		writeErr(w, http.StatusBadRequest, "Нельзя заблокировать себя")
		return
	}
	if t, err := s.St.UserByID(r.Context(), id); err != nil || t.Status == "deleted" {
		writeErr(w, http.StatusNotFound, "Пользователь не найден или удалён")
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Status == "blocked" && in.Reason == "" {
		writeFieldErr(w, "reason", "Укажите причину — человек увидит её при попытке войти")
		return
	}
	if r := []rune(in.Reason); len(r) > 300 {
		in.Reason = string(r[:300])
	}
	if in.Status == "active" {
		in.Reason = ""
	}
	if err := s.St.UpdateUserFields(r.Context(), id, map[string]any{"status": in.Status, "block_reason": in.Reason}); err != nil {
		s.internal(w, r, err)
		return
	}
	if in.Status == "blocked" {
		s.DB.ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id=?`, id)
	}
	s.St.Audit(r.Context(), id, currentUser(r).ID, "admin.status", in.Status+" "+in.Reason, clientIP(r))
	s.adminUser(w, r)
}

func (s *Server) adminUserRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role string `json:"role"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Role != "user" && in.Role != "support" && in.Role != "admin" {
		writeErr(w, http.StatusBadRequest, "Некорректная роль")
		return
	}
	id := pathID(r)
	if id == currentUser(r).ID {
		writeErr(w, http.StatusBadRequest, "Нельзя менять свою роль")
		return
	}
	s.St.UpdateUserFields(r.Context(), id, map[string]any{"role": in.Role})
	s.St.Audit(r.Context(), id, currentUser(r).ID, "admin.role", in.Role, clientIP(r))
	s.adminUser(w, r)
}

func (s *Server) adminGrantPro(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Days int `json:"days"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Days < 1 || in.Days > 366 {
		writeErr(w, http.StatusBadRequest, "От 1 до 366 дней")
		return
	}
	id := pathID(r)
	if err := s.En.ExtendPro(r.Context(), id, time.Duration(in.Days)*24*time.Hour); err != nil {
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), id, currentUser(r).ID, "admin.grant_pro", fmt.Sprint(in.Days), clientIP(r))
	s.N.Send(r.Context(), id, notify.Message{Kind: "billing", Title: fmt.Sprintf("Вам подключён Pro на %d дней", in.Days), Link: "/app/profile/billing"})
	s.adminUser(w, r)
}

func (s *Server) adminKYC(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	if status == "all" {
		status = ""
	}
	list, err := s.St.KYCList(r.Context(), status, 200)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": list})
}

func (s *Server) adminKYCReview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Approve     bool   `json:"approve"`
		Reason      string `json:"reason"`
		SubmittedAt int64  `json:"submitted_at"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	id := pathID(r)
	if !in.Approve && strings.TrimSpace(in.Reason) == "" {
		writeFieldErr(w, "reason", "Укажите причину отказа — её увидит пользователь")
		return
	}
	if err := s.St.ReviewKYC(r.Context(), id, currentUser(r).ID, in.Approve, strings.TrimSpace(in.Reason), in.SubmittedAt); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusConflict, "Заявка изменилась или уже проверена — обновите страницу")
			return
		}
		s.internal(w, r, err)
		return
	}
	s.St.Audit(r.Context(), id, currentUser(r).ID, "kyc.review", map[bool]string{true: "approved", false: "rejected: " + in.Reason}[in.Approve], clientIP(r))
	if in.Approve {
		s.N.Send(r.Context(), id, notify.Message{Kind: "kyc", Title: "Личность подтверждена", Body: "Теперь вам доступны все возможности сервиса.", Link: "/app/profile/verification", Email: true})
	} else {
		s.N.Send(r.Context(), id, notify.Message{Kind: "kyc", Title: "Верификация не пройдена", Body: in.Reason + ". Исправьте данные и отправьте снова.", Link: "/app/profile/verification", Email: true})
	}
	s.adminKYC(w, r)
}

func (s *Server) adminKYCFile(w http.ResponseWriter, r *http.Request) {
	k, err := s.St.KYC(r.Context(), pathID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	name := k.DocFile
	if r.PathValue("kind") == "selfie" {
		name = k.SelfieFile
	}
	if name == "" || strings.ContainsAny(name, `/\`) {
		writeErr(w, http.StatusNotFound, "Файл не загружен")
		return
	}
	s.St.Audit(r.Context(), k.UserID, currentUser(r).ID, "kyc.file_viewed", r.PathValue("kind"), clientIP(r))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, filepath.Join(s.Cfg.DataDir, "kyc", name))
}

func (s *Server) adminOrders(w http.ResponseWriter, r *http.Request) {
	per := min(max(qInt(r, "per", 50), 1), 200)
	page := max(qInt(r, "page", 1), 1)
	list, total, err := s.St.Orders(r.Context(), store.OrderFilter{Status: r.URL.Query().Get("status"), Side: r.URL.Query().Get("side"), Limit: per, Offset: (page - 1) * per})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, 200, map[string]any{"orders": nz(list), "total": total, "page": page, "per": per})
}

func (s *Server) adminPayoutDone(w http.ResponseWriter, r *http.Request) {
	if err := s.En.MarkPayoutDone(r.Context(), pathID(r), currentUser(r).ID); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminSystem(w http.ResponseWriter, r *http.Request) {
	var dbSize int64
	if fi, err := os.Stat(filepath.Join(s.Cfg.DataDir, "autoinvest.db")); err == nil {
		dbSize = fi.Size()
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	writeJSON(w, 200, map[string]any{
		"version": s.Version, "uptime_sec": int64(time.Since(s.Started).Seconds()), "mode": s.Cfg.Mode(), "payments": s.Pay.Name(),
		"payments_test": s.Pay.Test(), "broker": s.En.Broker.Name(), "market_demo": s.Hub.IsFixture(), "sources": s.Hub.Status(),
		"rates": s.Hub.Rates(), "db_bytes": dbSize, "mem_mb": ms.Alloc / 1024 / 1024, "goroutines": runtime.NumGoroutine(),
		"telegram": s.botUsername() != "", "smtp": s.N.Mailer().Enabled(), "require_kyc": s.Cfg.RequireKYC, "public_url": s.Cfg.PublicURL,
	})
}

func (s *Server) adminOrderFill(w http.ResponseWriter, r *http.Request) {
	if err := s.En.FillManual(r.Context(), pathID(r), currentUser(r).ID); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) adminOrderRefund(w http.ResponseWriter, r *http.Request) {
	if err := s.En.RefundManual(r.Context(), pathID(r), currentUser(r).ID); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
