import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Link, useLocation } from "react-router-dom";
import { AreaSeries, ColorType, createChart, LineSeries, LineStyle, type UTCTimestamp } from "lightweight-charts";
import { ArrowLeft, ArrowRight, Bell, Check, Lock, SlidersHorizontal } from "lucide-react";
import { api } from "../../lib/api";
import { byn, groupInt, kop, pct, planName, price, RUB } from "../../lib/format";
import { useTitle } from "../../lib/hooks";
import { useMarket, useSession } from "../../lib/session";
import type { Strategy } from "../../lib/types";
import { Accordion, AssetIcon, Change, RiskDots, riskLabel } from "../../components/ui";
import { PITCH, PlanMark, Ring } from "../../components/plans";

/** Оригинальные названия активов — так их называют биржи и так их узнают. */
function useAssetName() {
  const { config } = useSession();
  return (sym: string) => config?.assets.find((a) => a.symbol === sym)?.name || sym;
}

type ShowItem = {
  id: string; kind: "strategy" | "asset"; name: string; symbol?: string; color?: string; risk: number;
  invested_kop: number; value_kop: number; profit_pct: number; max_drawdown: number; from: string;
  points: { t: number; i: number; v: number }[];
  usd_cents?: number; fx_gain_kop?: number;
};

const itemTitle = (x: ShowItem) => (x.kind === "strategy" ? planName(x.name) : x.name);

function ItemMark({ x }: { x: ShowItem }) {
  return x.kind === "strategy" ? <PlanMark id={x.id} size={20} /> : <AssetIcon symbol={x.symbol!} size="sm" />;
}

function useShowcase(amount: number, years: number) {
  const [data, setData] = useState<ShowItem[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    let alive = true;
    setErr(null);
    api.get<{ items: ShowItem[] }>(`/calculator/showcase?amount_kop=${amount}&years=${years}`)
      .then((r) => alive && setData(r.items))
      .catch((e) => alive && setErr(e.message));
    return () => { alive = false; };
  }, [amount, years]);
  return { data, err };
}

const roundByn = (k: number) => groupRub(Math.round(k / 100));
function groupRub(n: number) { return n.toString().replace(/\B(?=(\d{3})+(?!\d))/g, " ") + " " + RUB; }

/* ---------------- «Машина времени» ---------------- */

function GrowthChart({ points }: { points: ShowItem["points"] }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!ref.current || !points.length) return;
    const c = createChart(ref.current, {
      autoSize: true, height: 170,
      layout: { background: { type: ColorType.Solid, color: "transparent" }, textColor: "#8a919e", fontFamily: "Onest Variable, sans-serif", attributionLogo: false },
      grid: { vertLines: { visible: false }, horzLines: { visible: false } },
      rightPriceScale: { visible: false }, leftPriceScale: { visible: false },
      timeScale: { visible: false, fixLeftEdge: true, fixRightEdge: true },
      crosshair: { vertLine: { visible: false, labelVisible: false }, horzLine: { visible: false, labelVisible: false } },
      handleScroll: false, handleScale: false,
    });
    const v = c.addSeries(AreaSeries, { lineColor: "#0a7a4b", topColor: "rgba(10,122,75,.22)", bottomColor: "rgba(10,122,75,0)", lineWidth: 2, priceLineVisible: false, lastValueVisible: false, crosshairMarkerVisible: false });
    const inv = c.addSeries(LineSeries, { color: "#8a919e", lineWidth: 1, lineStyle: LineStyle.Dashed, priceLineVisible: false, lastValueVisible: false, crosshairMarkerVisible: false });
    v.setData(points.map((p) => ({ time: p.t as UTCTimestamp, value: p.v / 100 })));
    inv.setData(points.map((p) => ({ time: p.t as UTCTimestamp, value: p.i / 100 })));
    c.timeScale().fitContent();
    return () => c.remove();
  }, [points]);
  return <div ref={ref} className="tm-chart" />;
}

const AMOUNTS = [1000, 2000, 5000];
const YEARS = [3, 5, 7];

