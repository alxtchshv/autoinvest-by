import { Suspense, useEffect, useRef, useState, type ReactNode } from "react";
import { Link, NavLink, Navigate, Outlet, useLocation, useNavigate } from "react-router-dom";
import { Bell, ChartCandlestick, CreditCard, Eye, EyeOff, History, House, LifeBuoy, PiggyBank, Calculator, Shield, LogOut, User } from "lucide-react";
import { api } from "../lib/api";
import { ago, initials } from "../lib/format";
import { useLoad } from "../lib/hooks";
import { MarketProvider, useSession, useUserEvent } from "../lib/session";
import type { Notification } from "../lib/types";
import { Button, Logo, PageLoader, useHide, useToast } from "./ui";

export function ModeBanner() {
  const { config } = useSession();
  if (!config) return null;
  if (config.mode === "live" && !config.market_demo) return null;
  return (
    <div className="mode-banner">
      {config.market_demo ? (
        <><b>Демо-котировки.</b> Сервер запущен без доступа к биржам (MARKET=fixture) — цены ненастоящие.</>
      ) : config.mode === "sandbox" ? (
        <><b>Тестовый режим.</b> Деньги не списываются, карты — тестовые. Покупки считаются по реальным ценам бирж.</>
      ) : (
        <><b>Тестовые платежи bePaid.</b> Используйте тестовые карты, деньги не списываются.</>
      )}
    </div>
  );
}

export function RequireAuth({ children, staff }: { children: ReactNode; staff?: boolean }) {
  const { me, loading } = useSession();
  const loc = useLocation();
  if (loading) return null;
  if (!me?.user) return <Navigate to={`/login?next=${encodeURIComponent(loc.pathname + loc.search)}`} replace />;
  if (staff && me.user.role === "user") return <Navigate to="/app" replace />;
  return <>{children}</>;
}

const NAV = [
  { to: "/app", label: "Главная", icon: House, end: true },
  { to: "/app/plans", label: "Копилки", icon: PiggyBank },
  { to: "/app/market", label: "Рынок", icon: ChartCandlestick },
  { to: "/app/calculator", label: "Калькулятор", icon: Calculator },
  { to: "/app/history", label: "Операции", icon: History },
  { to: "/app/cards", label: "Карты", icon: CreditCard },
];

