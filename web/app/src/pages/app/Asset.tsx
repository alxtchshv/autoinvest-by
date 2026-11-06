import { Link, useParams } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { api } from "../../lib/api";
import { pct, price } from "../../lib/format";
import { useLoad, useTitle } from "../../lib/hooks";
import { useMarket, useSession } from "../../lib/session";
import type { Asset, Quote, Stats } from "../../lib/types";
import { AssetIcon, Change, LoadError, Notice, PageLoader, RiskDots, riskLabel } from "../../components/ui";
import { PriceChart } from "../../components/charts";

export default function AssetPage() {
  const { symbol = "" } = useParams();
  const { config } = useSession();
  const { quotes } = useMarket();
  const { data, error, loading, reload } = useLoad(() => api.get<{ asset: Asset; quote?: Quote; stats?: Stats }>(`/market/assets/${symbol}`), [symbol]);
  useTitle(data?.asset.name || symbol);
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const a = data!.asset;
  const q = quotes[symbol] || data!.quote;
  const st = data!.stats;
  const stable = a.kind === "stable" || a.kind === "bond";
  const strategy = config?.strategies.find((s) => Object.keys(s.weights).length === 1 && s.weights[symbol]) ||
    config?.strategies.filter((s) => s.weights[symbol]).sort((x, y) => (y.weights[symbol] || 0) - (x.weights[symbol] || 0))[0];
  return (
    <div className="stack g16">
      <div>
        <Link to="/app/market" className="back"><ArrowLeft />Рынок</Link>
        <div className="page-head" style={{ marginBottom: 0 }}>
          <div className="asset-head">
            <AssetIcon symbol={symbol} size="lg" />
            <div>
              <h1>{a.name} <span className="muted" style={{ fontWeight: 500 }}>{a.symbol}</span></h1>
              <div className="row g12 wrap">
                <span className="asset-price num">{q ? price(q.price_byn) : "—"}</span>
                {q && <Change v={q.change_24h} />}
                {q && <span className="small muted">{price(q.price_usd, "$")} · {q.source}</span>}
              </div>
            </div>
          </div>
          <Link to={a.pro_only ? "/app/plans/new?strategy=custom" : `/app/plans/new?strategy=${strategy?.id || "balanced"}`} className="btn btn-primary">Копить в {a.name}</Link>
        </div>
      </div>
      {a.pro_only && <Notice kind="info" title="Доступно на тарифе Pro">Акции и токены компаний можно добавить в копилку со своим составом на тарифе Pro. <Link to="/app/profile/billing">Подробнее о Pro</Link></Notice>}
      {a.indicative && <Notice kind="warn" title="Расчётная цена">Это учебная модель: цена растёт по условной ставке {((a.coupon_bp || 0) / 100).toString().replace(".", ",")}% годовых в {a.face_currency}, биржевых торгов нет. Реальные выпуски с конкретными эмитентами появятся после подключения площадки-партнёра.</Notice>}
      <section className="sheet sheet-pad">
        <PriceChart symbol={symbol} stable={stable} />
      </section>
      {st && st.days > 30 && (
        <section className="sheet">
          <div className="sheet-head"><h2>За последний год</h2><span className="small muted">в долларах, по дневным ценам</span></div>
          <div className="stat-grid">
            <div><div className="l">Доходность</div><div className="v"><Change v={st.return_1y} /></div></div>
            <div><div className="l">За 30 дней</div><div className="v"><Change v={st.return_30d} /></div></div>
            <div><div className="l">Худшая просадка</div><div className="v down">{pct(st.max_drawdown)}</div></div>
            <div><div className="l">Волатильность</div><div className="v">{pct(st.volatility, { sign: false, digits: 0 })} в год</div></div>
            <div><div className="l">Максимум</div><div className="v num">{price(st.high_1y, "$")}</div></div>
            <div><div className="l">Минимум</div><div className="v num">{price(st.low_1y, "$")}</div></div>
            <div><div className="l">Лучший день</div><div className="v"><Change v={st.best_day} /></div></div>
            <div><div className="l">Худший день</div><div className="v"><Change v={st.worst_day} /></div></div>
          </div>
        </section>
      )}
      <section className="sheet sheet-pad stack g12">
        <h2>Об активе</h2>
        <p style={{ maxWidth: "62em" }}>{a.description}</p>
        <div className="row g12 small"><RiskDots risk={a.risk} /><span>Риск: {riskLabel(a.risk).toLowerCase()}</span></div>
        {!stable && <p className="small muted">История торгов на бирже — с {new Date(a.listed_since).getFullYear()} года. Прошлая доходность не гарантирует будущую.</p>}
      </section>
    </div>
  );
}
