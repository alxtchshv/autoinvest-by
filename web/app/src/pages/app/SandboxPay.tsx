import { useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Lock } from "lucide-react";
import { api } from "../../lib/api";
import { useLoad, useSubmit, useTitle } from "../../lib/hooks";
import { Button, Field, Input, LoadError, Notice, PageLoader } from "../../components/ui";

type TC = { number: string; brand: string; behavior: string; label: string };

/** Тестовая платёжная страница: имитирует hosted checkout bePaid в режиме PAYMENTS=sandbox. */
export default function SandboxPay() {
  useTitle("Тестовая оплата");
  const { ref } = useParams();
  const nav = useNavigate();
  const { data, error, loading } = useLoad(() => api.get<{ status: string; test_cards: TC[]; email: string; expired: boolean }>(`/sandbox/checkout/${ref}`), [ref]);
  const [f, setF] = useState({ number: "", exp: "", cvc: "", holder: "" });
  const s = useSubmit();
  const fmtNum = (v: string) => v.replace(/\D/g, "").slice(0, 16).replace(/(\d{4})(?=\d)/g, "$1 ");
  const fmtExp = (v: string) => { const d = v.replace(/\D/g, "").slice(0, 4); return d.length > 2 ? d.slice(0, 2) + "/" + d.slice(2) : d; };
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const [m, y] = f.exp.split("/");
    try {
      const r = await s.submit(() => api.post<{ redirect: string }>(`/sandbox/checkout/${ref}`, { number: f.number, holder: f.holder, exp_month: Number(m), exp_year: Number(y), cvc: f.cvc }));
      if (r) nav(r.redirect, { replace: true });
    } catch { /* */ }
  };
  return (
    <div className="paypage">
      <div className="box stack g16">
        <div className="row between small muted"><span className="row g8"><Lock size={14} />Защищённое соединение</span><span>Тестовая платёжная страница</span></div>
        {loading ? <PageLoader /> : error ? <LoadError error={error} /> : (
          <form className="sheet sheet-pad stack g16" onSubmit={submit}>
            <div>
              <h2>Привязка карты</h2>
              <p className="small muted mt8">AutoInvest · {data!.email}. Будет заблокировано 1,00 BYN и сразу возвращено.</p>
            </div>
            {data!.status !== "pending" || data!.expired ? (
              <Notice kind="warn">Сессия оплаты завершена. Вернитесь в приложение и начните привязку заново.</Notice>
            ) : (
              <>
                <Notice kind="warn">Это тестовый режим. Принимаются только тестовые карты ниже — не вводите данные настоящей карты.</Notice>
                <Field label="Номер карты" error={s.fieldError("number")}>
                  <Input inputMode="numeric" autoComplete="off" value={f.number} onChange={(e) => setF({ ...f, number: fmtNum(e.target.value) })} placeholder="0000 0000 0000 0000" autoFocus />
                </Field>
                <div className="grid2">
                  <Field label="Срок действия"><Input inputMode="numeric" autoComplete="off" placeholder="ММ/ГГ" value={f.exp} onChange={(e) => setF({ ...f, exp: fmtExp(e.target.value) })} /></Field>
                  <Field label="CVC" error={s.fieldError("cvc")}><Input inputMode="numeric" type="password" autoComplete="off" maxLength={3} value={f.cvc} onChange={(e) => setF({ ...f, cvc: e.target.value.replace(/\D/g, "") })} /></Field>
                </div>
                <Field label="Имя держателя"><Input autoComplete="off" value={f.holder} onChange={(e) => setF({ ...f, holder: e.target.value.toUpperCase() })} placeholder="IVAN IVANOV" /></Field>
                {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
                <Button type="submit" variant="primary" size="lg" block busy={s.busy}>Привязать карту</Button>
                <div className="testcards">
                  <div className="small muted" style={{ marginBottom: 4 }}>Тестовые карты (нажмите, чтобы подставить; срок — любой будущий, CVC — любые 3 цифры):</div>
                  {data!.test_cards.map((c) => (
                    <button type="button" key={c.number} onClick={() => setF({ number: fmtNum(c.number), exp: "12/" + String(new Date().getFullYear() + 2).slice(-2), cvc: "123", holder: f.holder || "TEST USER" })}>
                      <span className="num">{fmtNum(c.number)}</span><span className="muted">{c.label}</span>
                    </button>
                  ))}
                </div>
              </>
            )}
            <button type="button" className="link-btn small" onClick={() => nav("/app/cards", { replace: true })}>Отменить и вернуться в AutoInvest</button>
          </form>
        )}
      </div>
    </div>
  );
}
