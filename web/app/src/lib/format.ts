// Форматирование чисел, денег и дат. Время всегда показываем по Минску.

const TZ = "Europe/Minsk";
const nbsp = " ";
/** Знак белорусского рубля (НБРБ, 27.01.2026; U+20C5). Пока его нет в системных шрифтах,
 *  поэтому сайт подключает свой шрифт byn-sign-*.woff2 через unicode-range — см. styles/base.css. */
export const RUB = "⃅";

export function groupInt(n: number): string {
  const s = Math.trunc(Math.abs(n)).toString();
  return s.replace(/\B(?=(\d{3})+(?!\d))/g, nbsp);
}

/** 123456 → "1 234,56" */
export function kop(k: number, opts: { sign?: boolean } = {}): string {
  const neg = k < 0;
  const a = Math.abs(Math.round(k));
  const s = groupInt(Math.floor(a / 100)) + "," + String(a % 100).padStart(2, "0");
  if (neg) return "−" + s;
  if (opts.sign && k > 0) return "+" + s;
  return s;
}

export function byn(k: number, opts: { sign?: boolean } = {}): string {
  return kop(k, opts) + nbsp + RUB;
}

/** Цена с разумной точностью: 284 491 / 3 412,50 / 2,9612 */
export function price(v: number, cur = RUB): string {
  if (!isFinite(v)) return "—";
  let digits = 2;
  if (v >= 10000) digits = 0;
  else if (v < 10) digits = 4;
  const fixed = Math.abs(v).toFixed(digits);
  const [i, f] = fixed.split(".");
  const out = groupInt(Number(i)) + (f ? "," + f : "");
  return (v < 0 ? "−" : "") + out + (cur ? nbsp + cur : "");
}

export function pct(v: number, opts: { sign?: boolean; digits?: number } = {}): string {
  const d = opts.digits ?? 2;
  const s = Math.abs(v).toFixed(d).replace(".", ",");
  if (v < 0 && Number(s.replace(",", ".")) !== 0) return "−" + s + "%";
  if (opts.sign !== false && v > 0 && Number(s.replace(",", ".")) !== 0) return "+" + s + "%";
  return s + "%";
}

export function qty(q: number, symbol?: string): string {
  const v = q / 1e8;
  let d = 8;
  if (v >= 1000) d = 2;
  else if (v >= 1) d = 4;
  else if (v >= 0.01) d = 6;
  let s = v.toFixed(d).replace(/0+$/, "").replace(/\.$/, "");
  const [i, f] = s.split(".");
  s = groupInt(Number(i)) + (f ? "," + f : "");
  return symbol ? s + nbsp + symbol : s;
}

const dtf = (o: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat("ru-RU", { timeZone: TZ, ...o });
const fDate = dtf({ day: "numeric", month: "long", year: "numeric" });
const fDateShort = dtf({ day: "numeric", month: "short" });
const fDateNum = dtf({ day: "2-digit", month: "2-digit", year: "numeric" });
const fTime = dtf({ hour: "2-digit", minute: "2-digit" });
const fWeekday = dtf({ weekday: "long" });

export const date = (ts: number) => fDate.format(ts * 1000);
export const dateNum = (ts: number) => fDateNum.format(ts * 1000);
export const dateShort = (ts: number) => fDateShort.format(ts * 1000).replace(".", "");
export const time = (ts: number) => fTime.format(ts * 1000);
export const weekday = (ts: number) => fWeekday.format(ts * 1000);
export const dateTime = (ts: number) => `${fDateNum.format(ts * 1000)}, ${fTime.format(ts * 1000)}`;

function dayKey(ts: number) {
  return fDateNum.format(ts * 1000);
}

/** «Сегодня, 10:00», «Завтра, 10:00», «В пятницу, 10:00», «12 декабря, 10:00» */
export function when(ts: number): string {
  const now = Date.now() / 1000;
  const t = time(ts);
  if (dayKey(ts) === dayKey(now)) return "Сегодня, " + t;
  if (dayKey(ts) === dayKey(now + 86400)) return "Завтра, " + t;
  if (dayKey(ts) === dayKey(now - 86400)) return "Вчера, " + t;
  if (ts > now && ts - now < 6 * 86400) {
    const wd = weekday(ts);
    const pre = wd === "вторник" ? "Во " : "В ";
    const acc = wd.replace(/а$/, "у");
    return pre + acc + ", " + t;
  }
  return dateShort(ts) + ", " + t;
}

export function ago(ts: number): string {
  const d = Date.now() / 1000 - ts;
  if (d < 60) return "только что";
  if (d < 3600) return Math.floor(d / 60) + " мин назад";
  if (d < 86400) return Math.floor(d / 3600) + " ч назад";
  return when(ts);
}

export function plural(n: number, one: string, few: string, many: string): string {
  const m10 = n % 10, m100 = n % 100;
  if (m10 === 1 && m100 !== 11) return one;
  if (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14)) return few;
  return many;
}

export const WD_SHORT = ["", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];
const WD_PL = ["", "по понедельникам", "по вторникам", "по средам", "по четвергам", "по пятницам", "по субботам", "по воскресеньям"];
const WD_ACC = ["", "в понедельник", "во вторник", "в среду", "в четверг", "в пятницу", "в субботу", "в воскресенье"];

export function minuteStr(m: number) {
  return String(Math.floor(m / 60)).padStart(2, "0") + ":" + String(m % 60).padStart(2, "0");
}

export function scheduleText(p: { frequency: string; weekday: number; monthday: number; run_minute: number }): string {
  const t = minuteStr(p.run_minute);
  switch (p.frequency) {
    case "daily": return `каждый день в ${t}`;
    case "weekly": return `${WD_PL[p.weekday]} в ${t}`;
    case "biweekly": return `раз в две недели ${WD_ACC[p.weekday]} в ${t}`;
    case "monthly": return `${p.monthday}-го числа в ${t}`;
    default: return "только вручную";
  }
}

export const freqLabel: Record<string, string> = { daily: "Каждый день", weekly: "Каждую неделю", biweekly: "Раз в 2 недели", monthly: "Раз в месяц", manual: "Вручную" };

export const brandLabel = (b: string) => ({ visa: "Visa", mastercard: "Mastercard", belkart: "Белкарт", mir: "МИР", master: "Mastercard" } as Record<string, string>)[b.toLowerCase()] || b || "Карта";

export function initials(name: string) {
  const p = name.trim().split(/\s+/);
  return ((p[0]?.[0] || "") + (p[1]?.[0] || "")).toUpperCase() || "•";
}

/** Парсит "15", "15,5", "1 500.50" в копейки; NaN если пусто/ошибка */
export function parseKop(s: string): number {
  const c = s.replace(/[\s ]/g, "").replace(",", ".");
  // не больше 9 цифр в рублях: отсекаем «бесконечные» суммы ещё до сервера
  if (!c || !/^\d{1,9}(\.\d{0,2})?$/.test(c)) return NaN;
  return Math.round(parseFloat(c) * 100);
}

/** Готовые наборы активов везде называем «План «Баланс»», чтобы «Доллар» как план не путали с валютой. */
export const planName = (name: string) => `План «${name}»`;