function TimeMachine() {
  const [amount, setAmount] = useState(2000);
  const [years, setYears] = useState(7);
  const { data, err } = useShowcase(amount, years);
  const [sel, setSel] = useState<string | null>(null);
  useEffect(() => setSel(null), [amount, years]);
  const best = data?.[0];
  const cur = data?.find((x) => x.id === sel) || best;
  const from = cur ? new Date(cur.from).toLocaleDateString("ru-RU", { day: "numeric", month: "long", year: "numeric" }) : "";
  const plans = data?.filter((x) => x.kind === "strategy").slice(0, 3) || [];
  const assets = data?.filter((x) => x.kind === "asset").slice(0, 3) || [];
  const row = (x: ShowItem) => (
    <button key={x.id} role="listitem" className={`tm-row ${x.id === cur?.id ? "on" : ""}`} onClick={() => setSel(x.id)}>
      <ItemMark x={x} />
      <span className="tm-name">{itemTitle(x)}</span>
      <span className="tm-nums"><span className="num tm-v">{roundByn(x.value_kop)}</span><span className={`num tm-p ${x.profit_pct >= 0 ? "up" : "down"}`}>{pct(x.profit_pct, { digits: 0 })}</span></span>
    </button>
  );
  return (
    <div className="sheet tm">
      <div className="tm-head">
        <span className="tm-q">Если бы вы откладывали</span>
        <div className="tm-seg" role="group" aria-label="Сумма в неделю">
          {AMOUNTS.map((a) => <button key={a} aria-pressed={a === amount} onClick={() => setAmount(a)}>{kop(a).replace(",00", "")} {RUB}</button>)}
        </div>
        <span className="tm-q">в неделю</span>
        <div className="tm-seg" role="group" aria-label="Срок">
          {YEARS.map((y) => <button key={y} aria-pressed={y === years} onClick={() => setYears(y)}>{y === 3 ? "3 года" : `${y} лет`}</button>)}
        </div>
      </div>
      {cur ? (
        <div className="tm-body">
          <div className="tm-main">
            <div className="small muted row g8">{cur.id === best?.id ? "Лучший результат" : "Результат"} — <span className="row g4 ink"><ItemMark x={cur} />{itemTitle(cur)}</span></div>
            <div className="tm-value num up">{roundByn(cur.value_kop)}</div>
            <div className="small muted">вместо {roundByn(cur.invested_kop)} вложенных · <b className={cur.profit_pct >= 0 ? "up" : "down"}>{pct(cur.profit_pct, { digits: 0 })}</b></div>
            <GrowthChart points={cur.points} />
            <div className="tiny faint">С {from}, реальные цены и курс НБРБ, с комиссией. Пунктир — вложено. Прошлая доходность не гарантирует будущую.</div>
          </div>
          <div className="tm-side" role="list">
            <div className="tm-group">Планы</div>
            {plans.map(row)}
            <div className="tm-group">Монеты</div>
            {assets.map(row)}
          </div>
        </div>
      ) : (
        <div className="tm-empty muted small">{err || "Считаем по реальной истории цен…"}</div>
      )}
    </div>
  );
}

function LiveTicker() {
  const { quotes } = useMarket();
  const { config: cfg } = useSession();
  const list = (cfg?.assets || []).filter((a) => !a.pro_only && quotes[a.symbol] && !quotes[a.symbol].stale).map((a) => ({ a, q: quotes[a.symbol] }));
  if (!list.length) return null;
  const items = list.map(({ a, q }) => (
    <span key={a.symbol} className="ticker-item">
      <AssetIcon symbol={a.symbol} size="sm" />
      <span className="ticker-name">{a.name}</span>
      <b className="num">{price(q.price_byn)}</b>
      <Change v={q.change_24h} className="tiny" />
    </span>
  ));
  return (
    <div className="ticker-wrap">
      <div className="ticker-head"><span className="ticker-live"><i />Цены сейчас</span><span className="tiny faint">биржевые котировки в рублях по курсу НБРБ</span></div>
      <div className="marquee" aria-label="Цены сейчас">
        <div className="marquee-track" style={{ animationDuration: list.length * 6 + "s" }}>
          <div className="marquee-set">{items}</div>
          <div className="marquee-set" aria-hidden>{items}</div>
        </div>
      </div>
    </div>
  );
}

/* ---------------- Как это работает ---------------- */

