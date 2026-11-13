// Подсказки в углу экрана (Telegram, Face ID) показываются по одной и не мешают приветствию:
//   1. приветствие после анкеты («Добро пожаловать») — пока оно открыто, подсказок нет;
//   2. потом — «Подключите Telegram» (и на демо-счёте тоже: привяжется основной аккаунт);
//   3. когда Telegram подключён или «Не сейчас» — предложение Face ID / отпечатка.
// Всплывающие уведомления поднимаются над карточкой и не наезжают на неё.
import { useEffect, useState } from "react";
import type { Config, Me } from "./types";

const EVT = "ai:offers";
const WELCOME = "ai_welcome_pending";
const TG_KEY = "ai_tg_offer";
const TG_PAUSE = 14 * 86400_000;

const get = (k: string) => { try { return localStorage.getItem(k); } catch { return null; } };
const set = (k: string, v: string | null) => { try { v === null ? localStorage.removeItem(k) : localStorage.setItem(k, v); } catch { /* */ } };
const bump = () => window.dispatchEvent(new Event(EVT));

/** Перерисовать компонент, когда очередь подсказок изменилась. */
export function useOffersTick() {
  const [, setN] = useState(0);
  useEffect(() => {
    const h = () => setN((n) => n + 1);
    window.addEventListener(EVT, h);
    return () => window.removeEventListener(EVT, h);
  }, []);
}

/** Основной аккаунт: на демо-счёте подсказки относятся к нему. */
export const realUid = (me: Me | null | undefined) => me?.user?.demo_of || me?.user?.id || 0;

/** Приветствие открыто и ещё не закрыто — подсказки ждут (переживает переход на демо-счёт с перезагрузкой). */
// с запасом по времени: если вкладку закрыли с открытым приветствием, подсказки не застрянут навсегда
export const welcomePending = () => Date.now() - Number(get(WELCOME) || 0) < 10 * 60_000;
export const setWelcomePending = (on: boolean) => { set(WELCOME, on ? String(Date.now()) : null); bump(); };

/** Нужно ли сейчас предлагать Telegram. */
export function tgWanted(me: Me | null | undefined, config: Config | null | undefined) {
  const uid = realUid(me);
  if (!uid || !me?.user || me.telegram_linked || !config?.telegram_bot) return false;
  if (me.user.role !== "user" && !me.demo) return false;
  if (!me.plans) return false; // сначала анкета и копилка
  return Date.now() - Number(get(`${TG_KEY}:${uid}`) || 0) > TG_PAUSE;
}
export const dismissTg = (me: Me | null | undefined) => { set(`${TG_KEY}:${realUid(me)}`, String(Date.now())); bump(); };

/** Высота карточки подсказки — всплывающие уведомления встают над ней. */
export const setOfferHeight = (px: number) => {
  try { document.documentElement.style.setProperty("--offer-h", px ? `${px + 12}px` : "0px"); } catch { /* */ }
};
