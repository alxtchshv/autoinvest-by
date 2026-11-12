package engine

import (
	"context"
	"errors"
	"time"

	"autoinvest/internal/store"
)

// ErrWaitlist — до запуска настоящие пополнения закрыты (LAUNCH_MODE=waitlist).
var ErrWaitlist = errors.New("Пополнения откроются совсем скоро — вы в списке первых. А пока попробуйте демо-счёт на сайте")

// ErrNoCardResume — копилку нельзя включить: у пользователя нет ни одной карты.
var ErrNoCardResume = errors.New("Привяжите карту, чтобы включить копилку")

// PausePlan ставит копилку на паузу (сайт и Telegram-бот пользуются одной и той же логикой).
func (e *Engine) PausePlan(ctx context.Context, p *store.Plan, ip string) error {
	if p.Status != "active" {
		return nil
	}
	p.Status, p.NextRunAt, p.Allocations = "paused", nil, nil
	if err := e.St.UpdatePlan(ctx, p); err != nil {
		return err
	}
	e.St.Audit(ctx, p.UserID, p.UserID, "plan.paused", p.Name, ip)
	return nil
}

// ResumePlan включает копилку и считает следующее пополнение по расписанию.
func (e *Engine) ResumePlan(ctx context.Context, p *store.Plan, ip string) error {
	if p.Status == "closed" {
		return ErrPlanClosed
	}
	if e.Cfg != nil && e.Cfg.LaunchMode == "waitlist" {
		if u, err := e.St.UserByID(ctx, p.UserID); err == nil && !u.IsDemo() {
			e.St.JoinWaitlist(ctx, u.ID)
			return ErrWaitlist
		}
	}
	if p.CardID == nil {
		c, err := e.St.DefaultCard(ctx, p.UserID)
		if err != nil {
			return ErrNoCardResume
		}
		p.CardID = &c.ID
	}
	p.Status, p.FailCount, p.Allocations = "active", 0, nil
	p.NextRunAt = NextRun(p, time.Now(), false)
	if err := e.St.UpdatePlan(ctx, p); err != nil {
		return err
	}
	e.St.Audit(ctx, p.UserID, p.UserID, "plan.resumed", p.Name, ip)
	return nil
}