function HowItWorks() {
  const steps: { title: string; text: string; art: ReactNode }[] = [
    {
      title: "Привяжите карту", text: "Visa, Mastercard или Белкарт любого банка Беларуси.",
      art: (
        <div className="art-card">
          <div className="row between"><span className="art-chip" /><span className="art-lock"><Lock />3-D Secure</span></div>
          <div className="art-num">4200 00•• •••• 0000</div>
          <div className="row between tiny"><span>IVAN PETROV</span><span>12/29</span></div>
        </div>
      ),
    },
    {
      title: "Выберите сумму и план", text: "Например, 20 " + RUB + " каждую пятницу.",
      art: (
        <div className="art-setup">
          <div className="row between"><span className="art-amount num">20 {RUB}</span><span className="tiny muted">План «Резерв»</span></div>
          <div className="art-days">{["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"].map((d) => <span key={d} className={d === "Пт" ? "on" : ""}>{d}</span>)}</div>
          <div className="alloc-bar"><span style={{ width: "60%", background: "#26A17B" }} /><span style={{ width: "40%", background: "#C9A227" }} /></div>
          <div className="row g12 tiny muted"><span className="row g4"><span className="dot" style={{ background: "#26A17B" }} />Tether 60%</span><span className="row g4"><span className="dot" style={{ background: "#C9A227" }} />PAX Gold 40%</span></div>
        </div>
      ),
    },
    {
      title: "Копилка пополняется сама", text: "Чек приходит в приложение и Telegram. Пауза и вывод — в один клик.",
      art: (
        <div className="art-feed">
          <div className="art-note"><span className="art-bell"><Bell /></span><div><b>Копилка пополнена</b><span>Куплено на 20,00 {RUB}</span></div><span className="num up">+20 {RUB}</span></div>
          <div className="art-note dim"><span className="art-bell"><Bell /></span><div><b>Копилка пополнена</b><span>Неделю назад</span></div><span className="num up">+20 {RUB}</span></div>
        </div>
      ),
    },
  ];
  return (
    <div className="how">
      {steps.map((s, i) => (
        <div key={s.title} className="how-card">
          <div className="how-art">{s.art}</div>
          <div className="how-text">
            <span className="how-n">{i + 1}</span>
            <div><h3>{s.title}</h3><p>{s.text}</p></div>
          </div>
        </div>
      ))}
    </div>
  );
}

/* ---------------- Стратегии: листаемая презентация ---------------- */


function Donut({ parts, colorOf, center }: { parts: [string, number][]; colorOf: (s: string) => string; center: ReactNode }) {
  return (
    <div className="donut">
      <Ring parts={parts} colorOf={colorOf} size={180} r={70} width={22} gap={3} />
      <div className="donut-c">{center}</div>
    </div>
  );
}

function StrategySlide({ s, stat, cta }: { s: Strategy; stat?: ShowItem; cta: string }) {
  const { config } = useSession();
  const nameOf = useAssetName();
  const parts = Object.entries(s.weights).sort((a, b) => b[1] - a[1]);
  const colorOf = (a: string) => config?.assets.find((x) => x.symbol === a)?.color || "#8a919e";
  return (
    <div className="slide">
      <Donut parts={parts} colorOf={colorOf} center={<><b>{parts.length}</b><span>{parts.length === 1 ? "актив" : parts.length < 5 ? "актива" : "активов"}</span></>} />
      <div className="slide-body">
        <div className="row g8 small muted"><RiskDots risk={s.risk} />{riskLabel(s.risk)} риск</div>
        <h3>{planName(s.name)}</h3>
        <p className="slide-pitch">{PITCH[s.id] || s.description}</p>
        <div className="slide-mix">
          {parts.map(([a, w]) => <span key={a}><AssetIcon symbol={a} size="sm" />{nameOf(a)}<b className="num">{w / 100}%</b></span>)}
        </div>
        {stat && (stat.usd_cents ? (
          <div className="slide-stat">
            <span className="small muted">20 {RUB} каждую неделю последние 5 лет</span>
            <span><b className="num">${groupInt(Math.round(stat.usd_cents / 100))}</b> <span className="small muted">накоплено — {roundByn(stat.value_kop)} по курсу сегодня</span></span>
            {(stat.fx_gain_kop ?? 0) > 0 && <span className="small up">+{roundByn(stat.fx_gain_kop!)} принёс рост курса доллара</span>}
          </div>
        ) : (
          <div className="slide-stat">
            <span className="small muted">20 {RUB} каждую неделю последние 5 лет</span>
            <span><b className="num">{roundByn(stat.value_kop)}</b> <span className="small muted">из {roundByn(stat.invested_kop)} вложенных</span></span>
          </div>
        ))}
        <Link to={cta + "?strategy=" + s.id} className="btn btn-primary">Выбрать {planName(s.name).replace("План", "план")}</Link>
      </div>
    </div>
  );
}

