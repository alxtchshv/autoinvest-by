import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { ArrowDownToLine, ArrowLeft, Pause, Pencil, Play, Plus, SlidersHorizontal, Trash2 } from "lucide-react";
import { api, idemKey } from "../../lib/api";
import { byn, date, dateTime, kop, parseKop, pct, price, qty, scheduleText, when } from "../../lib/format";
import { useLoad, useSubmit, useTitle } from "../../lib/hooks";
import { useSession, useUserEvent } from "../../lib/session";
import type { Alloc, Card, Order, Plan, Snapshot } from "../../lib/types";
import { AssetIcon, Button, Change, Field, Input, LoadError, Modal, Money, Notice, OrderStatus, PageLoader, PLAN_ICONS, PlanIcon, PlanStatus, RiskDots, riskLabel, Segmented, Switch, useToast } from "../../components/ui";
import { ValueChart } from "../../components/charts";
import { SchedulePicker, StrategyPicker, type Schedule } from "./PlanNew";
import { AllocBar } from "./Plans";

const PERIODS = [{ value: "7d", label: "Неделя" }, { value: "1m", label: "Месяц" }, { value: "3m", label: "3 мес" }, { value: "all", label: "Всё" }] as const;

export function OperationRow({ o, showPlan }: { o: Order; showPlan?: boolean }) {
  const buy = o.side === "buy";
  const title = buy ? (o.trigger === "schedule" ? "Пополнение по расписанию" : o.trigger === "dip" ? "Докупка на просадке" : "Пополнение") : o.trigger === "close" ? "Закрытие копилки" : "Вывод на карту";
  return (
    <Link to={`/app/history/${o.id}`} className="list-row">
      <span className={`plan-ico`} style={{ width: 38, height: 38, borderRadius: 10, background: buy ? "var(--brand-soft)" : "var(--line-2)", color: buy ? "var(--brand-ink)" : "var(--ink-2)" }}>
        {buy ? <Plus size={18} /> : <ArrowDownToLine size={18} />}
      </span>
      <div className="grow">
        <div className="title">{title}</div>
        <div className="sub">{dateTime(o.created_at)}{showPlan ? ` · ${o.plan_name}` : ""}{o.status === "failed" && o.error ? ` · ${o.error}` : o.fills.length ? ` · ${o.fills.map((f) => f.asset).join(", ")}` : ""}</div>
      </div>
      <div className="end">
        <div className={`num ${o.status === "failed" ? "faint" : ""}`} style={{ fontWeight: 600, textDecoration: o.status === "failed" ? "line-through" : undefined }}>{buy ? "−" : "+"}{kop(o.amount_kop)} BYN</div>
        {o.status !== "filled" ? <OrderStatus s={o.status} /> : <div className="tiny muted">{o.card_last4 ? `•• ${o.card_last4}` : ""}</div>}
      </div>
    </Link>
  );
}

