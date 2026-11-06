import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { AreaSeries, ColorType, createChart, type UTCTimestamp } from "lightweight-charts";
import { Check } from "lucide-react";
import { api } from "../../lib/api";
import { byn, kop, pct, price } from "../../lib/format";
import { useTitle } from "../../lib/hooks";
import { useMarket, useSession } from "../../lib/session";
import type { BacktestResult, Candle, Strategy } from "../../lib/types";
import { AssetIcon, Change, RiskDots, riskLabel } from "../../components/ui";

function HeroChart({ candles }: { candles: Candle[] }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!ref.current || !candles.length) return;
    const c = createChart(ref.current, {
      autoSize: true, height: 180,
      layout: { background: { type: ColorType.Solid, color: "transparent" }, textColor: "#8a919e", fontFamily: "Onest Variable, sans-serif", attributionLogo: false },
      grid: { vertLines: { visible: false }, horzLines: { visible: false } },
      rightPriceScale: { visible: false }, leftPriceScale: { visible: false },
      timeScale: { visible: false, fixLeftEdge: true, fixRightEdge: true },
      crosshair: { vertLine: { visible: false, labelVisible: false }, horzLine: { visible: false, labelVisible: false } },
      handleScroll: false, handleScale: false,
    });
    const s = c.addSeries(AreaSeries, { lineColor: "#2340d8", topColor: "rgba(35,64,216,.2)", bottomColor: "rgba(35,64,216,0)", lineWidth: 2, priceLineVisible: false, lastValueVisible: false, crosshairMarkerVisible: false });
    s.setData(candles.map((x) => ({ time: x.t as UTCTimestamp, value: x.c })));
    c.timeScale().fitContent();
    return () => c.remove();
  }, [candles]);
  return <div ref={ref} style={{ height: 180, margin: "0 -4px" }} />;
}

const AMOUNTS = [1000, 2000, 5000];
const PICK = ["dollar", "balanced", "growth"];

function LiveDemo() {
  const { config } = useSession();
  const { quotes, rates } = useMarket();
  const [candles, setCandles] = useState<Candle[]>([]);
  const [amount, setAmount] = useState(2000);
  const [strategy, setStrategy] = useState("balanced");
  const [bt, setBt] = useState<BacktestResult | null>(null);
  const [btErr, setBtErr] = useState<string | null>(null);
  useEffect(() => {
    api.get<{ candles: Candle[] }>("/market/candles?symbol=BTC&interval=1d&currency=BYN&limit=180").then((r) => setCandles(r.candles)).catch(() => {});
  }, []);
  const st = config?.strategies.find((s) => s.id === strategy);
  useEffect(() => {
    if (!st) return;
    let alive = true;
    setBtErr(null);
    api.post<BacktestResult>("/calculator/backtest", { weights: st.weights, amount_kop: amount, frequency: "weekly", years: 3 })
      .then((r) => alive && setBt(r))
      .catch((e) => alive && setBtErr(e.message));
    return () => { alive = false; };
  }, [st, amount]);
  const q = quotes.BTC;
  const from = bt ? new Date(bt.from).toLocaleDateString("ru-RU", { month: "long", year: "numeric" }) : "";
  return (
    <div className="sheet hero-card">
      <div className="row between" style={{ padding: "20px 22px 0" }}>
        <div className="row g12">
          <AssetIcon symbol="BTC" />
          <div>
            <div style={{ fontWeight: 600 }}>Bitcoin сейчас</div>
            <div className="tiny muted">{q ? `${q.source}, обновляется каждые 5 секунд` : "Загрузка котировки…"}</div>
          </div>
        </div>
        <div style={{ textAlign: "right" }}>
          <div className="num" style={{ fontWeight: 650, fontSize: 18 }}>{q ? price(q.price_byn) : "—"}</div>
          {q && <Change v={q.change_24h} className="small" />}
        </div>
      </div>
      <HeroChart candles={candles} />
      <div className="hero-sim">
        <div className="small muted">Если бы вы откладывали каждую неделю</div>
        <div className="row g8 wrap mt8">
          {AMOUNTS.map((a) => <button key={a} className="chip" aria-pressed={a === amount} onClick={() => setAmount(a)}>{kop(a).replace(",00", "")} BYN</button>)}
        </div>
        <div className="row g8 wrap mt8">
          {PICK.map((id) => {
            const s = config?.strategies.find((x) => x.id === id);
            return s && <button key={id} className="chip" aria-pressed={id === strategy} onClick={() => setStrategy(id)}>{s.name}</button>;
          })}
        </div>
        {bt && !btErr && (
          <div className="hero-result">
            <div><span className="muted small">Вложили бы с {from}</span><b className="num">{byn(bt.invested_kop)}</b></div>
            <div><span className="muted small">Было бы сейчас</span><b className={`num ${bt.value_kop >= bt.invested_kop ? "up" : "down"}`}>{byn(bt.value_kop)}</b></div>
          </div>
        )}
        {btErr && <div className="small muted mt16">{btErr}</div>}
        <div className="tiny faint mt8">Расчёт по реальным дневным ценам и курсу НБРБ с учётом комиссии. {rates ? `Курс USD сегодня ${price(rates.usd)}.` : ""}</div>
      </div>
    </div>
  );
}