function CustomSlide({ cta }: { cta: string }) {
  const rows: [string, string, number][] = [["BTC", "Bitcoin", 45], ["SPY", "S&P 500", 30], ["PAXG", "PAX Gold", 25]];
  return (
    <div className="slide">
      <div className="custom-art">
        {rows.map(([s, n, w], i) => (
          <div key={s} className="custom-row" style={{ animationDelay: i * 0.15 + "s" }}>
            <div className="row between small"><span className="row g8"><AssetIcon symbol={s} size="sm" />{n}</span><b className="num">{w}%</b></div>
            <div className="custom-track"><i style={{ width: w * 1.6 + "%" }} /></div>
          </div>
        ))}
      </div>
      <div className="slide-body">
        <div className="row g8 small muted"><SlidersHorizontal className="ico-sm" />Тариф Pro</div>
        <h3>Свой состав</h3>
        <p className="slide-pitch">Сами решаете, что покупать и в какой доле — включая акции США и токены компаний Беларуси.</p>
        <ul className="slide-list">
          <li><Check />Сумму, день и частоту можно менять в любой копилке</li>
          <li><Check />Докупка, когда цена падает</li>
          <li><Check />Пауза и вывод в любой момент</li>
        </ul>
        <Link to={cta + "?strategy=custom"} className="btn btn-primary">Собрать свою копилку</Link>
      </div>
    </div>
  );
}

function StrategyDeck({ strategies }: { strategies: Strategy[] }) {
  const { me } = useSession();
  const cta = me?.user ? "/app/plans/new" : "/register";
  const { data } = useShowcase(2000, 5);
  const [i, setI] = useState(2);
  const n = strategies.length + 1;
  const go = (k: number) => setI(Math.max(0, Math.min(n - 1, k)));
  const drag = useRef<{ x: number; y: number; dx: number; on: boolean } | null>(null);
  const [dx, setDx] = useState(0);
  const tabsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const tab = tabsRef.current?.children[i] as HTMLElement | undefined;
    tab?.scrollIntoView({ behavior: "smooth", block: "nearest", inline: "center" });
  }, [i]);
  if (!strategies.length) return null;
  return (
    <div className="deck" onKeyDown={(e) => { if (e.key === "ArrowRight") go(i + 1); if (e.key === "ArrowLeft") go(i - 1); }}>
      <div className="deck-view"
        onPointerDown={(e) => { if (e.pointerType !== "mouse") drag.current = { x: e.clientX, y: e.clientY, dx: 0, on: false }; }}
        onPointerMove={(e) => {
          const d = drag.current; if (!d) return;
          const mx = e.clientX - d.x, my = e.clientY - d.y;
          if (!d.on && Math.abs(mx) > 8 && Math.abs(mx) > Math.abs(my)) d.on = true;
          if (d.on) { d.dx = mx; setDx(mx); }
        }}
        onPointerUp={() => { const d = drag.current; drag.current = null; setDx(0); if (d?.on && Math.abs(d.dx) > 60) go(i + (d.dx < 0 ? 1 : -1)); }}
        onPointerCancel={() => { drag.current = null; setDx(0); }}
      >
        <div className="deck-track" style={{ transform: `translateX(calc(${-i * 100}% + ${dx}px))`, transition: dx ? "none" : undefined }}>
          {strategies.map((s, k) => (
            <div key={s.id} className="deck-slide" aria-hidden={k !== i} inert={k !== i}>
              <StrategySlide s={s} stat={data?.find((x) => x.id === s.id)} cta={cta} />
            </div>
          ))}
          <div className="deck-slide" aria-hidden={i !== n - 1} inert={i !== n - 1}><CustomSlide cta={cta} /></div>
        </div>
        <button className="deck-arrow prev" aria-label="Предыдущий план" disabled={i === 0} onClick={() => go(i - 1)}><ArrowLeft /></button>
        <button className="deck-arrow next" aria-label="Следующий план" disabled={i === n - 1} onClick={() => go(i + 1)}><ArrowRight /></button>
      </div>
      <div className="deck-tabs" role="tablist" ref={tabsRef}>
        {strategies.map((s, k) => (
          <button key={s.id} role="tab" aria-selected={k === i} className={k === i ? "on" : ""} onClick={() => go(k)}>
            <span className={`deck-risk r${s.risk}`} />{planName(s.name)}
          </button>
        ))}
        <button role="tab" aria-selected={i === n - 1} className={i === n - 1 ? "on" : ""} onClick={() => go(n - 1)}><SlidersHorizontal className="ico-sm" />Свой состав</button>
      </div>
    </div>
  );
}

