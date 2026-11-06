import { cloneElement, createContext, isValidElement, useCallback, useContext, useEffect, useId, useRef, useState, type ButtonHTMLAttributes, type InputHTMLAttributes, type ReactNode } from "react";
import { AlertCircle, CheckCircle2, Info, MoreHorizontal, X, TriangleAlert, Car, GraduationCap, Gift, Heart, Home, Laptop, PiggyBank, Plane, Rocket, Shield } from "lucide-react";
import { kop, pct, RUB } from "../lib/format";
import { useSession } from "../lib/session";

export function Logo({ size = 30 }: { size?: number }) {
  return (
    <svg className="brand-mark" width={size} height={size} viewBox="0 0 32 32" aria-hidden>
      <rect width="32" height="32" rx="8" fill="#2340D8" />
      <path d="M8 22.5 13.2 14l4 5.2L24 9.5" fill="none" stroke="#fff" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="24" cy="9.5" r="2.2" fill="#fff" />
    </svg>
  );
}

type BtnProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  size?: "sm" | "md" | "lg";
  block?: boolean;
  busy?: boolean;
  icon?: ReactNode;
};
export function Button({ variant = "secondary", size = "md", block, busy, icon, className = "", children, ...rest }: BtnProps) {
  const cls = ["btn", "btn-" + variant, size !== "md" ? "btn-" + size : "", block ? "btn-block" : "", !children ? "btn-icon" : "", className].join(" ");
  return (
    <button type="button" className={cls} aria-busy={busy || undefined} disabled={rest.disabled || busy} {...rest}>
      {busy ? <span className="spinner" /> : icon}
      {children}
    </button>
  );
}

export function Field({ label, hint, error, children, htmlFor }: { label?: ReactNode; hint?: ReactNode; error?: string | null; children: ReactNode; htmlFor?: string }) {
  const auto = useId();
  let id = htmlFor;
  let body = children;
  // связываем подпись с полем, если id не задан явно
  if (!id && isValidElement<{ id?: string }>(children) && typeof children.type === "string" && ["input", "select", "textarea"].includes(children.type)) {
    id = children.props.id || auto;
    body = cloneElement(children, { id });
  } else if (!id && isValidElement<{ id?: string }>(children) && (children.type === Input)) {
    id = children.props.id || auto;
    body = cloneElement(children, { id });
  }
  return (
    <div className="field">
      {label && <label htmlFor={id}>{label}</label>}
      {body}
      {error ? <div className="error" role="alert">{error}</div> : hint ? <div className="hint">{hint}</div> : null}
    </div>
  );
}

export function Input({ invalid, ...p }: InputHTMLAttributes<HTMLInputElement> & { invalid?: boolean }) {
  return <input className="input" aria-invalid={invalid || undefined} {...p} />;
}

export function Switch({ checked, onChange, label, disabled }: { checked: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean }) {
  return (
    <span className="switch">
      <input type="checkbox" role="switch" aria-label={label} checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span />
    </span>
  );
}

