// Готовые планы: кольцо состава, значок и плитка выбора — одинаковые на сайте, в анкете и при создании копилки.
import { useId, type ReactNode } from "react";
import { Check } from "lucide-react";
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
  belarus: "Токены белорусских компаний с фиксированной доходностью в рублях и долларах.",
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
