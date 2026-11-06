import { Link, useNavigate } from "react-router-dom";
import { price } from "../../lib/format";
import { useTitle } from "../../lib/hooks";
import { useMarket, useSession } from "../../lib/session";
import { AssetIcon, Change, RiskDots, riskLabel } from "../../components/ui";
import { AssetSpark } from "../../components/charts";

export default function Market() {
  useTitle("Рынок");
  const { config } = useSession();
  const nav = useNavigate();
  const { quotes, rates, connected, lastTick } = useMarket();
  const { me } = useSession();
  const isPro = !!me?.is_pro;
  const groups = [
    { kind: "stable", title: "Доллар" },
    { kind: "gold", title: "Золото" },
    { kind: "crypto", title: "Криптовалюты" },
    { kind: "stock", title: "Токенизированные акции США" },
    { kind: "bond", title: "Токены белорусских компаний" },
  ];
  const src = Object.values(quotes).find((q) => q.symbol !== "USDT")?.source;
  return (
    <div>
      <div className="page-head">
        <div>
          <h1>Рынок</h1>
          <p>
            Цены в рублях по курсу НБРБ{src ? `, источник — ${src}` : ""}.{" "}
            <span className="row g4" style={{ display: "inline-flex" }}><span className="dot" style={{ background: connected ? "var(--up)" : "var(--faint)" }} />{connected && lastTick ? "обновляется в реальном времени" : "переподключение…"}</span>
          </p>
        </div>
        {rates && <div className="small muted num" style={{ textAlign: "right" }}>USD {price(rates.usd)}<br />EUR {price(rates.eur)}</div>}
      </div>
      <div className="sheet">
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>Актив</th><th className="r">Цена</th><th className="r">За сутки</th><th className="r" style={{ minWidth: 110 }}>7 дней</th><th>Риск</th></tr></thead>
            <tbody>
              {groups.map((g) => [
                <tr key={"h-" + g.kind} className="group-row"><td colSpan={5}>{g.title}{g.kind === "stock" || g.kind === "bond" ? <span className="badge badge-pro">{isPro ? "Pro" : "Доступно на тарифе Pro"}</span> : null}</td></tr>,
                ...(config?.assets.filter((a) => a.kind === g.kind).map((a) => {
                  const q = quotes[a.symbol];
                  return (
                    <tr key={a.symbol} className="click" onClick={() => nav(`/app/market/${a.symbol}`)}>
                      <td>
                        <Link to={`/app/market/${a.symbol}`} className="row g12" style={{ color: "inherit" }}>
                          <AssetIcon symbol={a.symbol} />
                          <span><div style={{ fontWeight: 600 }}>{a.name} <span className="muted" style={{ fontWeight: 500 }}>{a.symbol.replace("_", " ")}</span></div><div className="small muted">{a.short}{a.indicative ? " · расчётная цена" : ""}</div></span>
                        </Link>
                      </td>
                      <td className="r num" style={{ fontWeight: 600 }}>{q ? price(q.price_byn) : "—"}<div className="tiny muted" style={{ fontWeight: 400 }}>{q ? price(q.price_usd, "$") : ""}</div></td>
                      <td className="r">{q ? <Change v={q.change_24h} /> : "—"}</td>
                      <td className="r"><span style={{ display: "inline-block" }}>{a.kind !== "stable" && a.kind !== "bond" && <AssetSpark symbol={a.symbol} />}</span></td>
                      <td><span className="row g8 small"><RiskDots risk={a.risk} /><span className="muted">{riskLabel(a.risk)}</span></span></td>
                    </tr>
                  );
                }) || []),
              ])}
            </tbody>
          </table>
        </div>
      </div>
      <p className="small muted mt16">Акции: цена базовой акции на бирже США (данные с задержкой до 15 минут, вне торговой сессии — цена закрытия). Токены компаний РБ — учебная модель с условной ставкой, реальные выпуски появятся после подключения площадки. Цена покупки в копилке = биржевая цена + спред партнёра {((config?.spread_bp || 100) / 100).toString().replace(".", ",")}%. Курс USDT в рублях повторяет официальный курс доллара НБРБ.</p>
    </div>
  );
}