export function Segmented<T extends string>({ value, onChange, options, label }: { value: T; onChange: (v: T) => void; options: { value: T; label: ReactNode }[]; label: string }) {
  return (
    <div className="seg" role="group" aria-label={label}>
      {options.map((o) => (
        <button key={o.value} type="button" aria-pressed={o.value === value} onClick={() => onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </div>
  );
}

/** Сумма с копейками приглушённо: 1 234,56 ⃅ */
export function Money({ k, className = "", sign, cur = true, size }: { k: number; className?: string; sign?: boolean; cur?: boolean; size?: "xl" | "lg" }) {
  const s = kop(k, { sign });
  const [i, f] = s.split(",");
  const { hidden } = useHide();
  if (hidden) return <span className={`money ${size ? "money-" + size : ""} ${className}`}>•••••{cur && (size === "xl" ? <span className="cur">{RUB}</span> : " " + RUB)}</span>;
  return (
    <span className={`money ${size ? "money-" + size : ""} ${className}`}>
      {i}
      <span className="kop">,{f}</span>
      {cur && (size === "xl" ? <span className="cur">{RUB}</span> : " " + RUB)}
    </span>
  );
}

export function Change({ v, k, className = "" }: { v?: number; k?: number; className?: string }) {
  const val = v ?? k ?? 0;
  const cls = val > 0.00001 ? "up" : val < -0.00001 ? "down" : "muted";
  const { hidden } = useHide();
  return (
    <span className={`num ${cls} ${className}`}>
      {k !== undefined ? (hidden ? "•••" : kop(k, { sign: true }) + " " + RUB) : pct(v!, { sign: true })}
    </span>
  );
}

const ICON_LABELS: Record<string, string> = { PAXG: "Au", USDT: "$", SPY: "500", BYCORP_USD: "$%", BYCORP_BYN: RUB + "%" };

/** Настоящие логотипы монет (public/coins, см. README.txt там же); у остальных активов — буквенный значок. */
const COIN_LOGOS: Record<string, string> = { BTC: "btc", ETH: "eth", SOL: "sol", USDT: "usdt", PAXG: "paxg", TON: "ton" };

export function AssetIcon({ symbol, color, size }: { symbol: string; color?: string; size?: "sm" | "lg" }) {
  const { config } = useSession();
  const logo = COIN_LOGOS[symbol];
  if (logo) return <img className={`asset-ico asset-logo ${size || ""}`} src={`/coins/${logo}.svg`} alt="" aria-hidden draggable={false} />;
  const a = config?.assets.find((x) => x.symbol === symbol);
  const c = color || a?.color || "#8a919e";
  const label = ICON_LABELS[symbol] || symbol.slice(0, 3);
  return (
    <span className={`asset-ico ${size || ""}`} style={{ background: c }} aria-hidden>
      {label}
    </span>
  );
}

export const PLAN_ICONS: Record<string, typeof PiggyBank> = { piggy: PiggyBank, shield: Shield, plane: Plane, home: Home, laptop: Laptop, car: Car, gift: Gift, graduation: GraduationCap, heart: Heart, rocket: Rocket };
export function PlanIcon({ icon }: { icon: string }) {
  const I = PLAN_ICONS[icon] || PiggyBank;
  return (
    <span className="plan-ico">
      <I />
    </span>
  );
}

export function RiskDots({ risk }: { risk: number }) {
  return (
    <span className={`risk-dots r${risk}`} title={`Риск ${risk} из 5`} aria-label={`Риск ${risk} из 5`}>
      {[1, 2, 3, 4, 5].map((i) => (
        <i key={i} className={i <= risk ? "on" : ""} />
      ))}
    </span>
  );
}
export const riskLabel = (r: number) => ["", "Низкий", "Умеренный", "Средний", "Высокий", "Очень высокий"][r] || "";

export function Notice({ kind = "info", children, title }: { kind?: "info" | "warn" | "error" | "ok" | "plain"; children: ReactNode; title?: ReactNode }) {
  const I = kind === "warn" ? TriangleAlert : kind === "error" ? AlertCircle : kind === "ok" ? CheckCircle2 : Info;
  return (
    <div className={`notice ${kind === "plain" ? "" : kind}`} role={kind === "error" ? "alert" : undefined}>
      <I />
      <div>
        {title && <div style={{ fontWeight: 600, marginBottom: 2 }}>{title}</div>}
        {children}
      </div>
    </div>
  );
}

export function Empty({ icon, title, children, action }: { icon?: ReactNode; title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="empty">
      {icon}
      <h3>{title}</h3>
      {children && <p style={{ maxWidth: 380 }}>{children}</p>}
      {action && <div className="mt8">{action}</div>}
    </div>
  );
}

export function Skeleton({ h = 16, w = "100%", r }: { h?: number; w?: number | string; r?: number }) {
  return <div className="skeleton" style={{ height: h, width: w, borderRadius: r }} />;
}

export function PageLoader() {
  return (
    <div className="stack g16" aria-busy="true">
      <Skeleton h={34} w={240} />
      <Skeleton h={160} r={16} />
      <Skeleton h={260} r={16} />
    </div>
  );
}

export function LoadError({ error, retry }: { error: { message: string }; retry?: () => void }) {
  return (
    <div className="sheet">
      <Empty icon={<AlertCircle className="ico" />} title="Не удалось загрузить" action={retry && <Button onClick={retry}>Повторить</Button>}>
        {error.message}
      </Empty>
    </div>
  );
}

const ORDER_STATUS: Record<string, [string, string]> = {
  filled: ["Исполнено", "badge-up"],
  pending: ["В обработке", "badge-warn"],
  failed: ["Отклонено", "badge-down"],
  payout_pending: ["Ожидает выплаты", "badge-warn"],
  manual: ["На проверке", "badge-warn"],
  refunded: ["Возвращено", ""],
};
export function OrderStatus({ s }: { s: string }) {
  const [t, c] = ORDER_STATUS[s] || [s, ""];
  return <span className={`badge ${c}`}>{t}</span>;
}

export function PlanStatus({ s }: { s: string }) {
  if (s === "active") return <span className="badge badge-up"><span className="dot" style={{ background: "var(--up)" }} />Работает</span>;
  if (s === "paused") return <span className="badge">На паузе</span>;
  return <span className="badge">Закрыта</span>;
}

// ---------------------------------------------------------------- модальное окно

export function Modal({ open, onClose, title, children, wide }: { open: boolean; onClose: () => void; title: ReactNode; children: ReactNode; wide?: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const prev = document.activeElement as HTMLElement | null;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    document.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    setTimeout(() => ref.current?.querySelector<HTMLElement>("input, select, textarea, button:not(.modal-x)")?.focus(), 30);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
      prev?.focus?.();
    };
  }, [open, onClose]);
  if (!open) return null;
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={`modal ${wide ? "wide" : ""}`} role="dialog" aria-modal="true" aria-label={typeof title === "string" ? title : undefined} ref={ref}>
        <div className="modal-head">
          <h2>{title}</h2>
          <Button variant="ghost" size="sm" className="modal-x" aria-label="Закрыть" onClick={onClose} icon={<X />} />
        </div>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------- тосты

type Toast = { id: number; text: string; kind: "ok" | "error" };
const TCtx = createContext<(text: string, kind?: "ok" | "error") => void>(() => {});
export function ToastProvider({ children }: { children: ReactNode }) {
  const [list, setList] = useState<Toast[]>([]);
  const push = useCallback((text: string, kind: "ok" | "error" = "ok") => {
    const id = Date.now() + Math.random();
    setList((l) => [...l.slice(-2), { id, text, kind }]);
    setTimeout(() => setList((l) => l.filter((t) => t.id !== id)), kind === "error" ? 6000 : 3500);
  }, []);
  return (
    <TCtx.Provider value={push}>
      {children}
      <div className="toasts" aria-live="polite">
        {list.map((t) => (
          <div key={t.id} className={`toast ${t.kind === "error" ? "error" : ""}`}>
            {t.kind === "error" ? <AlertCircle size={18} /> : <CheckCircle2 size={18} />}
            <span>{t.text}</span>
          </div>
        ))}
      </div>
    </TCtx.Provider>
  );
}
export const useToast = () => useContext(TCtx);

// ---------------------------------------------------------------- скрытие сумм («глазок»)

const HCtx = createContext<{ hidden: boolean; toggle: () => void }>({ hidden: false, toggle: () => {} });
export function HideProvider({ children }: { children: ReactNode }) {
  const [hidden, setHidden] = useState(() => {
    try {
      return localStorage.getItem("ai_hide") === "1";
    } catch {
      return false;
    }
  });
  const toggle = useCallback(() => {
    setHidden((h) => {
      try {
        localStorage.setItem("ai_hide", h ? "0" : "1");
      } catch {
        /* приватный режим */
      }
      return !h;
    });
  }, []);
  return <HCtx.Provider value={{ hidden, toggle }}>{children}</HCtx.Provider>;
}
export const useHide = () => useContext(HCtx);

export { pct };

/** Меню «⋯» для редких действий, чтобы не перегружать шапку страницы кнопками. */
export function MoreMenu({ items, label = "Ещё действия" }: { items: { label: string; icon?: ReactNode; onClick: () => void; danger?: boolean }[]; label?: string }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const h = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    const k = (e: KeyboardEvent) => e.key === "Escape" && setOpen(false);
    document.addEventListener("mousedown", h);
    document.addEventListener("keydown", k);
    return () => { document.removeEventListener("mousedown", h); document.removeEventListener("keydown", k); };
  }, [open]);
  return (
    <div className="more" ref={ref}>
      <button type="button" className="btn btn-secondary more-btn" aria-label={label} title={label} aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}><MoreHorizontal /></button>
      <div className={`more-pop ${open ? "open" : ""}`} role="menu" inert={!open}>
        {items.map((it, i) => (
          <button key={i} type="button" role="menuitem" className={it.danger ? "danger" : ""} onClick={() => { setOpen(false); it.onClick(); }}>{it.icon}{it.label}</button>
        ))}
      </div>
    </div>
  );
}

/** Список вопросов с плавным раскрытием — один и тот же на сайте и в поддержке. */
export function Accordion({ items, first = 0 }: { items: [string, ReactNode][]; first?: number | null }) {
  const [open, setOpen] = useState<number | null>(first);
  return (
    <div className="faq">
      {items.map(([q, a], k) => (
        <div key={q} className={`faq-item ${open === k ? "open" : ""}`}>
          <button type="button" className="faq-q" aria-expanded={open === k} onClick={() => setOpen(open === k ? null : k)}>
            {q}<span className="faq-ic" aria-hidden />
          </button>
          <div className="faq-a"><div><div className="faq-p">{a}</div></div></div>
        </div>
      ))}
    </div>
  );
}
