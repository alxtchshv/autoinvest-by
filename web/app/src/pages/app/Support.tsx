import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { ArrowLeft, Bug, Lightbulb, MessageSquare, Plus, Send } from "lucide-react";
import { api } from "../../lib/api";
import { ago, dateTime } from "../../lib/format";
import { useLoad, useSubmit, useTitle } from "../../lib/hooks";
import { useSession, useUserEvent } from "../../lib/session";
import type { Ticket } from "../../lib/types";
import { Accordion, Button, Empty, Field, Input, LoadError, Modal, Notice, PageLoader, useToast } from "../../components/ui";

const HELP: [string, string][] = [
  ["Когда спишутся деньги?", "В день и время, указанные в расписании копилки (по Минску). За минуту до этого вы можете поставить копилку на паузу — списания не будет."],
  ["Списание не прошло — что делать?", "Проверьте баланс и лимиты на онлайн-платежи в банке. Копилка повторит попытку через 6 часов, а после трёх отказов подряд встанет на паузу и пришлёт уведомление."],
  ["Как быстро приходит вывод?", "Активы продаются сразу. Зачисление на карту обычно занимает от нескольких минут до 1–3 рабочих дней в зависимости от банка."],
  ["Почему стоимость отличается от суммы пополнений?", "Копилка хранит активы, а не рубли. Их цена меняется каждую секунду, вместе с ней меняется и стоимость. Сколько вы вложили — показано отдельно."],
];

