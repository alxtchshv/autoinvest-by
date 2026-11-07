// Готовые планы: кольцо состава, значок и плитка выбора — одинаковые на сайте, в анкете и при создании копилки.
import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Check, CircleAlert } from "lucide-react";
import { planName } from "../lib/format";
import { useSession } from "../lib/session";
import type { Strategy } from "../lib/types";
import { AssetIcon } from "./ui";

/** Короткое описание плана простыми словами — вместо длинного описания из каталога. */
export const PITCH: Record<string, string> = {
  dollar: "Копите в долларах. Рубль может слабеть — доллар остаётся долларом.",
  reserve: "Доллар и золото — то, что люди покупают в неспокойные времена.",
  balanced: "Половина в долларе, остальное — золото и биткоин ради роста.",
  growth: "Ставка на криптовалюты: растёт сильнее, но и падает глубже.",
  crypto: "Три крупные криптовалюты для тех, кто готов ждать от трёх лет.",
  max: "Только криптовалюта, включая молодые монеты. Деньги, которые не жалко.",
};

/** Подпись риска для плана (1–5) — одна и та же везде. */
export const RISK_TEXT = ["", "Низкий риск", "Умеренный риск", "Средний риск", "Высокий риск", "Очень высокий риск"];

/** Кольцо долей. Разделители — прозрачные радиальные прорези постоянной ширины (через маску): щель в штрихе
 *  дуги расширялась бы к внешнему краю, а прорезь одинаковая от внутреннего края до внешнего и не зависит от фона. */
export function Ring({ parts, colorOf, size, r, width, gap }: { parts: [string, number][]; colorOf: (s: string) => string; size: number; r: number; width: number; gap: number }) {
  const id = useId();
  const C = 2 * Math.PI * r, cx = size / 2;
  const r0 = r - width / 2 - 1, r1 = r + width / 2 + 1;
  let acc = 0, accBp = 0;
  return (
    <svg viewBox={`0 0 ${size} ${size}`} aria-hidden>
      {parts.length > 1 && (
        <mask id={id} maskUnits="userSpaceOnUse" x="0" y="0" width={size} height={size}>
          <rect width={size} height={size} fill="#fff" />
          {parts.map(([a, w]) => {
            const ang = (accBp / 10000) * 2 * Math.PI - Math.PI / 2;
            accBp += w;
            const c = Math.cos(ang), sn = Math.sin(ang);
            return <line key={a} x1={cx + r0 * c} y1={cx + r0 * sn} x2={cx + r1 * c} y2={cx + r1 * sn} stroke="#000" strokeWidth={gap} />;
          })}
        </mask>
      )}
      <g mask={parts.length > 1 ? `url(#${id})` : undefined}>
        {parts.map(([a, w]) => {
          const len = (w / 10000) * C;
          // единственная доля — полный круг без стыка
          const el = parts.length === 1
            ? <circle key={a} cx={cx} cy={cx} r={r} fill="none" stroke={colorOf(a)} strokeWidth={width} />
            : <circle key={a} cx={cx} cy={cx} r={r} fill="none" stroke={colorOf(a)} strokeWidth={width} strokeDasharray={`${len} ${C}`} strokeDashoffset={-acc} transform={`rotate(-90 ${cx} ${cx})`} />;
          acc += len;
          return el;
        })}
      </g>
    </svg>
  );
}


export function PlanMark({ id, size = 26 }: { id: string; size?: number }) {
  const { config } = useSession();
  const s = config?.strategies.find((x) => x.id === id);
  if (!s) return <span className="plan-mark" style={{ width: size, height: size }} />;
  const parts = Object.entries(s.weights).sort((a, b) => b[1] - a[1]);
  const colorOf = (a: string) => config?.assets.find((x) => x.symbol === a)?.color || "#8a919e";
  return <span className="plan-mark" style={{ width: size, height: size }}><Ring parts={parts} colorOf={colorOf} size={26} r={9} width={6} gap={1.2} /></span>;
}

