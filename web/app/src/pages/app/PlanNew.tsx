import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { api, ApiError } from "../../lib/api";
import { byn, freqLabel, kop, minuteStr, parseKop, plural, WD_SHORT, planName } from "../../lib/format";
import { useLoad, useSubmit, useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { Alloc, Card, Plan, Strategy } from "../../lib/types";
import { AssetIcon, Button, Field, Input, Modal, Notice, PLAN_ICONS, Segmented, Switch, useToast } from "../../components/ui";
import { PlanChip, PlanTile } from "../../components/plans";
import { findBank } from "../../lib/banks";
import { addMonths, DatePicker, isoDate, longDate, parseIso } from "../../components/DatePicker";

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

export function StrategyPicker({ strategy, allocations, onChange, isPro, compact }: { strategy: string; allocations: Alloc[]; onChange: (s: string, a: Alloc[]) => void; isPro: boolean; compact?: boolean }) {
  const { config } = useSession();
  const [mode, setMode] = useState<"ready" | "custom">(strategy === "custom" ? "custom" : "ready");
  const sum = allocations.reduce((s, a) => s + a.weight_bp, 0);
  const toggle = (sym: string) => {
    const has = allocations.find((a) => a.asset === sym);
    onChange("custom", has ? allocations.filter((a) => a.asset !== sym) : [...allocations, { asset: sym, weight_bp: 1000 }]);
  };
  const setW = (sym: string, w: number) => onChange("custom", allocations.map((a) => (a.asset === sym ? { ...a, weight_bp: Math.max(500, Math.min(10000, Math.round(w / 100) * 100)) } : a)));
  const choose = (st: Strategy) => onChange(st.id, Object.entries(st.weights).map(([asset, weight_bp]) => ({ asset, weight_bp })).sort((a, b) => b.weight_bp - a.weight_bp));
  return (
    <div className="stack g16">
      <Segmented fill label="Тип состава" value={mode} onChange={(m) => {
        setMode(m);
        if (m === "ready") choose(config!.strategies.find((x) => x.id === "balanced") || config!.strategies[0]);
        else onChange("custom", allocations.length ? allocations : [{ asset: "USDT", weight_bp: 5000 }, { asset: "BTC", weight_bp: 5000 }]);
      }} options={[{ value: "ready", label: "Готовые планы" }, { value: "custom", label: "Свой состав" }]} />
      {mode === "ready" ? (compact ? (
        <div className="plan-chips">
          {config?.strategies.map((st) => <PlanChip key={st.id} s={st} selected={strategy === st.id} onSelect={() => choose(st)} />)}
        </div>
      ) : (
        <div className="plan-tiles">
          {config?.strategies.map((st) => <PlanTile key={st.id} s={st} selected={strategy === st.id} onSelect={() => choose(st)} />)}
        </div>
      )
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
              <div className="input-affix"><input className="input" style={{ height: 36, paddingRight: 30 }} inputMode="numeric" maxLength={3} value={a.weight_bp / 100} onChange={(e) => setW(a.asset, Number(e.target.value.replace(/\D/g, "").slice(0, 3)) * 100)} aria-label={`Доля ${a.asset} в процентах`} /><span className="affix">%</span></div>
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
const GOAL_PRESETS = [{ label: "Через полгода", months: 6 }, { label: "Через год", months: 12 }, { label: "Через 2 года", months: 24 }, { label: "Через 3 года", months: 36 }, { label: "Через 5 лет", months: 60 }];
const GOAL_MAX_KOP = 100_000_000; // 1 000 000 ⃅ — как на сервере

/** Сколько пополнений успеет пройти до даты при такой частоте (приблизительно — точный прогноз считает сервер). */
function runsUntil(freq: Schedule["frequency"], dateIso: string) {
  const d = parseIso(dateIso);
  if (!d) return 0;
  const days = Math.floor((d.getTime() - Date.now()) / 86400000);
  if (days < 1) return 0;
  switch (freq) {
    case "daily": return days;
    case "weekly": return Math.max(1, Math.floor(days / 7));
    case "biweekly": return Math.max(1, Math.floor(days / 14));
    case "monthly": return Math.max(1, Math.floor(days / 30.44));
    default: return 0;
  }
}

/** Сумма одного пополнения, чтобы к дате накопить цель (с учётом комиссии, без учёта роста), с округлением вверх до рубля. */
function needPerRun(goalKop: number, runs: number, feeBp: number) {
  if (!runs) return 0;
  return Math.ceil(goalKop / (1 - feeBp / 10000) / runs / 100) * 100;
}

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
  const [touched, setTouched] = useState(!!sp.get("amount"));
  const [sched, setSched] = useState<Schedule>({ frequency: (sp.get("freq") as Schedule["frequency"]) || "weekly", weekday: 5, monthday: 1, run_minute: 600 });
  const [autoFreq, setAutoFreq] = useState(!sp.get("freq"));
  const [strategy, setStrategy] = useState(sp.get("strategy") || "balanced");
  const [allocs, setAllocs] = useState<Alloc[]>([]);
  const [dip, setDip] = useState(false);
  const [cardId, setCardId] = useState<number | null>(null);
  const [topup, setTopup] = useState(true);
  const [riskAsk, setRiskAsk] = useState(false);
  const s = useSubmit();
  const isPro = !!me?.is_pro;
  const feeBp = (isPro ? config?.fee_pro_bp : config?.fee_free_bp) ?? 100;

  useEffect(() => {
    if (config && !allocs.length) {
      const st = config.strategies.find((x) => x.id === strategy) || config.strategies.find((x) => x.id === "balanced") || config.strategies[0];
      setAllocs(Object.entries(st.weights).map(([asset, weight_bp]) => ({ asset, weight_bp })).sort((a, b) => b.weight_bp - a.weight_bp));
    }
  }, [config, strategy, allocs.length]);
  useEffect(() => {
    if (cards.data && cardId === null) setCardId(cards.data.cards.find((c) => c.is_default)?.id ?? cards.data.cards[0]?.id ?? null);
  }, [cards.data, cardId]);

  const amountKop = parseKop(amount);
  const goalKop = goal ? parseKop(goal) : 0;
  const minK = config?.min_contrib_kop ?? 500, maxK = config?.max_contrib_kop ?? 500000;
  const goalErr = goal && (!isFinite(goalKop) || goalKop < 1000) ? "Не меньше 10 ⃅" : goalKop > GOAL_MAX_KOP ? "Не больше 1 000 000 ⃅" : null;

  // Цель и срок заданы — система сама подбирает частоту и сумму; человек может поменять их вручную.
  const plan = useMemo(() => {
    if (!goalKop || goalErr || !goalDate) return null;
    const order: Schedule["frequency"][] = autoFreq ? ["weekly", "monthly", "daily"] : [sched.frequency];
    for (const f of order) {
      const runs = runsUntil(f, goalDate);
      const need = needPerRun(goalKop, runs, feeBp);
      if (!runs) continue;
      if (need >= minK && need <= maxK) return { freq: f, runs, need, ok: true };
      if (need < minK && (f === "monthly" || !autoFreq)) return { freq: f, runs, need: minK, ok: true };
    }
    const f = autoFreq ? "weekly" : sched.frequency;
    const runs = runsUntil(f, goalDate);
    return { freq: f, runs, need: needPerRun(goalKop, runs, feeBp), ok: false };
  }, [goalKop, goalErr, goalDate, autoFreq, sched.frequency, feeBp, minK, maxK]);

  useEffect(() => {
    if (step !== 2 || !plan || !plan.ok) return;
    if (autoFreq && sched.frequency !== plan.freq) setSched((x) => ({ ...x, frequency: plan.freq }));
    if (!touched) setAmount(String(plan.need / 100));
  }, [step, plan, autoFreq, touched, sched.frequency]);

  const monthly = isFinite(amountKop) ? amountKop * PERIODS_PER_MONTH[sched.frequency] : 0;
  const risk = config ? (config.strategies.find((x) => x.id === strategy)?.risk ?? planRisk(allocs, config.assets)) : 1;
  const tooRisky = !!me?.user?.risk_profile && risk > (MAX_RISK[me.user.risk_profile] || 5);
  const sum = allocs.reduce((x, a) => x + a.weight_bp, 0);
  const amountErr = !isFinite(amountKop) ? "Введите сумму" : amountKop < minK ? `Минимум ${byn(minK)}` : amountKop > maxK ? `Максимум ${byn(maxK)}` : null;

  // прогноз: к какой дате набежит цель при выбранной сумме (без учёта роста)
  const eta = useMemo(() => {
    if (!goalKop || goalErr || !isFinite(amountKop) || amountErr || sched.frequency === "manual") return null;
    const runs = Math.ceil(goalKop / (amountKop * (1 - feeBp / 10000)));
    const days = runs * ({ daily: 1, weekly: 7, biweekly: 14, monthly: 30.44 } as Record<string, number>)[sched.frequency];
    if (days > 365 * 30) return "more";
    return isoDate(new Date(Date.now() + days * 86400000));
  }, [goalKop, goalErr, amountKop, amountErr, sched.frequency, feeBp]);

  const stepsOk = [
    name.trim().length > 0 && !goalErr,
    sum === 10000 && (strategy !== "custom" || isPro),
    !amountErr,
    true,
  ];

  const create = async (ack = false) => {
    try {
      const r = await s.submit(() => api.post<{ plan: Plan; topup_error: string; waitlist?: { position: number; first?: boolean } }>("/plans", {
        name: name.trim(), icon, goal_kop: goalKop || undefined, goal_date: goalDate || undefined, amount_kop: amountKop,
        frequency: sched.frequency, weekday: sched.weekday, monthday: sched.monthday, run_minute: sched.run_minute,
        strategy, allocations: strategy === "custom" ? allocs : undefined, card_id: cardId ?? undefined, dip_enabled: dip,
        first_topup: topup && !!cardId, risk_ack: ack,
      }));
      if (!r) return;
      reload();
      if (r.waitlist) {
        // до запуска: копилка сохранена и ждёт, человек — в листе ожидания (окно с демо-счётом)
        nav(`/app/plans/${r.plan.id}`, { replace: true });
        window.dispatchEvent(new CustomEvent("ai:waitlist", { detail: { position: r.waitlist.position, first: r.waitlist.first } }));
        return;
      }
      if (r.topup_error) toast("Копилка создана, но первое пополнение не прошло: " + r.topup_error, "error");
      else toast(topup && cardId ? `Копилка «${r.plan.name}» создана и пополнена` : `Копилка «${r.plan.name}» создана`);
      nav(`/app/plans/${r.plan.id}`, { replace: true });
    } catch (e) {
      if (e instanceof ApiError && e.code === "risk_ack_required") setRiskAsk(true);
      else if (e instanceof ApiError && e.code === "plan_limit") setStep(3);
    }
  };

  const tomorrow = isoDate(new Date(Date.now() + 86400000));
  const maxDate = isoDate(addMonths(new Date(), 12 * 30));
  const st = config?.strategies.find((x) => x.id === strategy);
  // до запуска (лист ожидания) настоящий аккаунт не привязывает карту на этом шаге
  const waiting = config?.launch === "waitlist" && !me?.demo;
  const titles = ["На что копим", "Какой план", "Сколько и когда", waiting ? "Проверим и запустим" : "Карта и запуск"];
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
                <Field label="Сколько хочу накопить" hint="Необязательно" error={goalErr}>
                  <div className="input-affix"><Input inputMode="decimal" maxLength={12} value={goal} onChange={(e) => { setGoal(e.target.value); setTouched(false); }} placeholder="3 000" /><span className="affix">⃅</span></div>
                </Field>
                <Field label="К какой дате" hint={goalKop ? "Подберём сумму пополнения под срок" : "Необязательно"}>
                  <DatePicker value={goalDate} onChange={(v) => { setGoalDate(v); setTouched(false); setAutoFreq(true); }} min={tomorrow} max={maxDate} presets={GOAL_PRESETS} placeholder="Без срока" />
                </Field>
              </div>
            </>
          )}
          {step === 1 && config && (
            <>
              <StrategyPicker strategy={strategy} allocations={allocs} isPro={isPro} onChange={(stId, a) => { setStrategy(stId); setAllocs(a); }} />
              {tooRisky && <Notice kind="warn" title={`Риск ${risk} из 5 выше вашего профиля`}>По анкете вам подходит более спокойный план. Можно продолжить — мы попросим подтвердить, что вы понимаете риск.</Notice>}
              <div className="setting-row" style={{ borderTop: "1px solid var(--line-2)", paddingTop: 16 }}>
                <div>
                  <div style={{ fontWeight: 560 }}>Докупать при падении цены</div>
                  <div className="small muted">Если актив упадёт за сутки больше чем на 5%, копилка сделает дополнительное пополнение{isPro ? "" : " · доступно в Pro"}</div>
                </div>
                <Switch label="Докупать при падении" checked={dip} disabled={!isPro} onChange={setDip} />
              </div>
            </>
          )}
          {step === 2 && (
            <>
              {plan && plan.ok && (
                <div className="suggest">
                  <div className="suggest-t">Чтобы к {longDate(goalDate)} накопить {byn(goalKop).replace(",00", "")}, достаточно</div>
                  <div className="suggest-v num">{byn(plan.need).replace(",00", "")} <span>{freqLabel[plan.freq].toLowerCase()}</span></div>
                  <div className="small muted">{plan.runs} {plural(plan.runs, "пополнение", "пополнения", "пополнений")} с учётом комиссии, без учёта роста{st ? ` · ${planName(st.name)}` : ""}. Сумму и частоту можно поменять.</div>
                </div>
              )}
              {plan && !plan.ok && (
                <Notice kind="warn" title="Цель слишком большая для такого срока">Понадобится {byn(plan.need)} за пополнение — больше лимита {byn(maxK)}. Выберите срок подальше или сумму поменьше.</Notice>
              )}
              <Field label="Сумма одного пополнения" error={amount ? amountErr : null}>
                <div className="input-affix"><input className="input amount-input" inputMode="decimal" maxLength={10} value={amount} onChange={(e) => { setAmount(e.target.value); setTouched(true); }} aria-label="Сумма в рублях" /><span className="affix" style={{ fontSize: 18 }}>⃅</span></div>
                <div className="row g8 wrap mt8">{AMOUNTS.map((a) => <button key={a} type="button" className="chip" aria-pressed={amountKop === a} onClick={() => { setAmount(String(a / 100)); setTouched(true); }}>{kop(a).replace(",00", "")} ⃅</button>)}</div>
              </Field>
              <SchedulePicker value={sched} onChange={(v) => { if (v.frequency !== sched.frequency) setAutoFreq(false); setSched(v); }} />
              {sched.frequency !== "manual" && !amountErr && (
                <div className="notice">
                  <div>
                    В месяц около <b>{byn(Math.round(monthly))}</b>, за год — <b>{byn(Math.round(monthly * 12))}</b>.
                    {eta === "more" && goalKop > 0 && <> С такой суммой цель {byn(goalKop)} займёт больше 30 лет — увеличьте пополнение.</>}
                    {eta && eta !== "more" && <> Цель {byn(goalKop).replace(",00", "")} — примерно к <b>{longDate(eta)}</b>{goalDate && eta > goalDate ? ", позже срока" : ""}.</>}
                  </div>
                </div>
              )}
            </>
          )}
          {step === 3 && (
            <>
              {!waiting && <Field label="С какой карты пополнять">
                {cards.data?.cards.length ? (
                  <div className="stack g8">
                    {cards.data.cards.map((c) => (
                      <button key={c.id} type="button" className="choice row between" aria-pressed={cardId === c.id} onClick={() => setCardId(c.id)}>
                        <span><b>{findBank(c.issuer)?.name || c.issuer || c.brand.toUpperCase()} •• {c.last4}</b> <span className="muted small">{c.brand.toUpperCase()}, до {String(c.exp_month).padStart(2, "0")}/{String(c.exp_year).slice(-2)}</span></span>
                        {c.is_default && <span className="badge">Основная</span>}
                      </button>
                    ))}
                  </div>
                ) : (
                  <Notice kind="warn">Карта не привязана — копилка будет создана на паузе. <Link to="/app/cards">Привязать карту</Link></Notice>
                )}
              </Field>}
              <dl className="kv">
                <dt>Копилка</dt><dd>{name}{goalKop ? ` · цель ${byn(goalKop)}` : ""}</dd>
                <dt>Пополнение</dt><dd>{byn(amountKop)} · {sched.frequency === "manual" ? "вручную" : freqLabel[sched.frequency].toLowerCase()}</dd>
                <dt>План</dt><dd>{st ? planName(st.name) : "Свой состав"} · {allocs.map((a) => `${config?.assets.find((x) => x.symbol === a.asset)?.name || a.asset} ${a.weight_bp / 100}%`).join(" · ")}</dd>
                <dt>Комиссия</dt><dd>{((isPro ? config!.fee_pro_bp : config!.fee_free_bp) / 100).toString().replace(".", ",")}% с пополнения, спред {(config!.spread_bp / 100).toString().replace(".", ",")}%</dd>
              </dl>
              {cardId && !waiting && (
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
            <Button variant="primary" busy={s.busy} onClick={() => create(false)}>{cardId || waiting ? "Запустить копилку" : "Создать копилку"}</Button>
          )}
        </div>
      </section>
      <Modal open={riskAsk} onClose={() => setRiskAsk(false)} title="Подтвердите риск">
        <div className="stack g16">
          <p>Состав копилки — риск {risk} из 5. Это выше, чем подходит вам по анкете. Стоимость таких активов может за несколько месяцев снизиться на 50% и больше.</p>
          <div className="row g8" style={{ justifyContent: "flex-end" }}>
            <Button onClick={() => { setRiskAsk(false); setStep(1); }}>Изменить план</Button>
            <Button variant="primary" busy={s.busy} onClick={() => { setRiskAsk(false); create(true); }}>Понимаю риск, создать</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