/* ---------------- Вопросы ---------------- */

const FAQ: [string, string][] = [
  ["Где хранятся мои деньги и активы?", "Деньги списываются с вашей карты через платёжного агрегатора bePaid и сразу идут на покупку активов у оператора криптоплатформы — резидента Парка высоких технологий. На счетах AutoInvest деньги не лежат. Состав и стоимость копилки видны в реальном времени. Пока сервис работает в тестовом режиме, покупки учебные: цены настоящие, но деньги не списываются — об этом говорит плашка вверху страницы."],
  ["Сколько это стоит?", "Открыть и вести копилку бесплатно. С каждого пополнения удерживается комиссия сервиса — 1% на тарифе Старт и 0,5% на Pro. Курс покупки — биржевая цена плюс спред партнёра 1%. Всё это видно в чеке каждой операции."],
  ["Как вывести деньги?", "В любой момент: откройте копилку и нажмите «Вывести». Активы продаются по текущей цене, деньги приходят на привязанную карту. Без штрафов и сроков удержания."],
  ["Можно ли потерять деньги?", "Да. Криптовалюты сильно колеблются — биткоин исторически падал на 70–80% от пика. Цифровой доллар повторяет курс доллара, золото меняется медленнее. Выбирайте план по своему отношению к риску — анкета после регистрации подскажет."],
  ["Зачем пополнять регулярно, а не купить один раз?", "Когда вы покупаете на одинаковую сумму каждую неделю, в дешёвые периоды получаете больше актива, в дорогие — меньше. Средняя цена сглаживается, и не нужно угадывать момент. Этот подход называют усреднением."],
  ["Можно ли настроить копилку под себя?", "Да. Сумму, день и частоту пополнения можно поменять в любой момент, копилку — поставить на паузу. На тарифе Pro можно собрать свой состав из любых активов и включить докупку при падении цены."],
  ["Какие активы доступны?", "На всех тарифах — цифровой доллар USDT, золото PAX Gold, крупные криптовалюты и токены белорусских компаний с фиксированной доходностью. На Pro — ещё токены, следующие за акциями США (S&P 500, Apple, Microsoft, Nvidia, Tesla)."],
  ["Какие карты подходят?", "Visa, Mastercard и Белкарт белорусских банков. Номер карты вы вводите на защищённой странице bePaid, сервис получает только токен для списаний."],
  ["Что с налогами?", "По разъяснению МНС доходы физлиц от операций с токенами через резидентов ПВТ подоходным налогом не облагаются; через зарубежные площадки — 13% по декларации. Подробно — в разделе «Налоги» внизу страницы."],
];

function Faq() {
  return <Accordion items={FAQ} />;
}

/* ---------------- Страница ---------------- */

