import { useEffect, useMemo, useRef, useState } from "react";
import {
  AreaSeries, CandlestickSeries, ColorType, CrosshairMode, HistogramSeries, LineSeries, LineStyle, TickMarkType, createChart,
  type IChartApi, type ISeriesApi, type Time, type UTCTimestamp,
} from "lightweight-charts";
import { api } from "../lib/api";
import { dateNum, kop, price, RUB } from "../lib/format";
import { useMarket } from "../lib/session";
import type { Candle, Snapshot } from "../lib/types";
import { Segmented } from "./ui";

const TZ = "Europe/Minsk";
const fmt = (o: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat("ru-RU", { timeZone: TZ, ...o });
const fDay = fmt({ day: "numeric", month: "short" });
const fMonth = fmt({ month: "short" });
const fYear = fmt({ year: "numeric" });
const fTime = fmt({ hour: "2-digit", minute: "2-digit" });
const fFull = fmt({ day: "numeric", month: "short", year: "numeric", hour: "2-digit", minute: "2-digit" });

const baseOptions = {
  layout: { background: { type: ColorType.Solid, color: "transparent" }, textColor: "#5d6573", fontFamily: "Onest Variable, system-ui, sans-serif", fontSize: 12, attributionLogo: false },
  grid: { vertLines: { visible: false }, horzLines: { color: "#eef0f3" } },
  rightPriceScale: { borderVisible: false, scaleMargins: { top: 0.12, bottom: 0.12 } },
  timeScale: {
    borderVisible: false, rightOffset: 4, fixLeftEdge: true,
    tickMarkFormatter: (t: Time, type: TickMarkType) => {
      const d = new Date((t as number) * 1000);
      if (type === TickMarkType.Year) return fYear.format(d);
      if (type === TickMarkType.Month) return fMonth.format(d).replace(".", "");
      if (type === TickMarkType.DayOfMonth) return fDay.format(d).replace(".", "");
      return fTime.format(d);
    },
  },
  localization: { locale: "ru-RU", priceFormatter: (p: number) => price(p, ""), timeFormatter: (t: Time) => fFull.format(new Date((t as number) * 1000)).replace(".", "") },
  crosshair: { mode: CrosshairMode.Magnet, vertLine: { color: "#b7bdc8", labelBackgroundColor: "#161a23" }, horzLine: { color: "#b7bdc8", labelBackgroundColor: "#161a23" } },
  handleScale: { axisPressedMouseMove: { time: true, price: false } },
};

function useChart(ref: React.RefObject<HTMLDivElement | null>, height: number) {
  const [chart, setChart] = useState<IChartApi | null>(null);
  useEffect(() => {
    if (!ref.current) return;
    const c = createChart(ref.current, { ...baseOptions, height, autoSize: true } as any);
    setChart(c);
    return () => {
      c.remove();
      setChart(null);
    };
  }, [ref, height]);
  return chart;
}

const INTERVALS = [
  { value: "15m", label: "15 мин", limit: 192 },
  { value: "1h", label: "1 ч", limit: 336 },
  { value: "4h", label: "4 ч", limit: 360 },
  { value: "1d", label: "День", limit: 365 },
  { value: "1w", label: "Неделя", limit: 260 },
] as const;
type IntervalV = (typeof INTERVALS)[number]["value"];

/** Биржевой график актива: реальные свечи + доживление последней свечи по SSE. */
export function PriceChart({ symbol, stable }: { symbol: string; stable?: boolean }) {
  const box = useRef<HTMLDivElement>(null);
  const chart = useChart(box, 380);
  const [interval, setIntervalV] = useState<IntervalV>(stable ? "1d" : "1h");
  const [currency, setCurrency] = useState<"BYN" | "USD">("BYN");
  // по умолчанию линия — проще читать; свечи включаются кнопкой, выбор запоминается в этом браузере
  const [kind, setKindV] = useState<"candles" | "line">(() => {
    if (stable) return "line";
    try { return localStorage.getItem("ai_chart_kind") === "candles" ? "candles" : "line"; } catch { return "line"; }
  });
  const setKind = (k: "candles" | "line") => { setKindV(k); try { localStorage.setItem("ai_chart_kind", k); } catch { /* */ } };
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [legend, setLegend] = useState<Candle | null>(null);
  const [last, setLast] = useState<Candle | null>(null);
  const series = useRef<{ main?: ISeriesApi<"Candlestick"> | ISeriesApi<"Area">; vol?: ISeriesApi<"Histogram">; data: Candle[] }>({ data: [] });
  const { quotes } = useMarket();

  useEffect(() => {
    if (!chart) return;
    let cancelled = false;
    setErr(null);
    setLoading(true);
    const lim = INTERVALS.find((i) => i.value === interval)!.limit;
    api.get<{ candles: Candle[] }>(`/market/candles?symbol=${symbol}&interval=${interval}&currency=${currency}&limit=${lim}`)
      .then(({ candles }) => {
        if (cancelled) return;
        setLoading(false);
        // биржа ничего не отдала за этот период — понятное сообщение вместо пустого поля (раньше тут падал график)
        if (!candles?.length) { setErr("Нет данных за этот период — выберите другой интервал"); return; }
        if (series.current.main) chart.removeSeries(series.current.main);
        if (series.current.vol) chart.removeSeries(series.current.vol);
        series.current = { data: candles };
        const pf = { type: "price" as const, precision: currency === "BYN" && candles.at(-1)!.c < 10 ? 4 : 2, minMove: 0.0001 };
        if (kind === "candles") {
          const s = chart.addSeries(CandlestickSeries, { upColor: "#0a7a4b", downColor: "#c2372b", borderVisible: false, wickUpColor: "#0a7a4b", wickDownColor: "#c2372b", priceFormat: pf });
          s.setData(candles.map((c) => ({ time: c.t as UTCTimestamp, open: c.o, high: c.h, low: c.l, close: c.c })));
          series.current.main = s;
        } else {
          const s = chart.addSeries(AreaSeries, { lineColor: "#2340d8", topColor: "rgba(35,64,216,.18)", bottomColor: "rgba(35,64,216,0)", lineWidth: 2, priceFormat: pf });
          s.setData(candles.map((c) => ({ time: c.t as UTCTimestamp, value: c.c })));
          series.current.main = s;
        }
        if (!stable && candles.some((c) => c.v > 0)) {
          const v = chart.addSeries(HistogramSeries, { priceScaleId: "vol", priceFormat: { type: "volume" }, lastValueVisible: false, priceLineVisible: false });
          chart.priceScale("vol").applyOptions({ scaleMargins: { top: 0.84, bottom: 0 } });
          v.setData(candles.map((c) => ({ time: c.t as UTCTimestamp, value: c.v, color: c.c >= c.o ? "rgba(10,122,75,.22)" : "rgba(194,55,43,.22)" })));
          series.current.vol = v;
        }
        chart.timeScale().fitContent();
        setLast(candles.at(-1) || null);
      })
      .catch((e) => { if (!cancelled) { setLoading(false); setErr(e.message || "Не удалось загрузить график"); } });
    return () => {
      cancelled = true;
    };
  }, [chart, symbol, interval, currency, kind, stable]);

  // живое обновление последней свечи
  const q = quotes[symbol];
  useEffect(() => {
    const s = series.current;
    if (!q || !s.main || !s.data.length || stable || q.stale) return;
    const p = currency === "BYN" ? q.price_byn : q.price_usd;
    const lastC = { ...s.data[s.data.length - 1] };
    const step = { "15m": 900, "1h": 3600, "4h": 14400, "1d": 86400, "1w": 604800 }[interval];
    const now = Math.floor(Date.now() / 1000);
    let c: Candle;
    if (now >= lastC.t + step) c = { t: Math.floor(now / step) * step, o: lastC.c, h: Math.max(lastC.c, p), l: Math.min(lastC.c, p), c: p, v: 0 };
    else c = { ...lastC, c: p, h: Math.max(lastC.h, p), l: Math.min(lastC.l, p) };
    if (c.t === lastC.t) s.data[s.data.length - 1] = c;
    else s.data.push(c);
    try {
      if (kind === "candles") (s.main as ISeriesApi<"Candlestick">).update({ time: c.t as UTCTimestamp, open: c.o, high: c.h, low: c.l, close: c.c });
      else (s.main as ISeriesApi<"Area">).update({ time: c.t as UTCTimestamp, value: c.c });
    } catch {
      /* серия пересоздаётся */
    }
    setLast(c);
  }, [q, currency, interval, kind, stable]);

  useEffect(() => {
    if (!chart) return;
    const h = (p: any) => {
      const d = series.current.main && p.seriesData?.get(series.current.main);
      if (!d || p.time === undefined) return setLegend(null);
      const c = series.current.data.find((x) => x.t === p.time);
      setLegend(c || null);
    };
    chart.subscribeCrosshairMove(h);
    return () => chart.unsubscribeCrosshairMove(h);
  }, [chart]);

  const shown = legend || last;
  const cur = currency === "BYN" ? RUB : "$";
  return (
    <div>
      <div className="row between wrap g12" style={{ marginBottom: 12 }}>
        <div className="row g8 wrap">
          {!stable && <Segmented label="Интервал" value={interval} onChange={setIntervalV} options={INTERVALS.map((i) => ({ value: i.value, label: i.label }))} />}
          {!stable && <Segmented label="Вид графика" value={kind} onChange={setKind} options={[{ value: "line", label: "Линия" }, { value: "candles", label: "Свечи" }]} />}
        </div>
        <Segmented label="Валюта" value={currency} onChange={setCurrency} options={[{ value: "BYN", label: RUB }, { value: "USD", label: "$" }]} />
      </div>
      <div className="small muted num" style={{ minHeight: 18, marginBottom: 4 }}>
        {shown && !stable && (
          <>
            {fFull.format(new Date(shown.t * 1000)).replace(".", "")}&nbsp;&nbsp;
            О&nbsp;{price(shown.o, "")} · М&nbsp;{price(shown.h, "")} · Мин&nbsp;{price(shown.l, "")} · З&nbsp;<b style={{ color: "var(--ink)" }}>{price(shown.c, cur)}</b>
          </>
        )}
        {shown && stable && <>{dateNum(shown.t)}: {price(shown.c, cur)} — официальный курс НБРБ, меняется раз в день</>}
      </div>
      <div ref={box} style={{ height: 380, position: "relative" }}>
        {loading && !err && <div className="chart-loading" aria-live="polite"><span className="spinner" />Загружаем график…</div>}
        {err && <div className="notice error" style={{ position: "absolute", inset: "40% 10% auto", zIndex: 2 }}>{err}</div>}
      </div>
    </div>
  );
}

/** Стоимость портфеля/копилки и сколько вложено. */
export function ValueChart({ points, height = 260 }: { points: Snapshot[]; height?: number }) {
  const box = useRef<HTMLDivElement>(null);
  const chart = useChart(box, height);
  const [hover, setHover] = useState<Snapshot | null>(null);
  const refs = useRef<{ v?: ISeriesApi<"Area">; i?: ISeriesApi<"Line"> }>({});
  const pts = useMemo(() => {
    // одна точка на момент времени, по возрастанию
    const m = new Map<number, Snapshot>();
    for (const p of points) m.set(p.t, p);
    return [...m.values()].sort((a, b) => a.t - b.t);
  }, [points]);

  useEffect(() => {
    if (!chart) return;
    const v = chart.addSeries(AreaSeries, { lineColor: "#2340d8", topColor: "rgba(35,64,216,.16)", bottomColor: "rgba(35,64,216,0)", lineWidth: 2, priceFormat: { type: "custom", formatter: (x: number) => kop(x * 100) } });
    const i = chart.addSeries(LineSeries, { color: "#8a919e", lineWidth: 1, lineStyle: LineStyle.Dashed, lastValueVisible: false, priceLineVisible: false, crosshairMarkerVisible: false, priceFormat: { type: "custom", formatter: (x: number) => kop(x * 100) } });
    refs.current = { v, i };
    const h = (p: any) => {
      if (p.time === undefined) return setHover(null);
      setHover(pts.find((x) => x.t === p.time) || null);
    };
    chart.subscribeCrosshairMove(h);
    return () => {
      chart.unsubscribeCrosshairMove(h);
      try {
        chart.removeSeries(v);
        chart.removeSeries(i);
      } catch {
        /* график уже удалён */
      }
    };
  }, [chart, pts]);

  useEffect(() => {
    const { v, i } = refs.current;
    if (!v || !i) return;
    v.setData(pts.map((p) => ({ time: p.t as UTCTimestamp, value: p.value_kop / 100 })));
    i.setData(pts.map((p) => ({ time: p.t as UTCTimestamp, value: p.invested_kop / 100 })));
    chart?.timeScale().applyOptions({ rightOffset: 0 });
    chart?.timeScale().fitContent();
  }, [pts, chart]);

  return (
    <div style={{ position: "relative" }}>
      <div className="row g16 small muted" style={{ minHeight: 20, marginBottom: 6 }}>
        <span className="row g8"><span className="dot" style={{ background: "#2340d8" }} />Стоимость{hover && <b className="num" style={{ color: "var(--ink)" }}>{kop(hover.value_kop)}</b>}</span>
        <span className="row g8"><span className="dot" style={{ background: "#8a919e" }} />Вложено{hover && <b className="num" style={{ color: "var(--ink)" }}>{kop(hover.invested_kop)}</b>}</span>
        {hover && <span className="num">{fFull.format(new Date(hover.t * 1000)).replace(".", "")}</span>}
      </div>
      <div ref={box} style={{ height }} />
    </div>
  );
}

/** Линии сравнения для калькулятора. */
export function CompareChart({ series, height = 320 }: { series: { name: string; color: string; dashed?: boolean; data: { t: number; v: number }[] }[]; height?: number }) {
  const box = useRef<HTMLDivElement>(null);
  const chart = useChart(box, height);
  useEffect(() => {
    if (!chart) return;
    const made = series.map((s) => {
      const l = chart.addSeries(LineSeries, { color: s.color, lineWidth: s.dashed ? 1 : 2, lineStyle: s.dashed ? LineStyle.Dashed : LineStyle.Solid, priceLineVisible: false, lastValueVisible: !s.dashed, title: "", priceFormat: { type: "custom", formatter: (x: number) => kop(Math.round(x * 100)) } });
      l.setData(s.data.map((d) => ({ time: d.t as UTCTimestamp, value: d.v / 100 })));
      return l;
    });
    chart.timeScale().fitContent();
    return () => made.forEach((m) => { try { chart.removeSeries(m); } catch { /* */ } });
  }, [chart, series]);
  return <div ref={box} style={{ height }} />;
}

/** Мини-график для списков (SVG, без библиотеки). */
export function Sparkline({ values, width = 96, height = 32, color }: { values: number[]; width?: number; height?: number; color?: string }) {
  if (values.length < 2) return <svg width={width} height={height} />;
  const min = Math.min(...values), max = Math.max(...values);
  const span = max - min || 1;
  const pts = values.map((v, i) => `${(i / (values.length - 1)) * width},${height - 2 - ((v - min) / span) * (height - 4)}`).join(" ");
  const c = color || (values.at(-1)! >= values[0] ? "#0a7a4b" : "#c2372b");
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} aria-hidden>
      <polyline points={pts} fill="none" stroke={c} strokeWidth="1.6" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}

/** Спарклайн актива за 7 дней (кэшируется на время сессии страницы). */
const sparkCache = new Map<string, Promise<number[]>>();
export function AssetSpark({ symbol, width, height }: { symbol: string; width?: number; height?: number }) {
  const [vals, setVals] = useState<number[]>([]);
  useEffect(() => {
    let p = sparkCache.get(symbol);
    if (!p) {
      p = api.get<{ candles: Candle[] }>(`/market/candles?symbol=${symbol}&interval=4h&currency=BYN&limit=42`).then((r) => r.candles.map((c) => c.c)).catch(() => []);
      sparkCache.set(symbol, p);
    }
    let alive = true;
    p.then((v) => alive && setVals(v));
    return () => {
      alive = false;
    };
  }, [symbol]);
  return <Sparkline values={vals} width={width} height={height} />;
}
