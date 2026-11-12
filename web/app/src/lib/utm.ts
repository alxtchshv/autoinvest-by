// Откуда пришёл человек (метки рекламы utm_source/utm_campaign) и учёт посещений для админки.
// Без cookie и персональных данных: метка хранится в браузере, посещение отправляется не чаще раза в день.
import { api } from "./api";

const UTM_KEY = "ai_utm";
const VISIT_KEY = "ai_visit";

export type Utm = { utm_source: string; utm_campaign: string };

export function getUtm(): Utm {
  try {
    const v = JSON.parse(localStorage.getItem(UTM_KEY) || "{}");
    return { utm_source: v.utm_source || "", utm_campaign: v.utm_campaign || "" };
  } catch {
    return { utm_source: "", utm_campaign: "" };
  }
}

/** Вызывается на публичных страницах: запоминает метки из адреса и засчитывает посещение. */
export function trackVisit() {
  try {
    const q = new URLSearchParams(window.location.search);
    const src = q.get("utm_source"), camp = q.get("utm_campaign");
    if (src || camp) localStorage.setItem(UTM_KEY, JSON.stringify({ utm_source: src || "", utm_campaign: camp || "" }));
    const today = new Date().toISOString().slice(0, 10);
    if (localStorage.getItem(VISIT_KEY) === today) return;
    localStorage.setItem(VISIT_KEY, today);
    const u = getUtm();
    api.post("/track", { source: u.utm_source, campaign: u.utm_campaign }).catch(() => null);
  } catch {
    /* приватный режим браузера — не страшно */
  }
}

/** Метки для ссылки входа через Google. */
export function utmQuery() {
  const u = getUtm();
  const p = new URLSearchParams();
  if (u.utm_source) p.set("utm_source", u.utm_source);
  if (u.utm_campaign) p.set("utm_campaign", u.utm_campaign);
  return p.toString();
}
