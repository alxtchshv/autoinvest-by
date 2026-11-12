import { WaitlistHomeCard } from "../../components/Waitlist";
import { useState } from "react";
import { Link } from "react-router-dom";
import { ArrowDownToLine, CalendarClock, CreditCard, PiggyBank, Plus } from "lucide-react";
import { api } from "../../lib/api";
import { byn, pct, price, when } from "../../lib/format";
import { useLoad, useTitle } from "../../lib/hooks";
import { useMarket, useSession, useUserEvent } from "../../lib/session";
import type { Plan, Rates, Snapshot, Valuation } from "../../lib/types";
import { AssetIcon, Change, Empty, LoadError, Money, Notice, PageLoader, PlanIcon, Segmented, useHide } from "../../components/ui";
import { AssetSpark, ValueChart } from "../../components/charts";

type Portfolio = {
  valuation: Valuation; plans: Plan[];
  upcoming: { plan_id: number; plan_name: string; at: number; amount_kop: number; card_last4: string }[];
  monthly_plan_kop: number; month_invested_kop: number; rates: Rates;
};

const PERIODS = [
  { value: "7d", label: "Неделя" },
  { value: "1m", label: "Месяц" },
  { value: "3m", label: "3 месяца" },
  { value: "1y", label: "Год" },
  { value: "all", label: "Всё время" },
] as const;

function greeting() {
  const h = Number(new Intl.DateTimeFormat("ru-RU", { hour: "numeric", hourCycle: "h23", timeZone: "Europe/Minsk" }).format(new Date()));
  if (h < 5) return "Доброй ночи";
  if (h < 12) return "Доброе утро";
  if (h < 18) return "Добрый день";
  return "Добрый вечер";
}