export function AppShell() {
  const { me, config } = useSession();
  const banner = config && (config.mode !== "live" || config.market_demo);
  return (
    <MarketProvider userId={me?.user?.id ?? null}>
      <ModeBanner />
      <div className={`shell ${banner ? "has-banner" : ""}`}>
        <aside className="side">
          <Link to="/app" className="brand"><Logo />AutoInvest</Link>
          <nav className="nav" aria-label="Разделы">
            {NAV.map((n) => (
              <NavLink key={n.to} to={n.to} end={n.end}>
                <n.icon />{n.label}
              </NavLink>
            ))}
            <div className="nav-sep" />
            <NavLink to="/app/support"><LifeBuoy />Поддержка</NavLink>
            {me?.user && me.user.role !== "user" && <NavLink to="/admin"><Shield />Админка</NavLink>}
          </nav>
          <div className="side-foot">
            <NavLink to="/app/profile" className="side-user">
              <span className="avatar">{initials(me?.user?.name || "")}</span>
              <span className="grow" style={{ minWidth: 0 }}>
                <div style={{ fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{me?.user?.name}</div>
                <div className="tiny muted">{me?.is_pro ? "Тариф Pro" : "Тариф Старт"}</div>
              </span>
            </NavLink>
          </div>
        </aside>
        <div className="main">
          <TopBar />
          <main className="content" id="main">
            <Suspense fallback={<PageLoader />}><Outlet /></Suspense>
          </main>
        </div>
        <nav className="tabbar" aria-label="Разделы">
          <NavLink to="/app" end><House />Главная</NavLink>
          <NavLink to="/app/plans"><PiggyBank />Копилки</NavLink>
          <NavLink to="/app/market"><ChartCandlestick />Рынок</NavLink>
          <NavLink to="/app/history"><History />Операции</NavLink>
          <NavLink to="/app/profile"><User />Профиль</NavLink>
        </nav>
      </div>
    </MarketProvider>
  );
}

function TopBar() {
  const { hidden, toggle } = useHide();
  return (
    <header className="topbar">
      <Link to="/app" className="brand mobile-brand" style={{ padding: 0 }}><Logo size={28} />AutoInvest</Link>
      <span className="spacer" />
      <Button variant="ghost" size="sm" onClick={toggle} aria-label={hidden ? "Показать суммы" : "Скрыть суммы"} title={hidden ? "Показать суммы" : "Скрыть суммы"} icon={hidden ? <EyeOff /> : <Eye />} />
      <NotificationsBell />
    </header>
  );
}

function NotificationsBell() {
  const { me, reload: reloadMe } = useSession();
  const [open, setOpen] = useState(false);
  const toast = useToast();
  const nav = useNavigate();
  const ref = useRef<HTMLDivElement>(null);
  const { data, reload } = useLoad(() => api.get<{ notifications: Notification[]; unread: number }>("/notifications?limit=15"), []);
  useUserEvent("notification", (n: Notification) => {
    reload();
    reloadMe();
    if (n?.title) toast(n.title);
  });
  useEffect(() => {
    if (!open) return;
    const h = (e: MouseEvent) => !ref.current?.contains(e.target as Node) && setOpen(false);
    document.addEventListener("mousedown", h);
    return () => document.removeEventListener("mousedown", h);
  }, [open]);
  const unread = data?.unread ?? me?.unread ?? 0;
  const readAll = async () => {
    await api.post("/notifications/read", {});
    reload();
    reloadMe();
  };
  return (
    <div className="bell" ref={ref} style={{ position: "relative" }}>
      <Button variant="ghost" size="sm" aria-label={`Уведомления${unread ? `, непрочитанных: ${unread}` : ""}`} aria-expanded={open} onClick={() => setOpen((o) => !o)} icon={<Bell />} />
      {unread > 0 && <span className="pip" />}
      {open && (
        <div className="popover">
          <div className="row between" style={{ padding: "14px 16px", borderBottom: "1px solid var(--line-2)" }}>
            <b>Уведомления</b>
            {unread > 0 && <button className="link-btn small" onClick={readAll}>Прочитать все</button>}
          </div>
          <div className="list">
            {!data?.notifications.length && <div className="empty small">Здесь будут чеки о пополнениях и ответы поддержки</div>}
            {data?.notifications.map((n) => (
              <button key={n.id} className="list-row" style={{ alignItems: "flex-start", padding: "12px 16px" }} onClick={() => { setOpen(false); api.post("/notifications/read", { id: n.id }).then(() => { reload(); reloadMe(); }); if (n.link) nav(n.link); }}>
                {!n.read ? <span className="dot" style={{ background: "var(--brand)", marginTop: 7 }} /> : <span style={{ width: 8 }} />}
                <span className="grow">
                  <div style={{ fontWeight: n.read ? 500 : 600, fontSize: 14 }}>{n.title}</div>
                  {n.body && <div className="sub">{n.body}</div>}
                  <div className="tiny faint mt8" style={{ marginTop: 2 }}>{ago(n.created_at)}</div>
                </span>
              </button>
            ))}
          </div>
          <Link to="/app/notifications" onClick={() => setOpen(false)} className="row" style={{ justifyContent: "center", padding: 12, borderTop: "1px solid var(--line-2)", fontWeight: 560, fontSize: 14 }}>Все уведомления</Link>
        </div>
      )}
    </div>
  );
}

export function PublicLayout() {
  const { me } = useSession();
  const [scrolled, setScrolled] = useState(false);
  useEffect(() => {
    const h = () => setScrolled(window.scrollY > 8);
    window.addEventListener("scroll", h, { passive: true });
    return () => window.removeEventListener("scroll", h);
  }, []);
  return (
    <MarketProvider userId={null}>
      <ModeBanner />
      <header className={`site-head ${scrolled ? "scrolled" : ""}`}>
        <div className="in">
          <Link to="/" className="brand"><Logo />AutoInvest</Link>
          <nav aria-label="Сайт">
            <a href="/#how">Как это работает</a>
            <a href="/#strategies">Стратегии</a>
            <Link to="/calculator">Калькулятор</Link>
            <a href="/#pricing">Тарифы</a>
            <a href="/#faq">Вопросы</a>
          </nav>
          <span style={{ flex: 1 }} />
          {me?.user ? (
            <Link to="/app" className="btn btn-primary btn-sm">Личный кабинет</Link>
          ) : (
            <div className="row g8">
              <Link to="/login" className="btn btn-ghost btn-sm">Войти</Link>
              <Link to="/register" className="btn btn-primary btn-sm">Открыть копилку</Link>
            </div>
          )}
        </div>
      </header>
      <Suspense fallback={<PageLoader />}><Outlet /></Suspense>
      <SiteFooter />
    </MarketProvider>
  );
}

function SiteFooter() {
  return (
    <footer className="site-foot">
      <div className="wrapx">
        <div className="cols">
          <div>
            <Link to="/" className="brand" style={{ padding: 0, marginBottom: 12 }}><Logo size={26} />AutoInvest</Link>
            <p style={{ maxWidth: 340 }}>Регулярные накопления в долларе, золоте и криптовалюте за белорусские рубли.</p>
            <p className="mt16">support@autoinvest.by</p>
          </div>
          <div>
            <h4>Сервис</h4>
            <a href="/#how">Как это работает</a>
            <a href="/#strategies">Стратегии</a>
            <Link to="/calculator">Калькулятор</Link>
            <a href="/#pricing">Тарифы</a>
          </div>
          <div>
            <h4>Документы</h4>
            <Link to="/legal/offer">Публичная оферта</Link>
            <Link to="/legal/privacy">Обработка персональных данных</Link>
            <Link to="/legal/risks">Уведомление о рисках</Link>
            <Link to="/legal/tax">Налоги: гид по Декрету № 8</Link>
          </div>
          <div>
            <h4>Помощь</h4>
            <a href="/#faq">Частые вопросы</a>
            <Link to="/app/support">Написать в поддержку</Link>
          </div>
        </div>
        <p className="legal">
          AutoInvest — технологический сервис автоматизации регулярных покупок. Сделки с цифровыми знаками (токенами) проводит оператор криптоплатформы — резидент Парка высоких технологий, платежи по картам — платёжный агрегатор bePaid. AutoInvest не является банком, не принимает вклады и не гарантирует доход. Стоимость цифровых активов может снижаться, в том числе значительно. Прошлая доходность не означает доходность в будущем.
        </p>
      </div>
    </footer>
  );
}

export function LogoutButton() {
  const { reload } = useSession();
  const nav = useNavigate();
  return (
    <Button variant="ghost" icon={<LogOut />} onClick={async () => { await api.post("/auth/logout"); await reload(); nav("/"); }}>
      Выйти
    </Button>
  );
}
