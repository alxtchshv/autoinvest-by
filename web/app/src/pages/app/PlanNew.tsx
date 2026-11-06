import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { api, ApiError } from "../../lib/api";
import { byn, freqLabel, kop, minuteStr, parseKop, WD_SHORT, planName } from "../../lib/format";
import { useLoad, useSubmit, useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { Alloc, Card, Plan } from "../../lib/types";
import { AssetIcon, Button, Field, Input, Modal, Notice, PLAN_ICONS, RiskDots, riskLabel, Segmented, Switch, useToast } from "../../components/ui";

export type Schedule = { frequency: Plan["frequency"]; weekday: number; monthday: number; run_minute: number };

const PERIODS_PER_MONTH: Record<string, number> = { daily: 30.4, weekly: 52 / 12, biweekly: 26 / 12, monthly: 1, manual: 0 };

export function SchedulePicker({ value, onChange }: { value: Schedule; onChange: (s: Schedule) => void }) {
  const set = (p: Partial<Schedule>) => onChange({ ...value, ...p });
  return (
    <div className="stack g16">
      <Field label="Как часто">
        <Segmented label="Периодичность" value={value.frequency} onChange={(f) => set({ frequency: f })}
          options={(["daily", "weekly", "biweekly", "monthly", "manual"] as const).map((f) => ({ value: f, label: freqLabel[f] }))} />
      </Field>
      {(value.frequency === "weekly" || value.frequency === "biweekly") && (
        <Field label="День недели">
          <div className="row g8 wrap">
            {[1, 2, 3, 4, 5, 6, 7].map((d) => <button key={d} type="button" className="chip" aria-pressed={value.weekday === d} onClick={() => set({ weekday: d })}>{WD_SHORT[d]}</button>)}
          </div>
        </Field>
      )}
      {value.frequency === "monthly" && (
        <Field label="Число месяца" hint="Если в месяце нет такого числа, пополнение пройдёт в последний день">
          <select className="select" style={{ maxWidth: 200 }} value={value.monthday} onChange={(e) => set({ monthday: Number(e.target.value) })}>
            {Array.from({ length: 31 }, (_, i) => i + 1).map((d) => <option key={d} value={d}>{d}-е</option>)}
          </select>
        </Field>
      )}
      {value.frequency !== "manual" && (
        <Field label="Время списания" hint="По Минску">
          <select className="select" style={{ maxWidth: 200 }} value={value.run_minute} onChange={(e) => set({ run_minute: Number(e.target.value) })}>
            {Array.from({ length: 48 }, (_, i) => i * 30).filter((m) => m >= 360 && m <= 1380).map((m) => <option key={m} value={m}>{minuteStr(m)}</option>)}
          </select>
        </Field>
      )}
    </div>
  );
}

export function StrategyPicker({ strategy, allocations, onChange, isPro }: { strategy: string; allocations: Alloc[]; onChange: (s: string, a: Alloc[]) => void; isPro: boolean }) {
  const { config } = useSession();
  const [mode, setMode] = useState<"ready" | "custom">(strategy === "custom" ? "custom" : "ready");
  const sum = allocations.reduce((s, a) => s + a.weight_bp, 0);
  const toggle = (sym: string) => {
    const has = allocations.find((a) => a.asset === sym);
    onChange("custom", has ? allocations.filter((a) => a.asset !== sym) : [...allocations, { asset: sym, weight_bp: 1000 }]);
  };
  const setW = (sym: string, w: number) => onChange("custom", allocations.map((a) => (a.asset === sym ? { ...a, weight_bp: Math.max(500, Math.min(10000, Math.round(w / 100) * 100)) } : a)));
  return (
    <div className="stack g16">
      <Segmented label="Тип состава" value={mode} onChange={(m) => {
        setMode(m);
        if (m === "ready") { const s = config!.strategies[2]; onChange(s.id, Object.entries(s.weights).map(([asset, weight_bp]) => ({ asset, weight_bp }))); }
        else onChange("custom", allocations.length ? allocations : [{ asset: "USDT", weight_bp: 5000 }, { asset: "BTC", weight_bp: 5000 }]);
      }} options={[{ value: "ready", label: "Готовые стратегии" }, { value: "custom", label: "Свой состав" }]} />
      {mode === "ready" ? (
        <div className="strategy-pick">
          {config?.strategies.map((s) => (
            <button key={s.id} type="button" className="choice" aria-pressed={strategy === s.id} onClick={() => onChange(s.id, Object.entries(s.weights).map(([asset, weight_bp]) => ({ asset, weight_bp })).sort((a, b) => b.weight_bp - a.weight_bp))}>
              <div className="row between g12">
                <b>{planName(s.name)}</b>
                <span className="row g8 small muted"><RiskDots risk={s.risk} />{riskLabel(s.risk)} риск</span>
              </div>
              <div className="small muted mt8" style={{ marginTop: 4 }}>{s.description}</div>
              <div className="row g12 wrap small mt8">
                {Object.entries(s.weights).sort((a, b) => b[1] - a[1]).map(([a, w]) => <span key={a} className="row g4"><span className="dot" style={{ background: config.assets.find((x) => x.symbol === a)?.color }} />{a} {w / 100}%</span>)}
              </div>
            </button>
          ))}
        </div>
      ) : !isPro ? (
        <Notice kind="info" title="Свой состав — в тарифе Pro">Соберите копилку из любых активов в нужных пропорциях. <Link to="/app/profile/billing">Подключить Pro</Link></Notice>
      ) : (
        <div>
          <div className="row g8 wrap" style={{ marginBottom: 8 }}>
            {config?.assets.map((a) => <button key={a.symbol} type="button" className="chip" aria-pressed={!!allocations.find((x) => x.asset === a.symbol)} onClick={() => toggle(a.symbol)}>{a.name}</button>)}
          </div>
          {allocations.map((a) => (
            <div key={a.asset} className="alloc-edit">
              <AssetIcon symbol={a.asset} size="sm" />
              <input type="range" min={500} max={10000} step={100} value={a.weight_bp} onChange={(e) => setW(a.asset, Number(e.target.value))} aria-label={`Доля ${a.asset}`} />
              <div className="input-affix"><input className="input" style={{ height: 36, paddingRight: 30 }} inputMode="numeric" value={a.weight_bp / 100} onChange={(e) => setW(a.asset, Number(e.target.value.replace(/\D/g, "")) * 100)} aria-label={`Доля ${a.asset} в процентах`} /><span className="affix">%</span></div>
            </div>
          ))}
          <div className={`small mt8 ${sum === 10000 ? "up" : "down"}`} role="status">
            {sum === 10000 ? "Сумма долей 100%" : sum < 10000 ? `Сумма долей ${sum / 100}% — добавьте ещё ${(10000 - sum) / 100}%` : `Сумма долей ${sum / 100}% — уберите ${(sum - 10000) / 100}%`}
          </div>
        </div>
      )}
    </div>
  );
}

export function planRisk(allocs: Alloc[], assets: { symbol: string; risk: number }[]) {
  const r = allocs.reduce((s, a) => s + (assets.find((x) => x.symbol === a.asset)?.risk || 0) * a.weight_bp / 10000, 0);
  return Math.max(1, Math.round(r));
}

const NAME_HINTS = [["Подушка", "shield"], ["На отпуск", "plane"], ["На ноутбук", "laptop"], ["На машину", "car"], ["На учёбу", "graduation"], ["Долгосрочно", "rocket"]];
const AMOUNTS = [500, 1000, 2000, 5000, 10000];
const MAX_RISK: Record<string, number> = { conservative: 2, moderate: 3, aggressive: 5 };

export default function PlanNew() {
  useTitle("Новая копилка");
  const { config, me, reload } = useSession();
  const nav = useNavigate();
  const toast = useToast();
  const [sp] = useSearchParams();
  const cards = useLoad(() => api.get<{ cards: Card[] }>("/cards"), []);
  const [step, setStep] = useState(0);
  const [name, setName] = useState("");
  const [icon, setIcon] = useState("piggy");
  const [goal, setGoal] = useState("");
  const [goalDate, setGoalDate] = useState("");
  const [amount, setAmount] = useState(sp.get("amount") || "20");
  const [sched, setSched] = useState<Schedule>({ frequency: (sp.get("freq") as Schedule["frequency"]) || "weekly", weekday: 5, monthday: 1, run_minute: 600 });
  const [strategy, setStrategy] = useState(sp.get("strategy") || "balanced");
  const [allocs, setAllocs] = useState<Alloc[]>([]);
  const [dip, setDip] = useState(false);
  const [cardId, setCardId] = useState<number | null>(null);
  const [topup, setTopup] = useState(true);
  const [riskAsk, setRiskAsk] = useState(false);
  const s = useSubmit();
  const isPro = !!me?.is_pro;

  useEffect(() => {
    if (config && !allocs.length) {
      const st = config.strategies.find((x) => x.id === strategy) || config.strategies[2];
      setAllocs(Object.entries(st.weights).map(([asset, weight_bp]) => ({ asset, weight_bp })).sort((a, b) => b.weight_bp - a.weight_bp));
    }
  }, [config, strategy, allocs.length]);
  useEffect(() => {
    if (cards.data && cardId === null) setCardId(cards.data.cards.find((c) => c.is_default)?.id ?? cards.data.cards[0]?.id ?? null);
  }, [cards.data, cardId]);

  const amountKop = parseKop(amount);
  const goalKop = goal ? parseKop(goal) : 0;
  const monthly = isFinite(amountKop) ? amountKop * PERIODS_PER_MONTH[sched.frequency] : 0;
  const risk = config ? planRisk(allocs, config.assets) : 1;
  const tooRisky = !!me?.user?.risk_profile && risk > (MAX_RISK[me.user.risk_profile] || 5);
  const sum = allocs.reduce((x, a) => x + a.weight_bp, 0);
  const minK = config?.min_contrib_kop ?? 500, maxK = config?.max_contrib_kop ?? 500000;
  const amountErr = !isFinite(amountKop) ? "Введите сумму" : amountKop < minK ? `Минимум ${byn(minK)}` : amountKop > maxK ? `Максимум ${byn(maxK)}` : null;

  const stepsOk = [
    name.trim().length > 0 && (!goal || (isFinite(goalKop) && goalKop >= 1000)),
    !amountErr,
    sum === 10000 && (strategy !== "custom" || isPro),
    true,
  ];
  const eta = useMemo(() => {
    if (!goalKop || !monthly) return null;
    const months = Math.ceil(goalKop / monthly);
    return months <= 600 ? months : null;
  }, [goalKop, monthly]);

  const create = async (ack = false) => {
    try {
      const r = await s.submit(() => api.post<{ plan: Plan; topup_error: string }>("/plans", {
        name: name.trim(), icon, goal_kop: goalKop || undefined, goal_date: goalDate || undefined, amount_kop: amountKop,
        frequency: sched.frequency, weekday: sched.weekday, monthday: sched.monthday, run_minute: sched.run_minute,
        strategy, allocations: strategy === "custom" ? allocs : undefined, card_id: cardId ?? undefined, dip_enabled: dip,
        first_topup: topup && !!cardId, risk_ack: ack,
      }));
      if (!r) return;
      reload();
      if (r.topup_error) toast("Копилка создана, но первое пополнение не прошло: " + r.topup_error, "error");
      else toast(topup && cardId ? `Копилка «${r.plan.name}» создана и пополнена` : `Копилка «${r.plan.name}» создана`);
      nav(`/app/plans/${r.plan.id}`, { replace: true });
    } catch (e) {
      if (e instanceof ApiError && e.code === "risk_ack_required") setRiskAsk(true);
      else if (e instanceof ApiError && e.code === "plan_limit") setStep(3);
    }
  };

  const titles = ["На что копим", "Сколько и когда", "Во что вкладывать", "Карта и запуск"];
  return (
    <div className="wizard">
      <Link to="/app/plans" className="back"><ArrowLeft />Копилки</Link>
      <h1>Новая копилка</h1>
      <div className="wizard-steps" aria-hidden>{titles.map((_, i) => <span key={i} className={i <= step ? "on" : ""} />)}</div>
      <section className="sheet">
        <div className="sheet-head"><h2>{titles[step]}</h2><span className="small muted">Шаг {step + 1} из 4</span></div>
        <div className="sheet-body stack g24">
          {step === 0 && (
            <>
              <Field label="Название" htmlFor="pn">
                <Input id="pn" maxLength={40} value={name} onChange={(e) => setName(e.target.value)} placeholder="Например, «На отпуск в Грузию»" autoFocus />
                <div className="row g8 wrap mt8">
                  {NAME_HINTS.map(([n, ic]) => <button key={n} type="button" className="chip" aria-pressed={name === n} onClick={() => { setName(n); setIcon(ic); }}>{n}</button>)}
                </div>
              </Field>
              <Field label="Значок">
                <div className="icon-pick">
                  {Object.entries(PLAN_ICONS).map(([k, I]) => <button key={k} type="button" aria-pressed={icon === k} aria-label={k} onClick={() => setIcon(k)}><I /></button>)}
                </div>
              </Field>
              <div className="grid2">
                <Field label="Цель, ⃅" hint="Необязательно — покажем прогресс и прогноз" error={goal && (!isFinite(goalKop) || goalKop < 1000) ? "Не меньше 10 ⃅" : null}>
                  <div className="input-affix"><Input inputMode="decimal" value={goal} onChange={(e) => setGoal(e.target.value)} placeholder="3 000" /><span className="affix">⃅</span></div>
                </Field>
                <Field label="Хочу накопить к" hint="Необязательно">
                  <Input type="date" value={goalDate} min={new Date(Date.now() + 86400000).toISOString().slice(0, 10)} onChange={(e) => setGoalDate(e.target.value)} />
                </Field>
              </div>
            </>
          )}
          {step === 1 && (
            <>
              <Field label="Сумма одного пополнения" error={amount ? amountErr : null}>
                <div className="input-affix"><input className="input amount-input" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} aria-label="Сумма в рублях" autoFocus /><span className="affix" style={{ fontSize: 18 }}>⃅</span></div>
                <div className="row g8 wrap mt8">{AMOUNTS.map((a) => <button key={a} type="button" className="chip" aria-pressed={amountKop === a} onClick={() => setAmount(String(a / 100))}>{kop(a).replace(",00", "")} ⃅</button>)}</div>
              </Field>
              <SchedulePicker value={sched} onChange={setSched} />
              {sched.frequency !== "manual" && !amountErr && (
                <div className="notice">
                  <div>
                    В месяц около <b>{byn(Math.round(monthly))}</b>, за год — <b>{byn(Math.round(monthly * 12))}</b>.
                    {eta && <> Цель {byn(goalKop)} без учёта роста — примерно через <b>{eta} мес.</b></>}
                  </div>
                </div>
              )}
            </>
          )}
          {step === 2 && config && (
            <>
              <StrategyPicker strategy={strategy} allocations={allocs} isPro={isPro} onChange={(st, a) => { setStrategy(st); setAllocs(a); }} />
              {tooRisky && <Notice kind="warn" title={`Риск ${risk} из 5 выше вашего профиля`}>По анкете вам подходит более спокойная стратегия. Можно продолжить — мы попросим подтвердить, что вы понимаете риск.</Notice>}
              <div className="setting-row" style={{ borderTop: "1px solid var(--line-2)", paddingTop: 16 }}>
                <div>
                  <div style={{ fontWeight: 560 }}>Докупать при падении цены</div>
                  <div className="small muted">Если актив упадёт за сутки больше чем на 5%, копилка сделает дополнительное пополнение{isPro ? "" : " · доступно в Pro"}</div>
                </div>
                <Switch label="Докупать при падении" checked={dip} disabled={!isPro} onChange={setDip} />
              </div>
            </>
          )}
          {step === 3 && (
            <>
              <Field label="С какой карты пополнять">
                {cards.data?.cards.length ? (
                  <div className="stack g8">
                    {cards.data.cards.map((c) => (
                      <button key={c.id} type="button" className="choice row between" aria-pressed={cardId === c.id} onClick={() => setCardId(c.id)}>
                        <span><b>{c.brand.toUpperCase()} •• {c.last4}</b> <span className="muted small">до {String(c.exp_month).padStart(2, "0")}/{String(c.exp_year).slice(-2)}</span></span>
                        {c.is_default && <span className="badge">Основная</span>}
                      </button>
                    ))}
                  </div>
                ) : (
                  <Notice kind="warn">Карта не привязана — копилка будет создана на паузе. <Link to="/app/cards">Привязать карту</Link></Notice>
                )}
              </Field>
              <dl className="kv">
                <dt>Копилка</dt><dd>{name}{goalKop ? ` · цель ${byn(goalKop)}` : ""}</dd>
                <dt>Пополнение</dt><dd>{byn(amountKop)} · {sched.frequency === "manual" ? "вручную" : freqLabel[sched.frequency].toLowerCase()}</dd>
                <dt>Состав</dt><dd>{allocs.map((a) => `${a.asset} ${a.weight_bp / 100}%`).join(" · ")}</dd>
                <dt>Комиссия</dt><dd>{((isPro ? config!.fee_pro_bp : config!.fee_free_bp) / 100).toString().replace(".", ",")}% с пополнения, спред {(config!.spread_bp / 100).toString().replace(".", ",")}%</dd>
              </dl>
              {cardId && (
                <label className="check"><input type="checkbox" checked={topup} onChange={(e) => setTopup(e.target.checked)} /><span>Пополнить сразу на {byn(amountKop)}, не дожидаясь расписания</span></label>
              )}
              {s.error && !riskAsk && <Notice kind="error">{s.error}</Notice>}
            </>
          )}
        </div>
        <div className="sheet-foot row between">
          {step > 0 ? <Button variant="ghost" onClick={() => setStep(step - 1)}>Назад</Button> : <span />}
          {step < 3 ? (
            <Button variant="primary" disabled={!stepsOk[step]} onClick={() => setStep(step + 1)}>Дальше</Button>
          ) : (
            <Button variant="primary" busy={s.busy} onClick={() => create(false)}>{cardId ? "Запустить копилку" : "Создать копилку"}</Button>
          )}
        </div>
      </section>
      <Modal open={riskAsk} onClose={() => setRiskAsk(false)} title="Подтвердите риск">
        <div className="stack g16">
          <p>Состав копилки — риск {risk} из 5. Это выше, чем подходит вам по анкете. Стоимость таких активов может за несколько месяцев снизиться на 50% и больше.</p>
          <div className="row g8" style={{ justifyContent: "flex-end" }}>
            <Button onClick={() => { setRiskAsk(false); setStep(2); }}>Изменить состав</Button>
            <Button variant="primary" busy={s.busy} onClick={() => { setRiskAsk(false); create(true); }}>Понимаю риск, создать</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
