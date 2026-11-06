// Выбор даты вместо системного <input type="date">: неделя с понедельника, русские месяцы,
// быстрые варианты («через год») и переход по годам. Значение — строка YYYY-MM-DD или "".
import { useEffect, useRef, useState } from "react";
import { CalendarDays, ChevronLeft, ChevronRight, X } from "lucide-react";

const MONTHS = ["Январь", "Февраль", "Март", "Апрель", "Май", "Июнь", "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"];
const MONTHS_GEN = ["января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"];
const WD = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];

const pad = (n: number) => String(n).padStart(2, "0");
export const isoDate = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
export function parseIso(s: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s);
  if (!m) return null;
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
  return isNaN(d.getTime()) ? null : d;
}
export const longDate = (s: string) => {
  const d = parseIso(s);
  return d ? `${d.getDate()} ${MONTHS_GEN[d.getMonth()]} ${d.getFullYear()}` : "";
};
export function addMonths(d: Date, n: number) {
  const r = new Date(d.getFullYear(), d.getMonth() + n, 1);
  r.setDate(Math.min(d.getDate(), new Date(r.getFullYear(), r.getMonth() + 1, 0).getDate()));
  return r;
}

type Preset = { label: string; months: number };

export function DatePicker({ value, onChange, min, max, presets, placeholder = "Выберите дату", clearable = true, id }: {
  value: string; onChange: (v: string) => void; min?: string; max?: string; presets?: Preset[]; placeholder?: string; clearable?: boolean; id?: string;
}) {
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<"days" | "years">("days");
  const sel = parseIso(value);
  const minD = min ? parseIso(min) : null;
  const maxD = max ? parseIso(max) : null;
  const [view, setView] = useState(() => { const b = sel || minD || new Date(); return new Date(b.getFullYear(), b.getMonth(), 1); });
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const b = parseIso(value) || minD || new Date();
    setView(new Date(b.getFullYear(), b.getMonth(), 1));
    setMode("days");
    const close = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    const esc = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", esc);
    return () => { document.removeEventListener("mousedown", close); document.removeEventListener("keydown", esc); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const off = (d: Date) => (minD && d < minD) || (maxD && d > maxD);
  const pick = (d: Date) => { if (!off(d)) { onChange(isoDate(d)); setOpen(false); } };
  const first = (view.getDay() + 6) % 7; // сколько дней прошлого месяца до понедельника
  const days = Array.from({ length: 42 }, (_, i) => new Date(view.getFullYear(), view.getMonth(), i - first + 1));
  const today = isoDate(new Date());
  const y0 = (minD || new Date()).getFullYear();
  const y1 = (maxD || new Date(y0 + 30, 0, 1)).getFullYear();
  const canPrev = !minD || new Date(view.getFullYear(), view.getMonth(), 0) >= new Date(minD.getFullYear(), minD.getMonth(), 1);
  const canNext = !maxD || new Date(view.getFullYear(), view.getMonth() + 1, 1) <= maxD;

  return (
    <div className="dp" ref={ref}>
      <button type="button" id={id} className={`input dp-field ${open ? "focus" : ""}`} onClick={() => setOpen(!open)} aria-haspopup="dialog" aria-expanded={open}>
        <CalendarDays className="dp-ic" />
        <span className={value ? "" : "faint"}>{value ? longDate(value) : placeholder}</span>
        {clearable && value && <span className="dp-clear" role="button" aria-label="Очистить дату" onClick={(e) => { e.stopPropagation(); onChange(""); }}><X /></span>}
      </button>
      <div className={`dp-pop ${open ? "open" : ""}`} role="dialog" aria-label="Календарь" inert={!open}>
        {presets && presets.length > 0 && (
          <div className="dp-presets">
            {presets.map((p) => {
              const d = addMonths(new Date(), p.months);
              const iso = isoDate(d);
              return <button key={p.label} type="button" className="chip" aria-pressed={value === iso} disabled={!!off(d)} onClick={() => pick(d)}>{p.label}</button>;
            })}
          </div>
        )}
        <div className="dp-head">
          <button type="button" className="dp-nav" aria-label="Предыдущий месяц" disabled={mode === "years" || !canPrev} onClick={() => setView(new Date(view.getFullYear(), view.getMonth() - 1, 1))}><ChevronLeft /></button>
          <button type="button" className="dp-title" onClick={() => setMode(mode === "days" ? "years" : "days")}>{MONTHS[view.getMonth()]} {view.getFullYear()}</button>
          <button type="button" className="dp-nav" aria-label="Следующий месяц" disabled={mode === "years" || !canNext} onClick={() => setView(new Date(view.getFullYear(), view.getMonth() + 1, 1))}><ChevronRight /></button>
        </div>
        {mode === "days" ? (
          <div className="dp-grid">
            {WD.map((w) => <span key={w} className="dp-wd">{w}</span>)}
            {days.map((d) => {
              const iso = isoDate(d);
              const other = d.getMonth() !== view.getMonth();
              return (
                <button key={iso} type="button" className={`dp-day ${other ? "other" : ""} ${iso === value ? "on" : ""} ${iso === today ? "today" : ""}`} disabled={!!off(d)} onClick={() => pick(d)}>{d.getDate()}</button>
              );
            })}
          </div>
        ) : (
          <div className="dp-years">
            {Array.from({ length: Math.max(1, y1 - y0 + 1) }, (_, i) => y0 + i).map((y) => (
              <button key={y} type="button" className={`dp-year ${y === view.getFullYear() ? "on" : ""}`} onClick={() => { setView(new Date(y, view.getMonth(), 1)); setMode("days"); }}>{y}</button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
