// Тестовый Telegram: чат с настоящим ботом AutoInvest, но без настоящего Telegram (стенд, TELEGRAM_SANDBOX).
// Когда в .env появится TELEGRAM_BOT_TOKEN, вместо этой страницы откроется t.me/<бот>.
import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { ArrowLeft, Send } from "lucide-react";
import { api } from "../../lib/api";
import { useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import { Notice, useToast } from "../../components/ui";

type Btn = { text: string; data?: string; url?: string; contact?: boolean };
type Msg = { id: number; rev: number; from_bot: boolean; text: string; html: boolean; buttons?: Btn[][]; at: number };

/** Бот присылает HTML с <b>/<i>; всё остальное показываем как текст. */
function botHtml(t: string) {
  const esc = t.replace(/&(?!(amp|lt|gt|quot);)/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;"); // кавычки — чтобы ссылка не вышла за href
  return esc.replace(/&lt;(\/?)(b|i)&gt;/g, "<$1$2>").replace(/(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank" rel="noreferrer">$1</a>').replace(/\n/g, "<br>");
}

const time = (s: number) => new Date(s * 1000).toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });

export default function TelegramSandbox() {
  useTitle("Тестовый Telegram");
  const { me, reload: reloadMe } = useSession();
  const toast = useToast();
  const nav = useNavigate();
  const [sp, setSp] = useSearchParams();
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [bot, setBot] = useState("");
  const [kb, setKb] = useState<Btn[]>([]);
  const [text, setText] = useState("");
  const [err, setErr] = useState("");
  const rev = useRef(0);
  const end = useRef<HTMLDivElement>(null);
  const started = useRef(false);

  // длинный опрос: сервер отвечает сразу, как только бот что-то прислал или изменил
  useEffect(() => {
    let stop = false;
    const loop = async () => {
      while (!stop) {
        try {
          const r = await api.get<{ bot: string; messages: Msg[]; rev: number; keyboard?: Btn[] }>(`/telegram/sandbox?after=${rev.current}&wait=${rev.current ? 1 : 0}`);
          if (stop) return;
          setBot(r.bot);
          setKb(r.keyboard || []);
          setErr("");
          if (r.messages.length) {
            setMsgs((old) => {
              const m = new Map(old.map((x) => [x.id, x]));
              r.messages.forEach((x) => m.set(x.id, x));
              return [...m.values()].sort((a, b) => a.id - b.id);
            });
          }
          rev.current = r.rev;
        } catch (e: any) {
          setErr(e.message);
          await new Promise((res) => setTimeout(res, 3000));
        }
      }
    };
    loop();
    return () => { stop = true; };
  }, []);

  // пришли по ссылке «Подключить Telegram» — сразу отправляем /start с кодом, как это делает Telegram
  useEffect(() => {
    const code = sp.get("start");
    if (!code || started.current) return;
    started.current = true;
    api.post("/telegram/sandbox/send", { text: "/start " + code }).then(() => setTimeout(reloadMe, 600)).catch((e) => toast(e.message, "error"));
    setSp({}, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => { end.current?.scrollIntoView({ block: "end", behavior: "smooth" }); }, [msgs]);

  const send = async (e?: FormEvent, t = text) => {
    e?.preventDefault();
    if (!t.trim()) return;
    setText("");
    try { await api.post("/telegram/sandbox/send", { text: t }); } catch (x: any) { toast(x.message, "error"); }
  };
  const press = async (m: Msg, b: Btn) => {
    if (b.url) { window.open(b.url, "_blank", "noopener"); return; }
    try { await api.post("/telegram/sandbox/press", { message_id: m.id, data: b.data }); } catch (x: any) { toast(x.message, "error"); }
  };

  return (
    <div className="tg-page">
      <div className="tg-window">
        <header className="tg-head">
          <button type="button" className="tg-back" onClick={() => nav("/app/profile/notifications")} aria-label="Назад в профиль"><ArrowLeft /></button>
          <span className="tg-ava">AI</span>
          <div className="grow"><b>AutoInvest</b><span>@{bot || "…"} · тестовый Telegram</span></div>
        </header>
        <div className="tg-body">
          <Notice kind="warn">Это имитация Telegram для стенда: бот настоящий, сообщения никуда не уходят. Когда в настройках появится токен бота, здесь будет обычный Telegram.</Notice>
          {err && <Notice kind="error">{err}</Notice>}
          {msgs.length === 0 && <div className="tg-empty">Напишите /start или /menu</div>}
          {msgs.map((m) => (
            <div key={m.id} className={`tg-msg ${m.from_bot ? "bot" : "me"}`}>
              <div className="tg-bubble">
                {m.from_bot ? <span dangerouslySetInnerHTML={{ __html: botHtml(m.text) }} /> : m.text}
                <span className="tg-time">{time(m.at)}</span>
              </div>
              {m.buttons && m.buttons.length > 0 && (
                <div className="tg-kb">
                  {m.buttons.map((row, i) => (
                    <div key={i} className="tg-kb-row">{row.map((b, j) => <button key={j} type="button" onClick={() => press(m, b)}>{b.text}{b.url ? " ↗" : ""}</button>)}</div>
                  ))}
                </div>
              )}
            </div>
          ))}
          <div ref={end} />
        </div>
        {kb.length > 0 && (
          <div className="tg-replykb">
            {kb.map((b, i) => (
              <button key={i} type="button" onClick={async () => {
                if (!b.contact) { send(undefined, b.text); return; }
                // в настоящем Telegram номер берётся из аккаунта; здесь — спрашиваем, какой номер «у этого Telegram»
                const phone = window.prompt("Тестовый Telegram: номер этого аккаунта", "+375 29 123-45-67");
                if (!phone) return;
                try { await api.post("/telegram/sandbox/contact", { phone }); } catch (x: any) { toast(x.message, "error"); }
              }}>{b.text}</button>
            ))}
          </div>
        )}
        <div className="tg-quick">
          {["/menu", "/balance", "/plans", "/history", "/phone", "/support"].map((c) => <button key={c} type="button" onClick={() => send(undefined, c)}>{c}</button>)}
        </div>
        <form className="tg-input" onSubmit={send}>
          <input value={text} onChange={(e) => setText(e.target.value)} placeholder="Сообщение" maxLength={4000} aria-label="Сообщение боту" />
          <button type="submit" aria-label="Отправить" disabled={!text.trim()}><Send /></button>
        </form>
      </div>
      <p className="small muted tg-foot">{me?.user?.email} · <Link to="/app/profile/notifications">Настройки уведомлений</Link></p>
    </div>
  );
}