export default function Landing() {
  useTitle("");
  const { config, me } = useSession();
  const loc = useLocation();
  const strategies = config?.strategies || [];
  const month = config?.pro_month_kop ?? 990;
  const year = config?.pro_year_kop ?? 8900;
  const save = month * 12 - year;
  const savePct = Math.round((save / (month * 12)) * 100);
  // при каком ежемесячном пополнении экономия на комиссии покрывает годовую подписку
  const feeDiff = ((config?.fee_free_bp ?? 100) - (config?.fee_pro_bp ?? 50)) / 10000;
  const breakEven = feeDiff > 0 ? Math.ceil(year / 12 / feeDiff / 100) : 0;
  const feeFree = config ? pct(config.fee_free_bp / 100, { sign: false, digits: 1 }).replace(",0", "") : "1%";
  const feePro = config ? pct(config.fee_pro_bp / 100, { sign: false, digits: 1 }) : "0,5%";
  const ctaTo = me?.user ? "/app/plans/new" : "/register";
  const facts = useMemo(() => [
    [`от 5 ${RUB}`, "минимальное пополнение"],
    ["6 планов", "доллар, золото, крипта"],
    ["без штрафов", "вывод на карту в любой момент"],
  ], []);

  // переход с другой страницы по ссылке вида /#faq: прокручиваем, когда раздел отрисован
  useEffect(() => {
    if (!loc.hash) return;
    const t = setTimeout(() => document.getElementById(loc.hash.slice(1))?.scrollIntoView({ behavior: "smooth" }), 60);
    return () => clearTimeout(t);
  }, [loc.hash, strategies.length]);

  const proFeatures = [`До ${config?.pro_plan_limit ?? 20} копилок`, "Свой состав активов", "Акции США", "Докупка при падении цены", `Комиссия ${feePro} с пополнения`];
  return (
    <>
      <section className="wrapx hero">
        <div>
          <h1>Откладывайте рубли, а копилка сама купит доллар, золото или биткоин</h1>
          <p className="lead">Выберите сумму и день — AutoInvest будет сам пополнять копилку с карты и покупать активы по биржевой цене.</p>
          <div className="cta">
            <Link to={ctaTo} className="btn btn-primary btn-lg">Открыть копилку</Link>
            <Link to="/calculator" className="btn btn-secondary btn-lg">Посчитать доход</Link>
          </div>
          <div className="facts">
            {facts.map(([b, s]) => <div key={b}><b>{b}</b>{s}</div>)}
          </div>
        </div>
        <TimeMachine />
      </section>
      <div className="wrapx"><LiveTicker /></div>

      <section className="section alt" id="how">
        <div className="wrapx">
          <h2 className="big">Три шага — дальше всё само</h2>
          <HowItWorks />
        </div>
      </section>

      <section className="section" id="strategies">
        <div className="wrapx">
          <h2 className="big">Выберите план</h2>
          <p className="sub">Шесть готовых планов — от спокойного до смелого. Или соберите свой, в том числе с токенами белорусских компаний.</p>
          <StrategyDeck strategies={strategies} />
        </div>
      </section>

      <section className="section alt" id="pricing">
        <div className="wrapx">
          <h2 className="big">Прозрачные условия</h2>
          <p className="sub">Без платы за обслуживание. Всё, что удерживается, видно в чеке.</p>
          <div className="pricing mt32">
            <div className="price-col">
              <h3>Старт</h3>
              <div className="price-tag">0 {RUB}</div>
              <div className="small muted">навсегда</div>
              <ul>
                <li><Check />До {config?.free_plan_limit ?? 3} копилок</li>
                <li><Check />Шесть готовых планов</li>
                <li><Check />Токены компаний Беларуси</li>
                <li><Check />Комиссия {feeFree} с пополнения</li>
                <li><Check />Уведомления в Telegram</li>
              </ul>
              <Link to={ctaTo} className="btn btn-secondary btn-block mt-auto">Начать бесплатно</Link>
            </div>
            <div className="price-col">
              <h3>Pro</h3>
              <div className="price-tag">{kop(month)} {RUB}</div>
              <div className="small muted">в месяц</div>
              <ul>{proFeatures.map((f) => <li key={f}><Check />{f}</li>)}</ul>
              <Link to={me?.user ? "/app/profile/billing" : "/register"} className="btn btn-secondary btn-block mt-auto">Подключить Pro</Link>
            </div>
            <div className="price-col pro">
              <div className="row between"><h3>Pro на год</h3>{savePct > 0 && <span className="badge badge-brand">−{savePct}%</span>}</div>
              <div className="price-tag">{kop(year).replace(",00", "")} {RUB}</div>
              <div className="small muted">в год — {kop(Math.round(year / 12))} {RUB} в месяц</div>
              <ul>
                <li><Check />Всё, что в Pro</li>
                <li><Check />Экономия {byn(save).replace(",00", "")} в год</li>
                <li><Check />Одна оплата — и год без забот</li>
              </ul>
              {breakEven > 0 && <div className="tiny muted">Окупается сам, если откладывать от {groupRub(breakEven)} в месяц: комиссия с пополнений вдвое меньше.</div>}
              <Link to={me?.user ? "/app/profile/billing" : "/register"} className="btn btn-primary btn-block mt-auto">Выбрать год</Link>
            </div>
          </div>
        </div>
      </section>

      <section className="section" id="faq">
        <div className="wrapx faq-wrap">
          <h2 className="big">Частые вопросы</h2>
          <Faq />
        </div>
      </section>

      <section className="section alt">
        <div className="wrapx final-cta">
          <h2 className="big">Первая копилка — за две минуты</h2>
          <Link to={ctaTo} className="btn btn-primary btn-lg">Открыть копилку</Link>
        </div>
      </section>
    </>
  );
}