export default function Home() {
  useTitle("Главная");
  const { me, config } = useSession();
  const { quotes } = useMarket();
  const { hidden } = useHide();
  const [period, setPeriod] = useState<(typeof PERIODS)[number]["value"]>("1m");
  const pf = useLoad(() => api.get<Portfolio>("/portfolio"), []);
  const hist = useLoad(() => api.get<{ points: Snapshot[] }>("/portfolio/history?period=" + period), [period]);
  useUserEvent("plans", () => { pf.reload(); hist.reload(); });

  if (pf.loading && !pf.data) return <PageLoader />;
  if (pf.error) return <LoadError error={pf.error} retry={pf.reload} />;
  const d = pf.data!;
  const v = d.valuation;
  const hasMoney = v.invested_kop > 0;
  const firstName = me?.user?.name.split(" ")[0] || "";
  return (
    <div className="stack g16">
      <div className="page-head" style={{ marginBottom: 4 }}>
        <div>
          <h1>{greeting()}, {firstName}</h1>
          <p>{d.plans.length ? (d.plans.some((p) => p.waiting) ? `Копилок настроено: ${d.plans.length} — ждут запуска` : `${d.plans.filter((p) => p.status === "active").length} из ${d.plans.length} копилок работают`) : "Начните с первой копилки — это займёт пару минут"}</p>
        </div>
      </div>

      {!me?.user?.onboarded_at && (
        <Notice kind="info" title="Подберём копилку за 4 вопроса">Ответьте, сколько готовы откладывать и как относитесь к риску, — предложим состав и создадим копилку в один клик. <Link to="/app/onboarding">Подобрать план</Link></Notice>
      )}
      {config?.launch === "waitlist" && !me?.demo ? <WaitlistHomeCard /> : !me?.cards && <Notice kind="warn" title="Привяжите карту">Без карты копилки не смогут пополняться. <Link to="/app/cards">Привязать карту</Link></Notice>}
      {config?.require_kyc && me?.user?.kyc_status !== "approved" && (
        <Notice kind="info" title="Подтвердите личность">Для покупок нужна верификация по паспорту. <Link to="/app/profile/verification">Пройти верификацию</Link></Notice>
      )}

      <div className="home-grid">
        <div className="stack g16">
          <section className="sheet sheet-pad">
            <div className="balance-head">
              <div>
                <div className="muted small">Стоимость всех копилок</div>
                <Money k={v.value_kop} size="xl" />
                {hasMoney && (
                  <div className="row g12 wrap mt8 small">
                    <span className={`num ${v.pnl_kop >= 0 ? "up" : "down"}`} style={{ fontWeight: 600 }}>{hidden ? "•••" : `${byn(v.pnl_kop, { sign: true })} · ${pct(v.pnl_pct)}`}</span>
                    <span className="muted">за всё время</span>
                    <span className="muted">·</span>
                    <span>за сутки <Change k={v.day_pnl_kop} /></span>
                  </div>
                )}
              </div>
              <div className="quick">
                <Link to="/app/plans/new" className="btn btn-primary"><Plus />Новая копилка</Link>
              </div>
            </div>
            {hasMoney && (
              <div className="balance-meta">
                <div><span className="l">Вложено</span><Money k={v.invested_kop} /></div>
                <div><span className="l">Пополнено в этом месяце</span><Money k={d.month_invested_kop} /></div>
                <div><span className="l">По плану в месяц</span><Money k={d.monthly_plan_kop} /></div>
              </div>
            )}
            {v.stale && <div className="small muted mt16">Часть котировок обновляется — оценка может отличаться.</div>}
          </section>

          {hasMoney && (
            <section className="sheet">
              <div className="sheet-head">
                <h2>Динамика</h2>
                <Segmented label="Период" value={period} onChange={setPeriod} options={PERIODS.map((p) => ({ ...p }))} />
              </div>
              <div className="sheet-body">
                {hist.data && hist.data.points.length >= 2 ? <ValueChart points={hist.data.points} /> : <div className="muted small" style={{ padding: "40px 0", textAlign: "center" }}>График появится, когда накопится история — снимок стоимости записывается каждый час.</div>}
              </div>
            </section>
          )}

          <section className="sheet">
            <div className="sheet-head">
              <h2>Копилки</h2>
              <Link to="/app/plans" className="small">Все копилки</Link>
            </div>
            {d.plans.length === 0 ? (
              <Empty icon={<PiggyBank className="ico" />} title="Пока нет ни одной копилки" action={<Link to="/app/plans/new" className="btn btn-primary">Создать копилку</Link>}>
                Выберите цель, сумму и расписание — дальше пополнения пойдут автоматически.
              </Empty>
            ) : (
              <div className="list">
                {d.plans.slice(0, 5).map((p) => (
                  <Link key={p.id} to={`/app/plans/${p.id}`} className="list-row">
                    <PlanIcon icon={p.icon} />
                    <div className="grow">
                      <div className="title">{p.name}</div>
                      <div className="sub">{p.waiting ? "Ждёт запуска" : p.status === "paused" ? "На паузе" : p.goal_kop ? `${(p.goal_progress_bp / 100).toFixed(0)}% от цели ${byn(p.goal_kop).replace(",00", "")}` : `${byn(p.amount_kop)} · ${p.frequency === "manual" ? "вручную" : "по расписанию"}`}</div>
                    </div>
                    <div className="end">
                      <Money k={p.valuation.value_kop} />
                      {p.valuation.invested_kop > 0 && <div className="small"><Change v={p.valuation.pnl_pct} /></div>}
                    </div>
                  </Link>
                ))}
              </div>
            )}
          </section>
        </div>

        <div className="stack g16">
          <section className="sheet">
            <div className="sheet-head"><h2>Ближайшие пополнения</h2></div>
            {d.upcoming.length === 0 ? (
              <div className="sheet-body muted small">Нет запланированных списаний.</div>
            ) : (
              <div className="list">
                {d.upcoming.map((u) => (
                  <Link key={u.plan_id} to={`/app/plans/${u.plan_id}`} className="list-row">
                    <CalendarClock size={20} className="muted" />
                    <div className="grow">
                      <div className="title">{when(u.at)}</div>
                      <div className="sub">{u.plan_name}{u.card_last4 ? ` · карта •• ${u.card_last4}` : ""}</div>
                    </div>
                    <div className="end"><Money k={u.amount_kop} /></div>
                  </Link>
                ))}
              </div>
            )}
          </section>

          {v.positions.length > 0 && (
            <section className="sheet">
              <div className="sheet-head"><h2>Из чего состоит</h2></div>
              <div className="sheet-body stack g12">
                <div className="alloc-bar">
                  {v.positions.map((p) => <span key={p.asset} style={{ width: p.share_bp / 100 + "%", background: config?.assets.find((a) => a.symbol === p.asset)?.color }} />)}
                </div>
                {v.positions.map((p) => (
                  <div key={p.asset} className="row g12">
                    <AssetIcon symbol={p.asset} size="sm" />
                    <span className="grow">{config?.assets.find((a) => a.symbol === p.asset)?.name}<span className="muted small"> · {(p.share_bp / 100).toFixed(1).replace(".", ",")}%</span></span>
                    <Money k={p.value_kop} />
                  </div>
                ))}
              </div>
            </section>
          )}

          <section className="sheet">
            <div className="sheet-head">
              <h2>Курсы</h2>
              <Link to="/app/market" className="small">Рынок</Link>
            </div>
            <div className="list">
              {config?.assets.filter((a) => !a.pro_only).map((a) => {
                const q = quotes[a.symbol];
                return (
                  <Link key={a.symbol} to={`/app/market/${a.symbol}`} className="list-row ticker-row">
                    <AssetIcon symbol={a.symbol} size="sm" />
                    <span className="title" style={{ fontSize: 14 }}>{a.name}</span>
                    <span className="spark">{a.kind !== "stable" && <AssetSpark symbol={a.symbol} width={64} height={24} />}</span>
                    <span className="end num small">
                      <div style={{ fontWeight: 600 }}>{q ? price(q.price_byn) : "—"}</div>
                      {q && a.kind !== "stable" && <Change v={q.change_24h} />}
                    </span>
                  </Link>
                );
              })}
            </div>
            {d.rates?.usd > 0 && <div className="sheet-foot tiny muted">Официальный курс НБРБ: USD {price(d.rates.usd, "")} · EUR {price(d.rates.eur, "")} · 100 RUB {price(d.rates.rub100, "")}</div>}
          </section>

          <div className="row g8 wrap">
            <Link to="/app/cards" className="btn btn-secondary btn-sm"><CreditCard />Карты</Link>
            <Link to="/app/history" className="btn btn-secondary btn-sm"><ArrowDownToLine />Выписка</Link>
          </div>
        </div>
      </div>
    </div>
  );
}
