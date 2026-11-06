import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../lib/api";
import { byn, dateNum, kop, parseKop, pct } from "../../lib/format";
import { useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { BacktestResult } from "../../lib/types";
import { Field, Input, Notice, Segmented, Skeleton } from "../../components/ui";
import { CompareChart } from "../../components/charts";

const YEARS = [1, 2, 3, 5, 7];

export default function Calculator({ publicPage }: { publicPage?: boolean }) {
  useTitle("Калькулятор");
  const { config, me } = useSession();
  const [pick, setPick] = useState("balanced");
  const [amount, setAmount] = useState("20");
  const [freq, setFreq] = useState<"daily" | "weekly" | "monthly">("weekly");
  const [years, setYears] = useState(3);
  const [dep, setDep] = useState("9");
  const [res, setRes] = useState<BacktestResult | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const options = useMemo(() => {
    if (!config) return [];
    return [
      ...config.strategies.map((s) => ({ id: s.id, label: s.name, weights: s.weights })),
      ...config.assets.filter((a) => a.kind !== "stable").map((a) => ({ id: "a:" + a.symbol, label: "Только " + a.symbol, weights: { [a.symbol]: 10000 } })),
    ];
  }, [config]);
  const opt = options.find((o) => o.id === pick);
  const k = parseKop(amount);

  useEffect(() => {
    if (!opt || !isFinite(k) || k < 100) return;
    const t = setTimeout(async () => {
      setBusy(true);
      setErr(null);
      try {
        setRes(await api.post<BacktestResult>("/calculator/backtest", { weights: opt.weights, amount_kop: k, frequency: freq, years, deposit_rate: Number(dep.replace(",", ".")) || 9 }));
      } catch (e: any) {
        setErr(e.message);
      } finally {
        setBusy(false);
      }
    }, 300);
    return () => clearTimeout(t);
  }, [opt, k, freq, years, dep]);

  const series = useMemo(() => res ? [
    { name: "Копилка", color: "#2340d8", data: res.points.map((p) => ({ t: p.t, v: p.value_kop })) },
    { name: "Доллары", color: "#0a7a4b", data: res.points.map((p) => ({ t: p.t, v: p.usd_kop })) },
    { name: "Депозит", color: "#c9a227", data: res.points.map((p) => ({ t: p.t, v: p.deposit_kop })) },
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
          <p>Что было бы, если копить по плану последние годы. Реальные дневные цены бирж и официальный курс НБРБ, с комиссией сервиса и спредом.</p>
        </div>
      </div>
      <div className="home-grid" style={{ gridTemplateColumns: "minmax(0, 340px) minmax(0, 1fr)" }}>
        <section className="sheet sheet-pad stack g16">
          <Field label="Во что">
            <select className="select" value={pick} onChange={(e) => setPick(e.target.value)}>
              <optgroup label="Стратегии">{options.filter((o) => !o.id.startsWith("a:")).map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}</optgroup>
              <optgroup label="Один актив">{options.filter((o) => o.id.startsWith("a:")).map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}</optgroup>
            </select>
          </Field>
          <Field label="Сумма пополнения" error={!isFinite(k) || k < 100 ? "От 1 ⃅" : null}>
            <div className="input-affix"><Input inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} /><span className="affix">⃅</span></div>
          </Field>
          <Field label="Как часто">
            <Segmented label="Частота" value={freq} onChange={setFreq} options={[{ value: "daily", label: "День" }, { value: "weekly", label: "Неделя" }, { value: "monthly", label: "Месяц" }]} />
          </Field>
          <Field label="Сколько лет назад начать">
            <div className="row g8 wrap">{YEARS.map((y) => <button key={y} className="chip" aria-pressed={years === y} onClick={() => setYears(y)}>{y} {y === 1 ? "год" : y < 5 ? "года" : "лет"}</button>)}</div>
          </Field>
          <Field label="Ставка депозита для сравнения" hint="Годовая, в белорусских рублях">
            <div className="input-affix"><Input inputMode="decimal" value={dep} onChange={(e) => setDep(e.target.value)} /><span className="affix">%</span></div>
          </Field>
        </section>
        <section className="sheet">
          <div className="sheet-body stack g16">
            {err && <Notice kind="error">{err}</Notice>}
            {!res && !err && <Skeleton h={300} />}
            {res && (
              <>
                {res.note && <Notice kind="info">{res.note}</Notice>}
                <div className="grid3">
                  <div><div className="small muted">Вложили бы</div><div className="money-lg num">{byn(res.invested_kop)}</div><div className="tiny muted">{res.purchases} пополнений с {dateNum(new Date(res.from).getTime() / 1000)}</div></div>
                  <div><div className="small muted">Было бы сейчас</div><div className={`money-lg num ${profit >= 0 ? "up" : "down"}`}>{byn(res.value_kop)}</div><div className="tiny"><span className={profit >= 0 ? "up" : "down"}>{kop(profit, { sign: true })} ⃅ · {pct(res.profit_pct)}</span></div></div>
                  <div><div className="small muted">Худшая просадка</div><div className="money-lg num down">{pct(res.max_drawdown)}</div><div className="tiny muted">от пика стоимости по пути</div></div>
                </div>
                <div style={{ opacity: busy ? 0.5 : 1, transition: "opacity .15s" }}>
                  <div className="row g16 wrap small" style={{ marginBottom: 8 }}>
                    {series.map((s) => <span key={s.name} className="row g8"><span className="dot" style={{ background: s.color }} />{s.name}</span>)}
                  </div>
                  <CompareChart series={series} />
                </div>
                <div className="grid2 small">
                  <div className="notice">Те же деньги в наличных долларах по курсу НБРБ: <b>{byn(res.usd_kop)}</b></div>
                  <div className="notice">На депозите под {dep}% годовых: <b>{byn(res.deposit_kop)}</b></div>
                </div>
                <div className="row between wrap g12">
                  <span className="tiny muted">Прошлая доходность не гарантирует будущую. {res.rates_history ? "" : "История курса НБРБ недоступна — использован текущий курс."}</span>
                  <Link to={startTo} className="btn btn-primary">{publicPage && !me?.user ? "Начать копить" : "Создать такую копилку"}</Link>
                </div>
              </>
            )}
          </div>
        </section>
      </div>
    </div>
  );
}
