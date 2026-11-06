import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { CreditCard, Lock, Plus } from "lucide-react";
import { api } from "../../lib/api";
import { brandLabel } from "../../lib/format";
import { useLoad, useSubmit, useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { Card } from "../../lib/types";
import { findBank, type Bank } from "../../lib/banks";
import { Button, Empty, LoadError, Modal, Notice, PageLoader, useToast } from "../../components/ui";

function BankLogo({ bank, issuer }: { bank: Bank | null; issuer: string }) {
  const [img, setImg] = useState(true);
  if (bank && img) return <img className="bc-logo" src={`/banks/${bank.id}.svg`} alt="" onError={() => setImg(false)} />;
  return <span className="bc-mono">{bank ? bank.short : (issuer.trim()[0] || "•").toUpperCase()}</span>;
}

export function BankCard({ c }: { c: Card }) {
  const bank = findBank(c.issuer);
  const style = bank ? { background: `linear-gradient(135deg, ${bank.color} 0%, ${bank.color2} 100%)` } : undefined;
  return (
    <div className={`bank-card ${bank ? "branded" : c.brand.toLowerCase()}`} style={style}>
      <div className="row between">
        <span className="bc-bank"><BankLogo bank={bank} issuer={c.issuer} /><span>{bank?.name || c.issuer || "Банк карты"}</span></span>
        <span className="chip-ic" />
      </div>
      <div className="bc-num">{c.mask || `•••• •••• •••• ${c.last4}`}</div>
      <div className="bc-row">
        <span>{c.holder || "—"}<br />{String(c.exp_month).padStart(2, "0")}/{String(c.exp_year).slice(-2)}</span>
        <span className="bc-brand">{brandLabel(c.brand)}</span>
      </div>
    </div>
  );
}

export function useBindCard() {
  const toast = useToast();
  const s = useSubmit();
  const start = async () => {
    try {
      const r = await s.submit(() => api.post<{ redirect_url: string }>("/cards/bind"));
      if (r?.redirect_url) window.location.href = r.redirect_url;
    } catch (e: any) {
      toast(e.message, "error");
    }
  };
  return { start, busy: s.busy };
}

export default function Cards() {
  useTitle("Карты");
  const { reload: reloadMe } = useSession();
  const toast = useToast();
  const [sp, setSp] = useSearchParams();
  const { data, error, loading, reload } = useLoad(() => api.get<{ cards: Card[]; provider: string; test: boolean }>("/cards"), []);
  const bind = useBindCard();
  const [binding, setBinding] = useState<null | "wait" | "done" | "failed">(null);
  const [bindErr, setBindErr] = useState("");
  const [del, setDel] = useState<Card | null>(null);
  const delS = useSubmit();

  // возврат со страницы оплаты: опрашиваем статус привязки.
  // Эффект зависит только от номера привязки: перезагрузка профиля не должна запускать второй опрос и второй тост.
  const bindRef = sp.get("bind");
  useEffect(() => {
    const ref = bindRef;
    if (!ref) return;
    setBinding("wait");
    let n = 0, stop = false;
    const tick = async () => {
      if (stop) return;
      try {
        const r = await api.get<{ status: string; error: string }>(`/cards/bindings/${ref}`);
        if (stop) return;
        if (r.status === "done") { setBinding("done"); reload(); reloadMe(); toast("Карта привязана"); setSp({}, { replace: true }); return; }
        if (r.status === "failed") { setBinding("failed"); setBindErr(r.error); setSp({}, { replace: true }); return; }
      } catch { /* повторим */ }
      if (++n < 20) setTimeout(tick, 2000);
      else setBinding(null);
    };
    tick();
    return () => { stop = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bindRef]);

  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const cards = data!.cards;
  return (
    <div className="stack g16">
      <div className="page-head" style={{ marginBottom: 4 }}>
        <div><h1>Карты</h1><p>С них пополняются копилки и на них выводятся деньги</p></div>
        <Button variant="primary" icon={<Plus />} busy={bind.busy} onClick={bind.start} disabled={cards.length >= 5}>Привязать карту</Button>
      </div>
      {binding === "wait" && <Notice kind="info">Проверяем привязку карты…</Notice>}
      {binding === "failed" && <Notice kind="error" title="Карта не привязана">{bindErr || "Банк отклонил операцию"}. Попробуйте ещё раз или другую карту.</Notice>}
      {cards.length === 0 ? (
        <div className="sheet">
          <Empty icon={<CreditCard className="ico" />} title="Карт пока нет" action={<Button variant="primary" busy={bind.busy} onClick={bind.start}>Привязать карту</Button>}>
            Подойдёт карта любого белорусского банка: Visa, Mastercard или Белкарт.
          </Empty>
        </div>
      ) : (
        <div className="cards-grid">
          {cards.map((c) => (
            <div key={c.id} className="stack g12">
              <BankCard c={c} />
              <div className="row g8 wrap">
                {c.is_default ? <span className="badge badge-brand">Основная</span> : (
                  <Button size="sm" onClick={async () => { await api.post(`/cards/${c.id}/default`); reload(); toast("Основная карта изменена"); }}>Сделать основной</Button>
                )}
                <Button size="sm" variant="ghost" onClick={() => setDel(c)}>Удалить</Button>
              </div>
            </div>
          ))}
        </div>
      )}
      <div className="notice">
        <Lock />
        <div>Номер карты и CVC вы вводите на странице {data!.provider === "bepaid" ? "платёжного агрегатора bePaid" : "тестовой платёжки"} с 3-D Secure. AutoInvest хранит только токен для списаний, первые 6 и последние 4 цифры номера — так разрешает стандарт PCI DSS. Полный номер и CVC к нам не попадают. Привязку можно отменить в любой момент.</div>
      </div>
      <Modal open={!!del} onClose={() => setDel(null)} title="Удалить карту?">
        <div className="stack g16">
          <p>Карта •• {del?.last4} больше не будет использоваться. Копилки на ней перейдут на основную карту, а если других карт нет — встанут на паузу.</p>
          <div className="row g8" style={{ justifyContent: "flex-end" }}>
            <Button onClick={() => setDel(null)}>Отмена</Button>
            <Button variant="danger" busy={delS.busy} onClick={async () => {
              try {
                const r = await delS.submit(() => api.del<{ paused_plans: number }>(`/cards/${del!.id}`));
                toast(r?.paused_plans ? `Карта удалена, ${r.paused_plans} копилок на паузе` : "Карта удалена");
                setDel(null); reload(); reloadMe();
              } catch (e: any) { toast(e.message, "error"); }
            }}>Удалить</Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
