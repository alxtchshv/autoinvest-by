// «Подключите Telegram» — карточка в углу после приветствия (или на демо-счёте). Не всплывашка:
// не пропадает сама и не исчезает при переходе на демо-счёт. «Не сейчас» — спрячется на 2 недели.
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Send, X } from "lucide-react";
import { api } from "../lib/api";
import { dismissTg, setOfferHeight, tgWanted, useOffersTick, welcomePending } from "../lib/offers";
import { useSession } from "../lib/session";
import { Button, useToast } from "./ui";

export function TelegramOffer() {
  useOffersTick();
  const { me, config, reload } = useSession();
  const toast = useToast();
  const nav = useNavigate();
  const ref = useRef<HTMLDivElement>(null);
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const want = tgWanted(me, config) && !welcomePending();

  useEffect(() => {
    if (!want) { setShow(false); return; }
    const t = window.setTimeout(() => setShow(true), 1200); // после закрытия приветствия — короткая пауза
    return () => clearTimeout(t);
  }, [want]);

  // вернулся из Telegram после «Старт» — проверяем, привязался ли аккаунт
  useEffect(() => {
    if (!show) return;
    const f = () => reload();
    window.addEventListener("focus", f);
    return () => window.removeEventListener("focus", f);
  }, [show, reload]);

  useLayoutEffect(() => {
    setOfferHeight(show && ref.current ? ref.current.offsetHeight : 0);
    return () => setOfferHeight(0);
  }, [show]);

  if (!show) return null;
  const connect = async () => {
    setBusy(true);
    try {
      const r = await api.post<{ url: string; test?: string }>("/telegram/link");
      if (r.test) { nav(r.url); return; } // тестовый Telegram открывается прямо в кабинете
      window.open(r.url, "_blank", "noopener");
      toast("Откройте Telegram и нажмите «Старт»");
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="bio-offer" ref={ref} role="dialog" aria-label="Подключить Telegram">
      <button type="button" className="bio-x" aria-label="Закрыть" onClick={() => dismissTg(me)}><X /></button>
      <span className="bio-ic tg"><Send /></span>
      <div className="stack g4">
        <b>Узнайте о запуске первыми</b>
        <span>Подключите Telegram — пришлём новость о запуске, а потом чеки о пополнениях и коды входа. Без СМС и без спама.</span>
      </div>
      <div className="row g8">
        <Button variant="primary" size="sm" icon={<Send />} busy={busy} onClick={connect}>Подключить Telegram</Button>
        <Button size="sm" variant="ghost" onClick={() => dismissTg(me)}>Не сейчас</Button>
      </div>
    </div>
  );
}
