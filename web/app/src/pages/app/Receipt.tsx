import { Link, useParams } from "react-router-dom";
import { ArrowLeft, Printer } from "lucide-react";
import { api } from "../../lib/api";
import { brandLabel, byn, dateTime, kop, price, qty } from "../../lib/format";
import { useLoad, useTitle } from "../../lib/hooks";
import type { Order } from "../../lib/types";
import { Button, LoadError, Logo, OrderStatus, PageLoader } from "../../components/ui";

const TRIGGER: Record<string, string> = { schedule: "по расписанию", manual: "вручную", dip: "докупка на просадке", close: "закрытие копилки" };

export default function Receipt() {
  const { id } = useParams();
  const { data, error, loading, reload } = useLoad(() => api.get<{ operation: Order }>(`/operations/${id}`), [id]);
  useTitle("Чек операции");
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const o = data!.operation;
  const buy = o.side === "buy";
  const net = o.amount_kop - o.fee_kop;
  return (
    <div className="receipt">
      <Link to="/app/history" className="back no-print"><ArrowLeft />Операции</Link>
      <section className="sheet">
        <div className="sheet-body stack g16">
          <div className="row between">
            <span className="row g8" style={{ fontWeight: 650 }}><Logo size={24} />AutoInvest</span>
            <OrderStatus s={o.status} />
          </div>
          <div>
            <div className="muted small">{buy ? "Пополнение копилки" : "Вывод из копилки"} «{o.plan_name}»</div>
            <div className="amount num">{buy ? "−" : "+"}{byn(o.amount_kop)}</div>
            <div className="small muted">{dateTime(o.created_at)} · {TRIGGER[o.trigger] || o.trigger}</div>
          </div>
          {o.status === "failed" && <div className="notice error">{o.error || "Операция отклонена"}. Деньги не списаны.</div>}
          {o.status === "payout_pending" && <div className="notice warn">{o.error}</div>}
          {o.fills.length > 0 && (
            <div className="table-wrap" style={{ margin: "0 -16px" }}>
              <table className="table">
                <thead><tr><th>Актив</th><th className="r">Количество</th><th className="r">Цена</th><th className="r">Сумма</th></tr></thead>
                <tbody>
                  {o.fills.map((f, i) => (
                    <tr key={i}><td>{f.asset}</td><td className="r num">{qty(f.qty)}</td><td className="r num">{price(f.price_byn)}<div className="tiny muted">{price(f.price_usd, "$")}</div></td><td className="r num">{kop(f.amount_kop)}</td></tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <dl className="kv">
            <dt>Номер операции</dt><dd>AI-{String(o.id).padStart(8, "0")}</dd>
            {buy && <><dt>Списано с карты</dt><dd>{byn(o.amount_kop)}</dd></>}
            {buy && <><dt>Комиссия сервиса</dt><dd>{byn(o.fee_kop)}</dd></>}
            {buy && <><dt>Направлено на покупку</dt><dd>{byn(net)}</dd></>}
            {!buy && <><dt>К выплате на карту</dt><dd>{byn(o.amount_kop)}</dd></>}
            <dt>Карта</dt><dd>{o.card_last4 ? `${brandLabel(o.card_brand)} •• ${o.card_last4}` : "—"}</dd>
            <dt>Курс USD НБРБ</dt><dd>{o.usd_byn ? price(o.usd_byn) : "—"}</dd>
            <dt>Платёжный шлюз</dt><dd>{o.provider === "bepaid" ? "bePaid" : o.provider === "sandbox" ? "Тестовая платёжка" : o.provider || "—"}{o.test ? " (тестовый платёж)" : ""}</dd>
            {o.payment_uid && <><dt>ID транзакции</dt><dd style={{ wordBreak: "break-all" }}>{o.payment_uid}</dd></>}
            <dt>Исполнение</dt><dd>{o.broker === "paper" ? "Учебный счёт: учтено по биржевой цене, у партнёра не исполнялось" : "Исполнено через оператора криптоплатформы — резидента ПВТ"}</dd>
            {o.filled_at && <><dt>Исполнено</dt><dd>{dateTime(o.filled_at)}</dd></>}
          </dl>
        </div>
        <div className="sheet-foot row between no-print">
          <Link to={`/app/plans/${o.plan_id}`} className="small">Открыть копилку</Link>
          <Button size="sm" icon={<Printer />} onClick={() => window.print()}>Сохранить в PDF</Button>
        </div>
      </section>
    </div>
  );
}
