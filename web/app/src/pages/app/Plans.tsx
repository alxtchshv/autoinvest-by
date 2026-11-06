import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { Plus, Search } from "lucide-react";
import { api } from "../../lib/api";
import { byn, scheduleText, when } from "../../lib/format";
import { useLoad, useTitle } from "../../lib/hooks";
import { useSession, useUserEvent } from "../../lib/session";
import type { Plan } from "../../lib/types";
import { Change, Empty, Input, LoadError, Money, PageLoader, PlanIcon, PlanStatus, Segmented } from "../../components/ui";

export function AllocBar({ plan }: { plan: Plan }) {
  const { config } = useSession();
  return (
    <div className="alloc-bar" title={plan.allocations.map((a) => `${a.asset} ${a.weight_bp / 100}%`).join(", ")}>
      {plan.allocations.map((a) => <span key={a.asset} style={{ width: a.weight_bp / 100 + "%", background: config?.assets.find((x) => x.symbol === a.asset)?.color }} />)}
    </div>
  );
}

function PlanCard({ p }: { p: Plan }) {
  const v = p.valuation;
  return (
    <Link to={`/app/plans/${p.id}`} className={`sheet plan-card ${p.status !== "active" ? "paused" : ""}`}>
      <div className="pc-head">
        <PlanIcon icon={p.icon} />
        <div className="grow" style={{ minWidth: 0 }}>
          <div className="pc-name">{p.name}</div>
          <div className="small muted">{p.frequency === "manual" ? "Пополнение вручную" : `${byn(p.amount_kop)} ${scheduleText(p)}`}</div>
        </div>
        <PlanStatus s={p.status} />
      </div>
      <div className="row between" style={{ alignItems: "flex-end" }}>
        <Money k={v.value_kop} size="lg" />
        {v.invested_kop > 0 && <span className="small"><Change k={v.pnl_kop} /> <Change v={v.pnl_pct} className="muted" /></span>}
      </div>
      {p.goal_kop ? (
        <div>
          <div className="bar"><span style={{ width: p.goal_progress_bp / 100 + "%" }} /></div>
          <div className="goal-line"><span>{(p.goal_progress_bp / 100).toFixed(0)}% от цели</span><span>{byn(p.goal_kop)}</span></div>
        </div>
      ) : (
        <AllocBar plan={p} />
      )}
      <div className="pc-foot">
        <span>{p.status === "active" && p.next_run_at ? `Следующее: ${when(p.next_run_at)}` : p.status === "paused" ? "Пополнения остановлены" : p.status === "closed" ? "Закрыта" : "Без расписания"}</span>
        {p.card && <span>•• {p.card.last4}</span>}
      </div>
    </Link>
  );
}

export default function Plans() {
  useTitle("Копилки");
  const [filter, setFilter] = useState<"open" | "active" | "paused" | "closed">("open");
  const [q, setQ] = useState("");
  const { data, error, loading, reload } = useLoad(() => api.get<{ plans: Plan[]; limit: number }>("/plans" + (filter === "closed" ? "?closed=1" : "")), [filter === "closed"]);
  useUserEvent("plans", reload);
  const plans = useMemo(() => {
    let l = data?.plans || [];
    if (filter === "active") l = l.filter((p) => p.status === "active");
    if (filter === "paused") l = l.filter((p) => p.status === "paused");
    if (filter === "closed") l = l.filter((p) => p.status === "closed");
    if (filter === "open") l = l.filter((p) => p.status !== "closed");
    if (q.trim()) l = l.filter((p) => p.name.toLowerCase().includes(q.trim().toLowerCase()));
    return l;
  }, [data, filter, q]);
  const open = (data?.plans || []).filter((p) => p.status !== "closed").length;
  const total = plans.reduce((s, p) => s + p.valuation.value_kop, 0);

  return (
    <div>
      <div className="page-head">
        <div>
          <h1>Копилки</h1>
          {data && <p>{open} из {data.limit} · всего <Money k={total} /></p>}
        </div>
        <div className="row g8 wrap">
          <Link to="/app/onboarding" className="btn btn-secondary">Подобрать стратегию</Link>
          <Link to="/app/plans/new" className="btn btn-primary"><Plus />Новая копилка</Link>
        </div>
      </div>
      <div className="row between wrap g12" style={{ marginBottom: 16 }}>
        <Segmented label="Фильтр" value={filter} onChange={setFilter} options={[
          { value: "open", label: "Все" }, { value: "active", label: "Работают" }, { value: "paused", label: "На паузе" }, { value: "closed", label: "Закрытые" },
        ]} />
        {(data?.plans.length || 0) > 6 && (
          <div style={{ position: "relative", width: 260, maxWidth: "100%" }}>
            <Search size={16} className="muted" style={{ position: "absolute", left: 12, top: 14 }} />
            <Input placeholder="Найти копилку" value={q} onChange={(e) => setQ(e.target.value)} style={{ paddingLeft: 36 }} aria-label="Поиск копилки" />
          </div>
        )}
      </div>
      {loading && !data ? <PageLoader /> : error ? <LoadError error={error} retry={reload} /> : plans.length === 0 ? (
        <div className="sheet">
          <Empty title={filter === "closed" ? "Закрытых копилок нет" : q ? "Ничего не найдено" : "Пока нет копилок"} action={filter === "open" && !q && <Link to="/app/plans/new" className="btn btn-primary">Создать копилку</Link>}>
            {filter === "open" && !q ? "Копилка — это цель, сумма и расписание. Пополнения будут приходить автоматически." : null}
          </Empty>
        </div>
      ) : (
        <div className="plans-grid">
          {plans.map((p) => <PlanCard key={p.id} p={p} />)}
          {filter === "open" && !q && data && open < data.limit && (
            <Link to="/app/plans/new" className="plan-add"><Plus size={20} />Новая копилка</Link>
          )}
        </div>
      )}
    </div>
  );
}