function StrategyRow({ s }: { s: Strategy }) {
  const { config } = useSession();
  const parts = Object.entries(s.weights).sort((a, b) => b[1] - a[1]);
  return (
    <div className="strat-row">
      <div className="strat-name">
        <h3>{s.name}</h3>
        <div className="row g8 small muted"><RiskDots risk={s.risk} />{riskLabel(s.risk)} риск</div>
      </div>
      <div className="strat-mix">
        <div className="alloc-bar">
          {parts.map(([a, w]) => <span key={a} style={{ width: w / 100 + "%", background: config?.assets.find((x) => x.symbol === a)?.color }} />)}
        </div>
        <div className="row g12 wrap small mt8">
          {parts.map(([a, w]) => <span key={a} className="row g4"><span className="dot" style={{ background: config?.assets.find((x) => x.symbol === a)?.color }} />{config?.assets.find((x) => x.symbol === a)?.short} {w / 100}%</span>)}
        </div>
      </div>
      <p className="muted strat-desc">{s.description}</p>
    </div>
  );
}

const FAQ: [string, string][] = [
  ["Где хранятся мои деньги и активы?", "Деньги списываются с вашей карты через платёжного агрегатора bePaid и сразу направляются на покупку выбранных активов у оператора криптоплатформы — резидента Парка высоких технологий. На счетах AutoInvest деньги не лежат. Купленные активы учитываются за вами, их состав и стоимость видны в копилке в реальном времени."],
  ["Сколько это стоит?", "Открыть и вести копилку бесплатно. При каждом пополнении удерживается комиссия сервиса — 1% на тарифе Старт и 0,5% на Pro. Курс покупки — биржевая цена плюс спред партнёра 1%. Всё это показано в чеке каждой операции до копейки."],
  ["Как вывести деньги?", "В любой момент: откройте копилку и нажмите «Вывести». Активы продаются по текущей цене, деньги приходят на привязанную карту. Без штрафов и сроков удержания."],
  ["Можно ли потерять деньги?", "Да. Цены криптовалют сильно колеблются — биткоин исторически падал на 70–80% от пика. Цифровой доллар (USDT) повторяет официальный курс доллара, золото меняется медленнее. Выбирайте стратегию по своему отношению к риску — анкета в профиле подскажет."],
  ["Зачем регулярные пополнения, а не одна покупка?", "Когда вы покупаете на одинаковую сумму каждую неделю, в дешёвые периоды получаете больше актива, в дорогие — меньше. Средняя цена сглаживается, и не нужно угадывать момент входа. Этот подход называют усреднением (DCA)."],
  ["Какие карты подходят?", "Карты белорусских банков Visa, Mastercard и Белкарт. Данные карты вы вводите на защищённой странице bePaid, сервис получает только токен для списаний — номер карты у нас не хранится."],
  ["Что с налогами?", "Операции с токенами через резидентов ПВТ регулируются Декретом № 8 и Налоговым кодексом. Правила периодически меняются — актуальную информацию уточняйте в налоговой инспекции. Выписку по всем операциям можно выгрузить в разделе «Операции»."],
];