export default function PlanDetail() {
  const { id } = useParams();
  const { config, me, reload: reloadMe } = useSession();
  const nav = useNavigate();
  const toast = useToast();
  const [period, setPeriod] = useState<(typeof PERIODS)[number]["value"]>("1m");
  const [modal, setModal] = useState<null | "topup" | "withdraw" | "edit" | "mix" | "close">(null);
  const { data, error, loading, reload } = useLoad(() => api.get<{ plan: Plan; operations: Order[] }>(`/plans/${id}`), [id]);
  const hist = useLoad(() => api.get<{ points: Snapshot[] }>(`/plans/${id}/history?period=${period}`), [id, period]);
  const act = useSubmit();
  useUserEvent("plans", () => { reload(); hist.reload(); });
  useTitle(data?.plan.name || "Копилка");

  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const p = data!.plan;
  const v = p.valuation;
  const refresh = () => { reload(); hist.reload(); reloadMe(); };
  const toggle = async () => {
    try {
      await act.submit(() => api.post(`/plans/${p.id}/${p.status === "active" ? "pause" : "resume"}`));
      toast(p.status === "active" ? "Копилка на паузе — списаний не будет" : "Копилка снова работает");
      refresh();
    } catch (e: any) {
      if (e.code === "no_card") nav("/app/cards");
      else toast(e.message, "error");
    }
  };
  const closed = p.status === "closed";
  return (
    <div className="stack g16">
      <div>
        <Link to="/app/plans" className="back"><ArrowLeft />Копилки</Link>
        <div className="page-head" style={{ marginBottom: 0 }}>
          <div className="row g16">
            <PlanIcon icon={p.icon} />
            <div>
              <div className="row g12 wrap"><h1>{p.name}</h1><PlanStatus s={p.status} /></div>
              <p>{p.frequency === "manual" ? "Пополнение вручную" : `${byn(p.amount_kop)} ${scheduleText(p)}`}{p.card ? ` · карта •• ${p.card.last4}` : ""}</p>
            </div>
          </div>
          {!closed && (
            <div className="row g8 wrap">
              <Button variant="primary" icon={<Plus />} onClick={() => setModal("topup")}>Пополнить</Button>
              <Button icon={<ArrowDownToLine />} onClick={() => setModal("withdraw")} disabled={v.value_kop <= 0}>Вывести</Button>
              <Button icon={p.status === "active" ? <Pause /> : <Play />} busy={act.busy} onClick={toggle}>{p.status === "active" ? "Пауза" : "Включить"}</Button>
              {config?.mode === "sandbox" && config.broker === "paper" && p.status === "active" && p.frequency !== "manual" && (
                <Button variant="ghost" busy={act.busy} title="Только на тестовом стенде" onClick={async () => {
                  try { await act.submit(() => api.post(`/sandbox/plans/${p.id}/run-schedule`)); reload(); hist.reload(); toast("Плановое пополнение выполнено, следующее — по расписанию"); }
                  catch (e: any) { toast(e.message, "error"); }
                }}>Тест: пополнить по расписанию сейчас</Button>
              )}
            </div>
          )}
        </div>
      </div>

      {p.status === "paused" && !p.card && <Notice kind="warn">У копилки нет карты. <Link to="/app/cards">Привяжите карту</Link>, затем включите копилку.</Notice>}
      {p.fail_count > 0 && p.status === "active" && <Notice kind="warn">Последнее списание не прошло ({p.fail_count} подряд). После трёх отказов копилка встанет на паузу — проверьте баланс карты.</Notice>}

      <div className="home-grid">
        <div className="stack g16">
          <section className="sheet sheet-pad">
            <div className="plan-hero">
              <div>
                <div className="muted small">Стоимость сейчас</div>
                <Money k={v.value_kop} size="xl" />
                {v.invested_kop > 0 && (
                  <div className="row g12 wrap mt8 small">
                    <span className={`badge ${v.pnl_kop >= 0 ? "badge-up" : "badge-down"}`}>{byn(v.pnl_kop, { sign: true })} · {pct(v.pnl_pct)}</span>
                    <span className="muted">вложено {byn(v.invested_kop)}</span>
                  </div>
                )}
              </div>
            </div>
            {p.goal_kop ? (
              <div className="mt24">
                <div className="row between small"><b>Цель {byn(p.goal_kop)}</b><span className="muted">{(p.goal_progress_bp / 100).toFixed(1).replace(".", ",")}%</span></div>
                <div className="bar mt8"><span style={{ width: p.goal_progress_bp / 100 + "%" }} /></div>
                <div className="goal-line">
                  <span>{p.goal_progress_bp >= 10000 ? "Цель достигнута" : p.goal_eta ? `При текущем темпе — к ${date(new Date(p.goal_eta).getTime() / 1000)}` : "Включите пополнения, чтобы увидеть прогноз"}</span>
                  {p.goal_date && <span>Срок: {date(new Date(p.goal_date).getTime() / 1000)}</span>}
                </div>
                {p.goal_need_kop && p.goal_need_kop > p.amount_kop && p.goal_progress_bp < 10000 && (
                  <div className="small mt8">Чтобы успеть к сроку, пополняйте по <b>{byn(p.goal_need_kop)}</b>. <button className="link-btn" onClick={() => setModal("edit")}>Изменить сумму</button></div>
                )}
              </div>
            ) : null}
          </section>

          <section className="sheet">
            <div className="sheet-head"><h2>Динамика</h2><Segmented label="Период" value={period} onChange={setPeriod} options={PERIODS.map((x) => ({ ...x }))} /></div>
            <div className="sheet-body">
              {hist.data && hist.data.points.length >= 2 ? <ValueChart points={hist.data.points} height={240} /> : <div className="muted small" style={{ padding: "36px 0", textAlign: "center" }}>График появится после первого пополнения: стоимость записывается раз в час.</div>}
            </div>
          </section>

          <section className="sheet">
            <div className="sheet-head"><h2>Активы</h2>{!closed && <Button size="sm" variant="ghost" icon={<SlidersHorizontal />} onClick={() => setModal("mix")}>Изменить состав</Button>}</div>
            {v.positions.length === 0 ? (
              <div className="sheet-body"><AllocBar plan={p} /><div className="small muted mt8">{p.allocations.map((a) => `${a.asset} ${a.weight_bp / 100}%`).join(" · ")} — активы появятся после первого пополнения</div></div>
            ) : (
              <div className="table-wrap">
                <table className="table">
                  <thead><tr><th>Актив</th><th className="r">Количество</th><th className="r">Цена</th><th className="r">Стоимость</th><th className="r">Доход</th></tr></thead>
                  <tbody>
                    {v.positions.map((x) => {
                      const a = config?.assets.find((s) => s.symbol === x.asset);
                      const pnl = x.value_kop - x.cost_kop;
                      return (
                        <tr key={x.asset} className="click" onClick={() => nav(`/app/market/${x.asset}`)}>
                          <td><span className="row g12"><AssetIcon symbol={x.asset} size="sm" /><span><div style={{ fontWeight: 560 }}>{a?.name}</div><div className="tiny muted">{(p.allocations.find((al) => al.asset === x.asset)?.weight_bp || 0) / 100}% пополнений</div></span></span></td>
                          <td className="r num">{qty(x.qty, x.asset)}</td>
                          <td className="r num">{price(x.price_byn)}<div className="tiny"><Change v={x.change_24h} /></div></td>
                          <td className="r"><Money k={x.value_kop} /></td>
                          <td className="r"><Change k={pnl} /><div className="tiny"><Change v={x.cost_kop ? (pnl / x.cost_kop) * 100 : 0} /></div></td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </section>
        </div>

        <div className="stack g16">
          {!closed && (
            <section className="sheet">
              <div className="sheet-head"><h2>Расписание</h2><Button size="sm" variant="ghost" icon={<Pencil />} onClick={() => setModal("edit")}>Изменить</Button></div>
              <div className="sheet-body stack g12">
                {p.status === "active" && p.next_runs.length ? (
                  p.next_runs.map((t, i) => <div key={t} className="row between"><span className={i ? "muted" : ""}>{when(t)}</span><Money k={p.amount_kop} /></div>)
                ) : <div className="muted small">{p.frequency === "manual" ? "Пополнения только вручную" : "Копилка на паузе"}</div>}
                <div className="divider" style={{ margin: "4px 0" }} />
                <div className="row between small"><span className="muted">В месяц</span><Money k={p.monthly_kop} /></div>
                <div className="row between small"><span className="muted">Риск</span><span className="row g8"><RiskDots risk={p.risk} />{riskLabel(p.risk)}</span></div>
                {p.dip_enabled && <div className="row between small"><span className="muted">Докупка на просадке</span><span>при −{p.dip_threshold_bp / 100}% за сутки</span></div>}
              </div>
            </section>
          )}
          <section className="sheet">
            <div className="sheet-head"><h2>Операции</h2><Link to={`/app/history?plan=${p.id}`} className="small">Все</Link></div>
            {data!.operations.length ? <div className="list">{data!.operations.slice(0, 8).map((o) => <OperationRow key={o.id} o={o} />)}</div> : <div className="sheet-body small muted">Операций пока не было.</div>}
          </section>
          {!closed && <Button variant="danger" icon={<Trash2 />} onClick={() => setModal("close")}>Закрыть копилку</Button>}
        </div>
      </div>

      <TopupModal plan={p} open={modal === "topup"} onClose={() => setModal(null)} onDone={refresh} />
      <WithdrawModal plan={p} open={modal === "withdraw"} onClose={() => setModal(null)} onDone={refresh} />
      <EditModal plan={p} open={modal === "edit"} onClose={() => setModal(null)} onDone={refresh} />
      <MixModal plan={p} open={modal === "mix"} onClose={() => setModal(null)} onDone={refresh} isPro={!!me?.is_pro} />
      <CloseModal plan={p} open={modal === "close"} onClose={() => setModal(null)} onDone={() => { reloadMe(); nav("/app/plans"); }} />
    </div>
  );
}

type MP = { plan: Plan; open: boolean; onClose: () => void; onDone: () => void };

function TopupModal({ plan, open, onClose, onDone }: MP) {
  const { config } = useSession();
  const toast = useToast();
  const [amount, setAmount] = useState(String(plan.amount_kop / 100));
  const [key] = useState(idemKey);
  const s = useSubmit();
  const k = parseKop(amount);
  const { me } = useSession();
  const fee = isFinite(k) ? Math.round(k * ((me?.is_pro ? config?.fee_pro_bp : config?.fee_free_bp) ?? 100) / 10000) : 0;
  return (
    <Modal open={open} onClose={onClose} title="Пополнить копилку">
      <form className="stack g16" onSubmit={async (e) => {
        e.preventDefault();
        try {
          const r = await s.submit(() => api.post<{ order: Order; pending: boolean }>(`/plans/${plan.id}/topup`, { amount_kop: k, idempotency_key: key + amount }));
          if (r?.order?.status === "failed") { s.setError(r.order.error); return; }
          toast(r?.pending ? "Платёж в обработке — статус придёт уведомлением" : `Копилка пополнена на ${byn(k)}`);
          onDone();
          onClose();
        } catch { /* */ }
      }}>
        <Field label="Сумма" error={s.fieldError("amount_kop") || (!s.field ? s.error : null)}>
          <div className="input-affix"><input className="input amount-input" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} autoFocus aria-label="Сумма" /><span className="affix" style={{ fontSize: 18 }}>BYN</span></div>
        </Field>
        <div className="small muted">Спишем с карты •• {plan.card?.last4 || "—"} и купим {plan.allocations.map((a) => `${a.asset} ${a.weight_bp / 100}%`).join(", ")} по текущей цене. Комиссия сервиса ≈ {byn(fee)}.</div>
        <Button type="submit" variant="primary" size="lg" block busy={s.busy} disabled={!isFinite(k) || !plan.card}>Пополнить на {isFinite(k) ? byn(k) : "…"}</Button>
      </form>
    </Modal>
  );
}

function WithdrawModal({ plan, open, onClose, onDone }: MP) {
  const toast = useToast();
  const [all, setAll] = useState(false);
  const [amount, setAmount] = useState("");
  const [password, setPassword] = useState("");
  const [key] = useState(idemKey);
  const s = useSubmit();
  const k = parseKop(amount);
  const v = plan.valuation.value_kop;
  return (
    <Modal open={open} onClose={onClose} title="Вывести на карту">
      <form className="stack g16" onSubmit={async (e) => {
        e.preventDefault();
        try {
          const r = await s.submit(() => api.post<{ order: Order }>(`/plans/${plan.id}/withdraw`, { amount_kop: all ? 0 : k, all, password, idempotency_key: key + (all ? "all" : amount) }));
          toast(r?.order.status === "filled" ? `${byn(r.order.amount_kop)} отправлено на карту` : "Активы проданы, выплата будет проведена в течение 1–3 рабочих дней");
          onDone();
          onClose();
        } catch { /* */ }
      }}>
        <div className="small">В копилке сейчас <b><Money k={v} /></b>. Продадим активы пропорционально по текущей цене.</div>
        <Segmented label="Сколько" value={all ? "all" : "part"} onChange={(x) => setAll(x === "all")} options={[{ value: "part", label: "Часть" }, { value: "all", label: "Всё" }]} />
        {!all && (
          <Field label="Сумма" error={s.fieldError("amount_kop")}>
            <div className="input-affix"><input className="input amount-input" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} autoFocus aria-label="Сумма" placeholder="0" /><span className="affix" style={{ fontSize: 18 }}>BYN</span></div>
          </Field>
        )}
        <Field label="Пароль от аккаунта" error={s.fieldError("password")} hint="Подтверждаем вывод, чтобы деньги не увёл тот, кто получил доступ к вашему устройству">
          <Input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
        <div className="small muted">Деньги придут на карту •• {plan.card?.last4 || "основную"}. Цена продажи — биржевая минус спред партнёра. Пополнения по расписанию продолжатся.</div>
        <Button type="submit" variant="primary" size="lg" block busy={s.busy} disabled={(!all && !isFinite(k)) || !password}>{all ? `Вывести всё ≈ ${byn(v)}` : `Вывести ${isFinite(k) ? byn(k) : ""}`}</Button>
      </form>
    </Modal>
  );
}

function EditModal({ plan, open, onClose, onDone }: MP) {
  const toast = useToast();
  const cards = useLoad(() => (open ? api.get<{ cards: Card[] }>("/cards") : Promise.resolve({ cards: [] })), [open]);
  const [name, setName] = useState(plan.name);
  const [icon, setIcon] = useState(plan.icon);
  const [goal, setGoal] = useState(plan.goal_kop ? String(plan.goal_kop / 100) : "");
  const [goalDate, setGoalDate] = useState(plan.goal_date || "");
  const [amount, setAmount] = useState(String(plan.amount_kop / 100));
  const [sched, setSched] = useState<Schedule>({ frequency: plan.frequency, weekday: plan.weekday, monthday: plan.monthday, run_minute: plan.run_minute });
  const [cardId, setCardId] = useState(plan.card_id);
  const s = useSubmit();
  return (
    <Modal open={open} onClose={onClose} title="Настройки копилки" wide>
      <form className="stack g16" onSubmit={async (e) => {
        e.preventDefault();
        const gk = goal ? parseKop(goal) : 0;
        try {
          await s.submit(() => api.patch(`/plans/${plan.id}`, { name, icon, goal_kop: gk, goal_date: goalDate, clear_goal: !goal, amount_kop: parseKop(amount), ...sched, card_id: cardId ?? undefined }));
          toast("Сохранено");
          onDone();
          onClose();
        } catch { /* */ }
      }}>
        {s.error && <Notice kind="error">{s.error}</Notice>}
        <Field label="Название"><Input value={name} maxLength={40} onChange={(e) => setName(e.target.value)} /></Field>
        <div className="icon-pick">{Object.entries(PLAN_ICONS).map(([k, I]) => <button key={k} type="button" aria-pressed={icon === k} aria-label={k} onClick={() => setIcon(k)}><I /></button>)}</div>
        <div className="grid2">
          <Field label="Цель, BYN" hint="Пусто — без цели"><Input inputMode="decimal" value={goal} onChange={(e) => setGoal(e.target.value)} /></Field>
          <Field label="Срок"><Input type="date" value={goalDate} onChange={(e) => setGoalDate(e.target.value)} /></Field>
        </div>
        <Field label="Сумма пополнения, BYN"><Input inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} /></Field>
        <SchedulePicker value={sched} onChange={setSched} />
        {!!cards.data?.cards.length && (
          <Field label="Карта">
            <select className="select" value={cardId ?? ""} onChange={(e) => setCardId(Number(e.target.value))}>
              {cards.data.cards.map((c) => <option key={c.id} value={c.id}>{c.brand.toUpperCase()} •• {c.last4}</option>)}
            </select>
          </Field>
        )}
        <div className="row g8" style={{ justifyContent: "flex-end" }}><Button onClick={onClose}>Отмена</Button><Button type="submit" variant="primary" busy={s.busy}>Сохранить</Button></div>
      </form>
    </Modal>
  );
}

function MixModal({ plan, open, onClose, onDone, isPro }: MP & { isPro: boolean }) {
  const toast = useToast();
  const [strategy, setStrategy] = useState(plan.strategy);
  const [allocs, setAllocs] = useState<Alloc[]>(plan.allocations);
  const [dip, setDip] = useState(plan.dip_enabled);
  const [ack, setAck] = useState(false);
  const s = useSubmit();
  const sum = allocs.reduce((x, a) => x + a.weight_bp, 0);
  return (
    <Modal open={open} onClose={onClose} title="Состав копилки" wide>
      <div className="stack g16">
        <p className="small muted">Новый состав применится к следующим пополнениям. Уже купленные активы останутся как есть.</p>
        <StrategyPicker strategy={strategy} allocations={allocs} isPro={isPro} onChange={(st, a) => { setStrategy(st); setAllocs(a); }} />
        <div className="setting-row" style={{ borderTop: "1px solid var(--line-2)", paddingTop: 16 }}>
          <div><div style={{ fontWeight: 560 }}>Докупать при падении больше 5% за сутки</div><div className="small muted">{isPro ? "Дополнительное пополнение на обычную сумму, не чаще раза в сутки" : "Доступно в Pro"}</div></div>
          <Switch label="Докупка на просадке" checked={dip} disabled={!isPro} onChange={setDip} />
        </div>
        {s.error && <Notice kind="error">{s.error}</Notice>}
        {ack && <label className="check"><input type="checkbox" onChange={(e) => setAck(e.target.checked)} />Понимаю, что этот состав рискованнее моего профиля</label>}
        <div className="row g8" style={{ justifyContent: "flex-end" }}>
          <Button onClick={onClose}>Отмена</Button>
          <Button variant="primary" busy={s.busy} disabled={sum !== 10000} onClick={async () => {
            try {
              await s.submit(() => api.patch(`/plans/${plan.id}`, { strategy, allocations: strategy === "custom" ? allocs : undefined, dip_enabled: dip, risk_ack: ack }));
              toast("Состав обновлён");
              onDone();
              onClose();
            } catch (e: any) {
              if (e.code === "risk_ack_required") { setAck(true); s.setError(e.message); }
            }
          }}>Сохранить</Button>
        </div>
      </div>
    </Modal>
  );
}

function CloseModal({ plan, open, onClose, onDone }: MP) {
  const toast = useToast();
  const s = useSubmit();
  const [password, setPassword] = useState("");
  const v = plan.valuation.value_kop;
  return (
    <Modal open={open} onClose={onClose} title="Закрыть копилку?">
      <div className="stack g16">
        <p>{v > 0 ? <>Все активы будут проданы по текущей цене, около <b>{byn(v)}</b> придёт на карту •• {plan.card?.last4 || "основную"}. Пополнения прекратятся.</> : "Пополнения прекратятся, копилка переместится в закрытые."}</p>
        {v > 0 && <Field label="Пароль от аккаунта" error={s.fieldError("password")}><Input type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} /></Field>}
        {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
        <div className="row g8" style={{ justifyContent: "flex-end" }}>
          <Button onClick={onClose}>Оставить</Button>
          <Button variant="danger" busy={s.busy} onClick={async () => {
            try { await s.submit(() => api.post(`/plans/${plan.id}/close`, { password })); toast("Копилка закрыта"); onDone(); } catch { /* */ }
          }}>Закрыть копилку</Button>
        </div>
      </div>
    </Modal>
  );
}
