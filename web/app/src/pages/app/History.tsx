import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Download, FileText, History as HistoryIcon } from "lucide-react";
import { Link } from "react-router-dom";
import { api } from "../../lib/api";
import { useLoad, useTitle } from "../../lib/hooks";
import { useUserEvent } from "../../lib/session";
import type { Order, Plan } from "../../lib/types";
import { Button, Empty, LoadError, PageLoader, Segmented } from "../../components/ui";
import { DatePicker, isoDate } from "../../components/DatePicker";
import { OperationRow } from "./PlanDetail";

export default function History() {
  useTitle("Операции");
  const [sp, setSp] = useSearchParams();
  const [side, setSide] = useState<"" | "buy" | "sell">("");
  const [page, setPage] = useState(1);
  const plan = sp.get("plan") || "";
  const plans = useLoad(() => api.get<{ plans: Plan[] }>("/plans?closed=1"), []);
  const { data, error, loading, reload } = useLoad(
    () => api.get<{ operations: Order[]; total: number; per: number }>(`/operations?page=${page}&per=30${side ? "&side=" + side : ""}${plan ? "&plan=" + plan : ""}`),
    [page, side, plan],
  );
  useUserEvent("plans", reload);
  const pages = data ? Math.max(1, Math.ceil(data.total / data.per)) : 1;
  const [period, setPeriod] = useState({ from: "", to: "" });
  const periodQS = new URLSearchParams(Object.entries(period).filter(([, v]) => v)).toString();
  return (
    <div>
      <div className="page-head">
        <div><h1>Операции</h1><p>Все пополнения и выводы с чеками</p></div>
      </div>
      <section className="sheet sheet-pad export-bar">
        <div className="row wrap g12" style={{ alignItems: "flex-end" }}>
          <div className="stack g4 small export-date"><span className="muted">С</span><DatePicker value={period.from} max={period.to || isoDate(new Date())} min="2020-01-01" onChange={(v) => setPeriod({ ...period, from: v })} placeholder="С начала" /></div>
          <div className="stack g4 small export-date"><span className="muted">По</span><DatePicker value={period.to} min={period.from || "2020-01-01"} max={isoDate(new Date())} onChange={(v) => setPeriod({ ...period, to: v })} placeholder="По сегодня" /></div>
          <div className="row g8 wrap grow" style={{ justifyContent: "flex-end" }}>
            <a href={`/api/v1/operations/export.csv${periodQS ? "?" + periodQS : ""}`} className="btn btn-secondary"><Download />Таблица для Excel</a>
            <Link to={`/app/history/statement${periodQS ? "?" + periodQS : ""}`} className="btn btn-secondary"><FileText />Выписка для печати и PDF</Link>
          </div>
        </div>
        <p className="small muted mt8">Без дат — за всё время. В выписке указан курс доллара НБРБ на момент каждой операции.</p>
      </section>
      <div className="row wrap g12 ops-filter">
        <Segmented label="Тип операции" value={side} onChange={(v) => { setSide(v); setPage(1); }} options={[{ value: "", label: "Все" }, { value: "buy", label: "Пополнения" }, { value: "sell", label: "Выводы" }]} />
        <select className="select" style={{ width: 240 }} value={plan} aria-label="Копилка" onChange={(e) => { setPage(1); const n = new URLSearchParams(sp); if (e.target.value) n.set("plan", e.target.value); else n.delete("plan"); setSp(n); }}>
          <option value="">Все копилки</option>
          {plans.data?.plans.map((p) => <option key={p.id} value={p.id}>{p.name}{p.status === "closed" ? " (закрыта)" : ""}</option>)}
        </select>
      </div>
      {loading && !data ? <PageLoader /> : error ? <LoadError error={error} retry={reload} /> : (
        <div className="sheet">
          {data!.operations.length === 0 ? (
            <Empty icon={<HistoryIcon className="ico" />} title="Операций пока нет">Здесь появятся пополнения копилок и выводы на карту.</Empty>
          ) : (
            <div className="list">{data!.operations.map((o) => <OperationRow key={o.id} o={o} showPlan />)}</div>
          )}
          {pages > 1 && (
            <div className="sheet-foot row between">
              <Button size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>Назад</Button>
              <span className="small muted">Страница {page} из {pages}</span>
              <Button size="sm" disabled={page >= pages} onClick={() => setPage(page + 1)}>Дальше</Button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