export default function Landing() {
  useTitle("");
  const { config, me } = useSession();
  const strategies = config?.strategies || [];
  const proMonth = config ? kop(config.pro_month_kop) : "9,90";
  const feeFree = config ? pct(config.fee_free_bp / 100, { sign: false, digits: 1 }).replace(",0", "") : "1%";
  const feePro = config ? pct(config.fee_pro_bp / 100, { sign: false, digits: 1 }) : "0,5%";
  const ctaTo = me?.user ? "/app/plans/new" : "/register";
  const facts = useMemo(() => [
    ["от 5 BYN", "минимальное пополнение"],
    ["6 активов", "доллар, золото, криптовалюты"],
    ["в любой момент", "вывод на карту без штрафов"],
  ], []);
  return (
    <>
      <section className="wrapx hero">
        <div>
          <h1>Откладывайте рубли, а копилка сама купит доллар, золото или биткоин</h1>
          <p className="lead">Выберите сумму и день — каждую неделю или месяц AutoInvest спишет её с карты и купит активы по биржевой цене. Без бирж, кошельков и ручной рутины.</p>
          <div className="cta">
            <Link to={ctaTo} className="btn btn-primary btn-lg">Открыть копилку</Link>
            <Link to="/calculator" className="btn btn-secondary btn-lg">Посчитать доход</Link>
          </div>
          <div className="facts">
            {facts.map(([b, s]) => <div key={b}><b>{b}</b>{s}</div>)}
          </div>
        </div>
        <LiveDemo />
      </section>

      <section className="section alt" id="how">
        <div className="wrapx">
          <h2 className="big">Три шага, дальше всё происходит само</h2>
          <div className="steps">
            <div><h3>Привяжите карту</h3><p>Любая карта белорусского банка. Реквизиты вводятся на странице bePaid с 3-D Secure.</p></div>
            <div><h3>Настройте копилку</h3><p>Цель, сумма, расписание и стратегия. Например, 20 BYN по пятницам: 60% в доллар, 40% в золото.</p></div>
            <div><h3>Следите за ростом</h3><p>Каждое пополнение приходит чеком в приложение и Telegram. Пауза, изменение и вывод — в один клик.</p></div>
          </div>
        </div>
      </section>

      <section className="section" id="strategies">
        <div className="wrapx">
          <h2 className="big">Пять готовых стратегий — от спокойной до рискованной</h2>
          <p className="sub">Состав можно поменять в любой момент. На тарифе Pro доступен собственный набор активов и докупка на просадках.</p>
          <div className="sheet mt32">{strategies.map((s) => <StrategyRow key={s.id} s={s} />)}</div>
        </div>
      </section>

      <section className="section alt" id="pricing">
        <div className="wrapx">
          <h2 className="big">Прозрачные условия</h2>
          <p className="sub">Никаких платежей за обслуживание счёта. Всё, что удерживается, видно в чеке каждой операции.</p>
          <div className="pricing mt32">
            <div className="price-col">
              <h3>Старт</h3>
              <div className="price-tag">0 BYN</div>
              <ul>
                <li><Check />До {config?.free_plan_limit ?? 3} копилок</li>
                <li><Check />Готовые стратегии</li>
                <li><Check />Комиссия {feeFree} с пополнения</li>
                <li><Check />Уведомления в Telegram</li>
              </ul>
            </div>
            <div className="price-col pro">
              <h3>Pro</h3>
              <div className="price-tag">{proMonth} BYN <span>в месяц</span></div>
              <ul>
                <li><Check />До {config?.pro_plan_limit ?? 20} копилок</li>
                <li><Check />Свой состав активов</li>
                <li><Check />Докупка при падении цены</li>
                <li><Check />Комиссия {feePro} с пополнения</li>
              </ul>
              <div className="tiny muted mt16">При пополнениях от 990 BYN в месяц Pro окупается за счёт меньшей комиссии.</div>
            </div>
          </div>
        </div>
      </section>

      <section className="section" id="faq">
        <div className="wrapx faq-wrap">
          <h2 className="big">Частые вопросы</h2>
          <div className="faq">
            {FAQ.map(([q, a]) => (
              <details key={q}>
                <summary>{q}</summary>
                <p>{a}</p>
              </details>
            ))}
          </div>
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
