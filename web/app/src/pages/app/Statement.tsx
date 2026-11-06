import { Link, useSearchParams } from "react-router-dom";
import { ArrowLeft, Printer } from "lucide-react";
import { api } from "../../lib/api";
import { brandLabel, byn, date, dateTime, kop, price, qty } from "../../lib/format";
import { useLoad, useTitle } from "../../lib/hooks";
import type { Order } from "../../lib/types";
import { Button, LoadError, Logo, Notice, PageLoader } from "../../components/ui";

type StatementData = {
  holder: { name: string; email: string };
  from: number; to: number; generated_at: number; total: number; truncated: boolean;
  operations: (Order & { venue: string })[];
  totals: { deposited_kop: number; withdrawn_kop: number; fees_kop: number };
  live_broker: boolean; test_money: boolean;
};

const STATUS: Record<string, string> = { filled: "Исполнено", failed: "Отклонено", pending: "В обработке", payout_pending: "Ожидает выплаты", manual: "На проверке" };

// Печатная выписка по операциям за период: открывается из «Операций», сохраняется в PDF через печать браузера.
export default function Statement() {
  useTitle("Выписка по операциям");
  const [sp] = useSearchParams();
  const q = new URLSearchParams();
  if (sp.get("from")) q.set("from", sp.get("from")!);
  if (sp.get("to")) q.set("to", sp.get("to")!);
  const { data, error, loading, reload } = useLoad(() => api.get<StatementData>(`/operations/statement?${q}`), [q.toString()]);
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const s = data!;
  const period = s.from || s.to ? `${s.from ? date(s.from) : "начала"} — ${s.to ? date(s.to - 1) : date(s.generated_at)}` : "за всё время";
  return (
    <div className="statement">
      <div className="row between no-print" style={{ marginBottom: 12 }}>
        <Link to="/app/history" className="back"><ArrowLeft />Операции</Link>
        <Button icon={<Printer />} onClick={() => window.print()}>Сохранить в PDF</Button>
      </div>
      <section className="sheet">
        <div className="sheet-body stack g16">
          <div className="row between">
            <span className="row g8" style={{ fontWeight: 650 }}><Logo size={24} />AutoInvest</span>
            <span className="small muted">Сформировано {dateTime(s.generated_at)}</span>
          </div>
          <div>
            <h1 style={{ fontSize: 22 }}>Выписка по операциям</h1>
            <div className="muted">{s.holder.name} · {s.holder.email} · период: {period}</div>
          </div>
          {!s.live_broker && <Notice kind="warn">Учебный счёт: операции учтены по реальным биржевым ценам, но у оператора криптоплатформы не исполнялись{s.test_money ? ", деньги с карт не списывались" : ""}.</Notice>}
          <dl className="kv">
            <dt>Пополнено</dt><dd>{byn(s.totals.deposited_kop)}</dd>
            <dt>Выведено</dt><dd>{byn(s.totals.withdrawn_kop)}</dd>
            <dt>Комиссии сервиса</dt><dd>{byn(s.totals.fees_kop)}</dd>
            <dt>Операций</dt><dd>{s.total}{s.truncated ? " (показаны последние 500)" : ""}</dd>
          </dl>
          {s.operations.length === 0 ? <p className="muted">За этот период операций нет.</p> : (
            <div className="table-wrap" style={{ margin: "0 -16px" }}>
              <table className="table statement-table">
                <thead><tr><th>№ / дата</th><th>Операция</th><th>Активы</th><th className="r">Сумма</th><th className="r">Курс USD НБРБ</th></tr></thead>
                <tbody>
                  {s.operations.map((o) => (
                    <tr key={o.id}>
                      <td className="nowrap">AI-{String(o.id).padStart(8, "0")}<div className="tiny muted">{dateTime(o.created_at)}</div></td>
                      <td>{o.side === "buy" ? "Пополнение" : "Вывод"} «{o.plan_name}»<div className="tiny muted">{STATUS[o.status] || o.status}{o.card_last4 ? ` · ${brandLabel(o.card_brand)} •• ${o.card_last4}` : ""}</div><div className="tiny muted">{o.venue}</div></td>
                      <td className="small">{o.fills.length ? o.fills.map((f) => <div key={f.asset} className="nowrap">{qty(f.qty)} {f.asset} по {price(f.price_byn)}</div>) : "—"}</td>
                      <td className="r num nowrap">{o.side === "buy" ? "−" : "+"}{kop(o.amount_kop)}{o.fee_kop > 0 && <div className="tiny muted">комиссия {kop(o.fee_kop)}</div>}</td>
                      <td className="r num">{o.usd_byn ? price(o.usd_byn) : "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <p className="tiny muted">Суммы в белорусских рублях. Курс — официальный курс доллара США Национального банка Республики Беларусь на момент операции. Выписка сформирована автоматически и не требует подписи.</p>
        </div>
      </section>
    </div>
  );
}
