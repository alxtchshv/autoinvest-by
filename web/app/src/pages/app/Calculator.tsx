import { useMemo, useState } from "react";
import { TrendingUp } from "lucide-react";
import { Link } from "react-router-dom";
import { api } from "../../lib/api";
import { byn, dateNum, kop, parseKop, pct, planName, plural } from "../../lib/format";
import { useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { BacktestResult } from "../../lib/types";
import { Button, Field, Input, Notice, Segmented } from "../../components/ui";
import { CompareChart } from "../../components/charts";

const YEARS = [1, 2, 3, 5, 7];

type Params = { pick: string; k: number; freq: "daily" | "weekly" | "monthly"; years: number; dep: number | null };

export default function Calculator({ publicPage }: { publicPage?: boolean }) {
  useTitle("Калькулятор");
  const { config, me } = useSession();
  const [pick, setPick] = useState("balanced");
  const [amount, setAmount] = useState("20");
  const [freq, setFreq] = useState<Params["freq"]>("weekly");
  const [years, setYears] = useState(3);
  const [withDep, setWithDep] = useState(false);
  const [dep, setDep] = useState("9");
  const [res, setRes] = useState<BacktestResult | null>(null);
  const [done, setDone] = useState<Params | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const options = useMemo(() => {
    if (!config) return [];
    return [
      ...config.strategies.map((s) => ({ id: s.id, label: planName(s.name), weights: s.weights, group: "plan" })),
      ...config.assets.filter((a) => a.kind !== "stable" && a.kind !== "bond" && !a.pro_only).map((a) => ({ id: "a:" + a.symbol, label: a.name, weights: { [a.symbol]: 10000 }, group: "asset" })),
    ];
  }, [config]);
  const opt = options.find((o) => o.id === pick);
  const k = parseKop(amount);
  const depRate = Number(dep.replace(",", "."));
  const amountErr = !isFinite(k) || k < 100 ? "От 1 ⃅" : k > 10_000_000 ? "Не больше 100 000 ⃅" : null;
  const depErr = withDep && (!isFinite(depRate) || depRate <= 0 || depRate > 30) ? "От 0,1 до 30%" : null;
  const params: Params = { pick, k, freq, years, dep: withDep ? depRate : null };
  const stale = !!done && JSON.stringify(done) !== JSON.stringify(params);

  const run = async () => {
    if (!opt || amountErr || depErr) return;
    setBusy(true);
    setErr(null);
    try {
      setRes(await api.post<BacktestResult>("/calculator/backtest", { weights: opt.weights, amount_kop: k, frequency: freq, years, deposit_rate: withDep ? depRate : 9 }));
      setDone(params);
    } catch (e: any) {
      setErr(e.message);
    } finally {
      setBusy(false);
    }
  };

  const series = useMemo(() => res ? [
    { name: "Копилка", color: "#2340d8", data: res.points.map((p) => ({ t: p.t, v: p.value_kop })) },
    { name: "Вложено", color: "#8a919e", dashed: true, data: res.points.map((p) => ({ t: p.t, v: p.invested_kop })) },
  ] : [], [res]);

  const q = `amount=${encodeURIComponent(amount)}&freq=${freq}${pick.startsWith("a:") ? "" : "&strategy=" + pick}`;
  const startTo = me?.user ? `/app/plans/new?${q}` : `/register?next=${encodeURIComponent("/app/plans/new?" + q)}`;
  const profit = res ? res.value_kop - res.invested_kop : 0;
  return (
    <div className="stack g16">
      <div className="page-head" style={{ marginBottom: 0 }}>
        <div>
          <h1>Машина времени</h1>
          <p>Что было бы, если копить по плану последние годы — по реальным ценам и курсу НБРБ.</p>
        </div>
      </div>
      <div className="calc-grid">
        <section className="sheet sheet-pad stack g16">
          <Field label="Во что">
            <select className="select" value={pick} onChange={(e) => setPick(e.target.value)}>
              <optgroup label="Планы">{options.filter((o) => o.group === "plan").map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}</optgroup>
              <optgroup label="Одна монета">{options.filter((o) => o.group === "asset").map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}</optgroup>
            </select>
          </Field>
          <Field label="Сумма пополнения" error={amountErr}>
            <div className="input-affix"><Input inputMode="decimal" maxLength={10} value={amount} onChange={(e) => setAmount(e.target.value)} /><span className="affix">⃅</span></div>
          </Field>
          <Field label="Как часто">
            <Segmented label="Частота" value={freq} onChange={setFreq} options={[{ value: "daily", label: "День" }, { value: "weekly", label: "Неделя" }, { value: "monthly", label: "Месяц" }]} />
          </Field>
          <Field label="Сколько лет назад начать">
            <div className="row g8 wrap">{YEARS.map((y) => <button key={y} type="button" className="chip" aria-pressed={years === y} onClick={() => setYears(y)}>{y} {y === 1 ? "год" : y < 5 ? "года" : "лет"}</button>)}</div>
          </Field>
          <div className="calc-dep">
            <label className="check"><input type="checkbox" checked={withDep} onChange={(e) => setWithDep(e.target.checked)} /><span>Сравнить с депозитом</span></label>
            {withDep && (
              <Field label="Ставка депозита, % годовых" error={depErr}>
                <div className="input-affix"><Input inputMode="decimal" maxLength={5} value={dep} onChange={(e) => setDep(e.target.value)} /><span className="affix">%</span></div>
              </Field>
            )}
          </div>
          <Button variant="primary" size="lg" block busy={busy} disabled={!!amountErr || !!depErr || !opt} onClick={run}>{res && !stale ? "Пересчитать" : "Рассчитать"}</Button>
        </section>
        <section className="sheet">
          <div className="sheet-body stack g16">
            {err && <Notice kind="error">{err}</Notice>}
            {!res && !err && (
              <div className="calc-empty">
                <div className="calc-empty-ic"><TrendingUp /></div>
                <b>Выберите план, сумму и срок</b>
                <span className="muted">и нажмите «Рассчитать» — покажем, сколько было бы сейчас.</span>
              </div>
            )}
            {res && (
              <div className={`stack g16 ${stale ? "calc-stale" : ""}`}>
                {stale && <div className="small muted">Параметры изменились — нажмите «Пересчитать».</div>}
                {res.note && <Notice kind="info">{res.note}</Notice>}
                <div className="calc-result">
                  <div><div className="muted">Вложили бы</div><div className="money-lg num">{byn(res.invested_kop)}</div><div className="small muted">{res.purchases} {plural(res.purchases, "пополнение", "пополнения", "пополнений")} с {dateNum(new Date(res.from).getTime() / 1000)}</div></div>
                  <div><div className="muted">Было бы сейчас</div><div className={`money-lg num ${profit >= 0 ? "up" : "down"}`}>{byn(res.value_kop)}</div><div className={`small ${profit >= 0 ? "up" : "down"}`}>{kop(profit, { sign: true })} ⃅ · {pct(res.profit_pct)}</div></div>
                </div>
                <div>
                  <div className="row g16 wrap small" style={{ marginBottom: 8 }}>
                    {series.map((s) => <span key={s.name} className="row g8"><span className="dot" style={{ background: s.color }} />{s.name}</span>)}
                  </div>
                  <CompareChart series={series} />
                </div>
                {done?.dep != null && (
                  <div className="notice">На депозите под {String(done.dep).replace(".", ",")}% годовых те же пополнения дали бы <b>{byn(res.deposit_kop)}</b>.</div>
                )}
                <div className="row between wrap g12">
                  <span className="small muted">Прошлая доходность не гарантирует будущую. {res.rates_history ? "" : "История курса НБРБ недоступна — использован текущий курс."}</span>
                  <Link to={startTo} className="btn btn-primary">{publicPage && !me?.user ? "Начать копить" : "Создать такую копилку"}</Link>
                </div>
              </div>
            )}
          </div>
        </section>
      </div>
    </div>
  );
}
