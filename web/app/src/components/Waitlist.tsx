// До запуска (LAUNCH_MODE=waitlist): лист ожидания, переход на демо-счёт и плашка демо-счёта.
// Окно листа ожидания открывается само, когда сервер отвечает code=waitlist (оплата, карта, пополнение).
import { useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { Gift, PlayCircle } from "lucide-react";
import { api } from "../lib/api";
import { setWelcomePending } from "../lib/offers";
import { useSession } from "../lib/session";
import type { Me } from "../lib/types";
import { Button, Modal, useToast } from "./ui";

/** Перейти на демо-счёт и обратно — сессия меняется на сервере, интерфейс перезагружает данные. */
export function useDemoSwitch() {
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const go = async (to: "enter" | "exit") => {
    setBusy(true);
    try {
      await api.post<Me>(`/demo/${to}`);
      // полная перезагрузка: данные, живые котировки и уведомления открываются заново уже для другого счёта —
      // так цифры демо-счёта и основного аккаунта никогда не смешаются на экране
      window.location.assign("/app");
    } catch (e: any) {
      toast(e.message, "error");
      setBusy(false);
    }
  };
  return { enter: () => go("enter"), exit: () => go("exit"), busy };
}

/** Содержимое «вы в листе ожидания»: место, бонус, демо-счёт и обратная связь. */
export function WaitlistContent({ position, onDone, welcome }: { position?: number; onDone?: () => void; welcome?: boolean }) {
  const { me } = useSession();
  const demo = useDemoSwitch();
  return (
    <div className="wl stack g16">
      {welcome && <p className="wl-hello">Копилка сохранена{me?.user?.name ? `, ${me.user.name.split(" ")[0]}` : ""}! Мы готовим запуск пополнений — а пока всё можно попробовать на демо-счёте.</p>}
      <div className="wl-head">
        <span className="wl-num">{position ? `№${position}` : "✓"}</span>
        <div>
          <b>Вы в списке первых</b>
          <span>Мы подключаем пополнения с картой. Как только откроем — напишем {me?.telegram_linked ? "в Telegram и " : ""}на почту.</span>
        </div>
      </div>
      <div className="wl-gift"><Gift /><span><b>Месяц Pro бесплатно</b> всем из списка ожидания при запуске: комиссия 0,5% вместо 1% и до 20 копилок.</span></div>
      <div className="wl-actions">
        <Button variant="primary" size="lg" block icon={<PlayCircle />} busy={demo.busy} onClick={() => { onDone?.(); demo.enter(); }}>А пока — попробовать на демо-счёте</Button>
        <span className="small muted wl-note">Демо-счёт: виртуальные деньги, настоящие цены. Можно пополнять, выводить, менять план — как будет после запуска.</span>
      </div>
    </div>
  );
}

/** Окно листа ожидания: открывается по событию ai:waitlist (его шлёт api.ts на ответ code=waitlist). */
export function WaitlistModal() {
  const [pos, setPos] = useState<number | null>(null);
  const [welcome, setWelcome] = useState(false);
  useEffect(() => {
    const h = (e: Event) => {
      const d = (e as CustomEvent).detail;
      setWelcome(!!d?.first);
      if (d?.first) setWelcomePending(true); // подсказки (Telegram, Face ID) подождут, пока приветствие не закроют
      setPos(d?.position || 0);
    };
    window.addEventListener("ai:waitlist", h);
    return () => window.removeEventListener("ai:waitlist", h);
  }, []);
  const close = () => { setPos(null); setWelcomePending(false); };
  return (
    <Modal open={pos !== null} onClose={close} title={welcome ? "Добро пожаловать в AutoInvest" : "Скоро запуск"}>
      <WaitlistContent position={pos || undefined} welcome={welcome} onDone={close} />
    </Modal>
  );
}

/** Плашка демо-счёта сверху: всегда видно, что деньги виртуальные, и как вернуться. */
export function DemoBar() {
  const { me } = useSession();
  const demo = useDemoSwitch();
  if (!me?.demo) return null;
  return (
    <div className="demo-bar">
      <span><b>Демо-счёт.</b> Деньги виртуальные, цены настоящие. Нашли ошибку или есть идея — напишите в поддержку, за полезные дарим Pro.</span>
      <button type="button" onClick={demo.exit} disabled={demo.busy}>Вернуться в мой аккаунт</button>
    </div>
  );
}

/** Пункт «Демо-счёт» в меню: всегда под рукой у того, кто настроил копилку до запуска. */
export function DemoNavButton({ collapsed }: { collapsed?: boolean }) {
  const { me, config } = useSession();
  const demo = useDemoSwitch();
  if (config?.launch !== "waitlist" || me?.demo || !me?.plans || me.user?.role !== "user") return null;
  return (
    <button type="button" className="nav-demo" title={collapsed ? "Демо-счёт" : undefined} onClick={demo.enter} disabled={demo.busy}>
      <PlayCircle /><span className="nav-label">Демо-счёт</span>
    </button>
  );
}

/** Сначала анкета: новый человек без копилки из любого раздела кабинета попадает на 4 вопроса
 *  (вход через Google мог вернуть на другой адрес). «Пропустить» на анкете это снимает. */
export function OnboardingGuard() {
  const { me } = useSession();
  const nav = useNavigate();
  const { pathname } = useLocation();
  const need = !!me?.user && me.user.role === "user" && !me.demo && !me.user.onboarded_at && !me.plans;
  const free = /^\/app\/(onboarding|plans\/new|profile|support|notifications)/.test(pathname);
  useEffect(() => {
    if (need && !free) nav("/app/onboarding", { replace: true });
  }, [need, free, nav]);
  return null;
}

/** Карточка на главной для настоящего аккаунта в листе ожидания. */
export function WaitlistHomeCard() {
  const { me, config } = useSession();
  const [pos, setPos] = useState(0);
  const demo = useDemoSwitch();
  useEffect(() => {
    if (config?.launch !== "waitlist" || me?.demo) return;
    api.get<{ position: number }>("/waitlist").then((r) => setPos(r.position)).catch(() => null);
  }, [config?.launch, me?.demo]);
  // демо-счёт — после анкеты и копилки: без копилки на главной и так есть «Подберём копилку за 4 вопроса»
  if (config?.launch !== "waitlist" || me?.demo || !me?.plans) return null;
  return (
    <section className="sheet wl-home">
      <div>
        <b>{pos ? `Вы №${pos} в списке первых` : "Скоро запуск"}</b>
        <span>Пополнения с карты откроются совсем скоро. Всем из списка — месяц Pro бесплатно. А пока всё можно попробовать на демо-счёте.</span>
      </div>
      <Button variant="primary" icon={<PlayCircle />} busy={demo.busy} onClick={demo.enter}>Открыть демо-счёт</Button>
    </section>
  );
}