export function Support() {
  useTitle("Поддержка");
  const nav = useNavigate();
  const toast = useToast();
  const { data, error, loading, reload } = useLoad(() => api.get<{ tickets: Ticket[]; categories: Record<string, string> }>("/support/tickets"), []);
  const [open, setOpen] = useState(false);
  const [f, setF] = useState({ subject: "", category: "plans", body: "" });
  const s = useSubmit();
  const [sp, setSp] = useSearchParams();
  useUserEvent("notification", reload);
  const startFeedback = (category: "idea" | "bug") => {
    setF({ subject: category === "idea" ? "Идея: " : "Ошибка: ", category, body: "" });
    setOpen(true);
  };
  // ссылка «Предложить идею» из листа ожидания и плашки демо-счёта: /app/support?new=idea
  useEffect(() => {
    const n = sp.get("new");
    if (n === "idea" || n === "bug") { startFeedback(n); setSp({}, { replace: true }); }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const statusBadge = (st: string) => st === "answered" ? <span className="badge badge-brand">Есть ответ</span> : st === "open" ? <span className="badge badge-warn">Ждёт ответа</span> : <span className="badge">Закрыто</span>;
  return (
    <div className="stack g16">
      <div className="page-head" style={{ marginBottom: 4 }}>
        <div><h1>Поддержка</h1><p>Отвечаем в рабочие дни с 9:00 до 21:00, обычно в течение часа</p></div>
        <Button variant="primary" icon={<Plus />} onClick={() => setOpen(true)}>Новое обращение</Button>
      </div>
      <section className="sheet feedback-card">
        <div className="fb-text">
          <b>Помогите сделать AutoInvest лучше</b>
          <span>Есть идея или нашли ошибку? Напишите — за полезные предложения подарим дополнительное время Pro при запуске.</span>
        </div>
        <div className="row g8 wrap">
          <Button icon={<Lightbulb />} onClick={() => startFeedback("idea")}>Предложить идею</Button>
          <Button icon={<Bug />} onClick={() => startFeedback("bug")}>Сообщить об ошибке</Button>
        </div>
      </section>
      <div className="home-grid">
        <section className="sheet">
          <div className="sheet-head"><h2>Мои обращения</h2></div>
          {loading && !data ? <div className="sheet-body"><PageLoader /></div> : error ? <LoadError error={error} retry={reload} /> : data!.tickets.length === 0 ? (
            <Empty icon={<MessageSquare className="ico" />} title="Обращений пока нет">Опишите вопрос — ответ придёт сюда, на почту и в Telegram.</Empty>
          ) : (
            <div className="list">
              {data!.tickets.map((t) => (
                <Link key={t.id} to={`/app/support/${t.id}`} className="list-row">
                  <div className="grow">
                    <div className="title">{t.subject}</div>
                    <div className="sub" style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{t.last_message}</div>
                  </div>
                  <div className="end stack g4" style={{ alignItems: "flex-end" }}>{statusBadge(t.status)}<span className="tiny muted">{ago(t.updated_at)}</span></div>
                </Link>
              ))}
            </div>
          )}
        </section>
        <section className="sheet">
          <div className="sheet-head"><h2>Частые вопросы</h2></div>
          <div className="sheet-body faq-compact" style={{ paddingTop: 0, paddingBottom: 0 }}>
            <Accordion items={HELP} first={null} />
          </div>
          <div className="sheet-foot small muted">Почта: support@autoinvest.by</div>
        </section>
      </div>
      <Modal open={open} onClose={() => setOpen(false)} title="Новое обращение" wide>
        <form className="stack g16" onSubmit={async (e) => {
          e.preventDefault();
          try {
            const r = await s.submit(() => api.post<{ ticket: Ticket }>("/support/tickets", f));
            toast("Обращение отправлено");
            setOpen(false);
            if (r) nav(`/app/support/${r.ticket.id}`);
          } catch { /* */ }
        }}>
          <Field label="Тема" error={s.fieldError("subject")}><Input value={f.subject} onChange={(e) => setF({ ...f, subject: e.target.value })} maxLength={120} /></Field>
          <Field label="Раздел">
            <select className="select" value={f.category} onChange={(e) => setF({ ...f, category: e.target.value })}>
              {Object.entries(data?.categories || {}).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
            </select>
          </Field>
          <Field label="Сообщение" error={s.fieldError("body")} hint="Если вопрос про операцию — укажите её номер из чека">
            <textarea className="textarea" value={f.body} onChange={(e) => setF({ ...f, body: e.target.value })} maxLength={5000} />
          </Field>
          {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
          <div className="row g8" style={{ justifyContent: "flex-end" }}><Button onClick={() => setOpen(false)}>Отмена</Button><Button type="submit" variant="primary" busy={s.busy}>Отправить</Button></div>
        </form>
      </Modal>
    </div>
  );
}

export function ChatView({ ticket, mineIsStaff }: { ticket: Ticket; mineIsStaff: boolean }) {
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [ticket.messages?.length]);
  return (
    <div className="chat">
      {ticket.messages?.map((m) => (
        <div key={m.id} className={`msg ${m.is_staff === mineIsStaff ? "mine" : m.is_staff ? "staff" : ""}`}>
          <div className="meta">{m.author_name} · {dateTime(m.created_at)}</div>
          {m.body}
        </div>
      ))}
      <div ref={end} />
    </div>
  );
}

export function TicketPage() {
  const { id } = useParams();
  const { me } = useSession();
  const { data, error, loading, reload } = useLoad(() => api.get<{ ticket: Ticket }>(`/support/tickets/${id}`), [id]);
  const [body, setBody] = useState("");
  const s = useSubmit();
  useUserEvent("notification", reload);
  useTitle(data?.ticket.subject || "Обращение");
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const t = data!.ticket;
  return (
    <div className="stack g16" style={{ maxWidth: 760 }}>
      <div>
        <Link to="/app/support" className="back"><ArrowLeft />Поддержка</Link>
        <div className="page-head" style={{ marginBottom: 0 }}>
          <div><h1>{t.subject}</h1><p>Обращение №{t.id} · {dateTime(t.created_at)}</p></div>
          {t.status !== "closed" && <Button size="sm" onClick={async () => { await api.post(`/support/tickets/${t.id}/close`); reload(); }}>Вопрос решён</Button>}
        </div>
      </div>
      <section className="sheet sheet-pad"><ChatView ticket={t} mineIsStaff={false} /></section>
      <form className="sheet sheet-pad stack g12" onSubmit={async (e) => {
        e.preventDefault();
        try { await s.submit(() => api.post(`/support/tickets/${t.id}/messages`, { body })); setBody(""); reload(); } catch { /* */ }
      }}>
        {t.status === "closed" && <div className="small muted">Обращение закрыто. Напишите сообщение, чтобы открыть его снова.</div>}
        <textarea className="textarea" placeholder={`Ответ от имени ${me?.user?.name || ""}`} value={body} onChange={(e) => setBody(e.target.value)} aria-label="Сообщение" />
        {s.error && <Notice kind="error">{s.error}</Notice>}
        <div className="row" style={{ justifyContent: "flex-end" }}><Button type="submit" variant="primary" icon={<Send />} busy={s.busy} disabled={!body.trim()}>Отправить</Button></div>
      </form>
    </div>
  );
}