/** Плитка готового плана: кольцо, название, одна фраза и логотипы активов с долями. */
export function PlanTile({ s, selected, onSelect, extra }: { s: Strategy; selected?: boolean; onSelect?: () => void; extra?: ReactNode }) {
  const parts = Object.entries(s.weights).sort((a, b) => b[1] - a[1]);
  return (
    <button type="button" className={`plan-tile ${selected ? "on" : ""}`} aria-pressed={selected} onClick={onSelect}>
      <PlanMark id={s.id} size={44} />
      <span className="pt-body">
        <span className="pt-name">{planName(s.name)}</span>
        <span className="pt-pitch">{PITCH[s.id] || s.description}</span>
        <span className="pt-mix">
          {parts.map(([a, w]) => <span key={a}><AssetIcon symbol={a} size="sm" />{w / 100}%</span>)}
          <span className={`pt-risk r${s.risk}`}>{RISK_TEXT[s.risk]}</span>
        </span>
        {extra}
      </span>
      <span className="pt-check" aria-hidden>{selected && <Check />}</span>
    </button>
  );
}

/** Подробности плана во всплывающей карточке: открывается при наведении на значок (на телефоне — по нажатию).
 *  Рисуется в body с position: fixed, чтобы её не обрезало модальное окно с прокруткой. */
export function PlanHint({ s }: { s: Strategy }) {
  const { config } = useSession();
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{ left: number; top: number; above: boolean } | null>(null);
  const btn = useRef<HTMLSpanElement>(null);
  const pop = useRef<HTMLDivElement>(null);
  const hideT = useRef<number | undefined>(undefined);
  const show = () => { window.clearTimeout(hideT.current); setOpen(true); };
  const hide = () => { hideT.current = window.setTimeout(() => setOpen(false), 120); };
  useLayoutEffect(() => {
    if (!open || !btn.current) return;
    const place = () => {
      const r = btn.current!.getBoundingClientRect();
      const w = 300, h = pop.current?.offsetHeight || 220;
      const above = r.bottom + 10 + h > window.innerHeight && r.top - 10 - h > 0;
      const left = Math.max(12, Math.min(window.innerWidth - w - 12, r.right - w + 14));
      setPos({ left, top: above ? r.top - 10 - h : r.bottom + 10, above });
    };
    place();
    window.addEventListener("scroll", place, true);
    window.addEventListener("resize", place);
    return () => { window.removeEventListener("scroll", place, true); window.removeEventListener("resize", place); };
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const away = (e: Event) => !btn.current?.contains(e.target as Node) && !pop.current?.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("pointerdown", away);
    document.addEventListener("keydown", esc);
    return () => { document.removeEventListener("pointerdown", away); document.removeEventListener("keydown", esc); };
  }, [open]);
  const parts = Object.entries(s.weights).sort((a, b) => b[1] - a[1]);
  const nameOf = (a: string) => config?.assets.find((x) => x.symbol === a)?.name || a;
  return (
    <>
      <span ref={btn} className={`pt-hint ${open ? "on" : ""}`} role="button" tabIndex={0} aria-label={`Подробнее о плане «${s.name}»`} aria-expanded={open}
        onMouseEnter={show} onMouseLeave={hide} onFocus={show} onBlur={hide}
        onClick={(e) => { e.stopPropagation(); setOpen((o) => !o); }}
        onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); e.stopPropagation(); setOpen((o) => !o); } }}>
        <CircleAlert />
      </span>
      {open && createPortal(
        <div ref={pop} className={`plan-pop ${pos ? "in" : ""} ${pos?.above ? "above" : ""}`} role="tooltip" style={{ left: pos?.left ?? -9999, top: pos?.top ?? 0 }} onMouseEnter={show} onMouseLeave={hide}>
          <div className="pp-head"><PlanMark id={s.id} size={36} /><div><b>{planName(s.name)}</b><span className={`pt-risk r${s.risk}`}>{RISK_TEXT[s.risk]}</span></div></div>
          <p>{s.description}</p>
          <ul className="pp-mix">
            {parts.map(([a, w]) => <li key={a}><AssetIcon symbol={a} size="sm" /><span>{nameOf(a)}</span><b>{w / 100}%</b></li>)}
          </ul>
        </div>,
        document.body,
      )}
    </>
  );
}

/** Компактная плитка плана: только название и риск, подробности — в подсказке. Для смены плана в копилке. */
export function PlanChip({ s, selected, onSelect }: { s: Strategy; selected?: boolean; onSelect?: () => void }) {
  return (
    <div className={`plan-chip ${selected ? "on" : ""}`}>
      <button type="button" className="pc-main" aria-pressed={selected} onClick={onSelect}>
        <span className="pc-radio" aria-hidden>{selected && <Check />}</span>
        <span className="pc-name">{s.name}</span>
        <span className={`pt-risk r${s.risk}`}>{RISK_TEXT[s.risk]}</span>
      </button>
      <PlanHint s={s} />
    </div>
  );
}
