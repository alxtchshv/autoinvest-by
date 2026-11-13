import { Suspense, useEffect, useRef, useState, type ReactNode } from "react";
import { BiometricOffer } from "./BiometricOffer";
import { TelegramOffer } from "./TelegramOffer";
import { DemoBar, DemoNavButton, OnboardingGuard, WaitlistModal } from "./Waitlist";
import { Link, NavLink, Navigate, Outlet, useLocation, useNavigate } from "react-router-dom";
import { Menu, X, PanelLeftClose, PanelLeftOpen, Bell, ChartCandlestick, CreditCard, Eye, EyeOff, History, House, LifeBuoy, PiggyBank, Calculator, Shield, LogOut, User } from "lucide-react";
import { api } from "../lib/api";
import { trackVisit } from "../lib/utm";
import { ago, initials } from "../lib/format";
import { useLoad } from "../lib/hooks";
import { MarketProvider, useSession, useUserEvent } from "../lib/session";
import type { Notification } from "../lib/types";
import { Button, Logo, PageLoader, useHide, useToast } from "./ui";

export function ModeBanner() {
  const { config } = useSession();
  if (!config) return null;
  // до запуска сайт выглядит как настоящий: плашки «тестовый режим» нет, демо-счёт помечен отдельно (DemoBar)
  if (config.launch === "waitlist" && !config.market_demo) return null;
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

const SIDE_KEY = "ai_side_collapsed";

export function AppShell() {
  const { me } = useSession();
  const [collapsed, setCollapsed] = useState(() => { try { return localStorage.getItem(SIDE_KEY) === "1"; } catch { return false; } });
  const toggleSide = () => setCollapsed((c) => { try { localStorage.setItem(SIDE_KEY, c ? "0" : "1"); } catch { /* */ } return !c; });
  const item = (to: string, label: string, Icon: typeof House, end?: boolean) => (
    <NavLink key={to} to={to} end={end} title={collapsed ? label : undefined}><Icon /><span className="nav-label">{label}</span></NavLink>
  );
  return (
    <MarketProvider userId={me?.user?.id ?? null}>
      <div className={`shell ${collapsed ? "collapsed" : ""}`}>
        <aside className="side">
          <div className="side-top">
            <Link to="/app" className="brand" title="AutoInvest"><Logo /><span className="nav-label">AutoInvest</span></Link>
            <button className="side-toggle" onClick={toggleSide} aria-label={collapsed ? "Развернуть меню" : "Свернуть меню"} title={collapsed ? "Развернуть меню" : "Свернуть меню"}>
              {collapsed ? <PanelLeftOpen /> : <PanelLeftClose />}
            </button>
          </div>
          <nav className="nav" aria-label="Разделы">
            {NAV.map((n) => item(n.to, n.label, n.icon, n.end))}
            <div className="nav-sep" />
            <DemoNavButton collapsed={collapsed} />
            {item("/app/support", "Поддержка", LifeBuoy)}
            {me?.user && (me.user.role === "admin" || me.user.role === "support") && item("/admin", "Админка", Shield)}
          </nav>
          <div className="side-foot">
            <NavLink to="/app/profile" className="side-user" title={collapsed ? (me?.user?.name || "Профиль") : undefined}>
              <span className="avatar">{initials(me?.user?.name || "")}</span>
              <span className="grow nav-label" style={{ minWidth: 0 }}>
                <div style={{ fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{me?.user?.name}</div>
                <div className="tiny muted">{me?.is_pro ? "Тариф Pro" : "Тариф Старт"}</div>
              </span>
            </NavLink>
          </div>
        </aside>
        <div className="main">
          <ModeBanner />
          <DemoBar />
          <TopBar />
          <main className="content" id="main">
            <OnboardingGuard />
            <TelegramOffer />
            <BiometricOffer />
            <WaitlistModal />
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

/** Ссылка на раздел главной: на главной плавно прокручивает, с других страниц — переходит и докручивает. */
function SectionLink({ id, children, onClick }: { id: string; children: ReactNode; onClick?: () => void }) {
  const nav = useNavigate();
  const loc = useLocation();
  return (
    <a href={"/#" + id} onClick={(e) => {
      e.preventDefault();
      onClick?.();
      if (loc.pathname === "/") {
        document.getElementById(id)?.scrollIntoView({ behavior: "smooth" });
        history.replaceState(null, "", "/#" + id);
      } else nav("/#" + id);
    }}>{children}</a>
  );
}

const SITE_LINKS: [string, string][] = [["how", "Как это работает"], ["strategies", "Планы"], ["calc", "Калькулятор"], ["pricing", "Тарифы"], ["faq", "Вопросы"]];

export function PublicLayout() {
  const { me } = useSession();
  const loc = useLocation();
  useEffect(() => { trackVisit(); }, []);
  const [scrolled, setScrolled] = useState(false);
  const [menu, setMenu] = useState(false);
  useEffect(() => {
    const h = () => setScrolled(window.scrollY > 8);
    h();
    window.addEventListener("scroll", h, { passive: true });
    return () => window.removeEventListener("scroll", h);
  }, []);
  useEffect(() => setMenu(false), [loc.pathname]);
  const links = (close?: () => void) => SITE_LINKS.map(([id, label]) => id === "calc"
    ? <NavLink key={id} to="/calculator" onClick={() => { close?.(); window.scrollTo({ top: 0, behavior: "smooth" }); }}>{label}</NavLink>
    : <SectionLink key={id} id={id} onClick={close}>{label}</SectionLink>);
  return (
    <MarketProvider userId={null}>
      <ModeBanner />
      <header className={`site-head ${scrolled || menu ? "scrolled" : ""}`}>
        <div className="in">
          <Link to="/" className="brand"><Logo />AutoInvest</Link>
          <nav aria-label="Сайт">{links()}</nav>
          <span style={{ flex: 1 }} />
          {me?.user ? (
            <Link to="/app" className="btn btn-primary btn-sm">Личный кабинет</Link>
          ) : (
            <div className="row g8">
              <Link to="/login" className="btn btn-ghost btn-sm">Войти</Link>
              <Link to="/register" className="btn btn-primary btn-sm hide-sm">Начать копить</Link>
            </div>
          )}
          <button className="burger" aria-label={menu ? "Закрыть меню" : "Открыть меню"} aria-expanded={menu} onClick={() => setMenu(!menu)}>{menu ? <X /> : <Menu />}</button>
        </div>
        <div className={`mnav ${menu ? "open" : ""}`} inert={!menu}>
          <div>
            {links(() => setMenu(false))}
            {!me?.user && <Link to="/register" onClick={() => setMenu(false)}>Начать копить</Link>}
          </div>
        </div>
      </header>
      {/* смена страницы — мягкое появление, как прокрутка к разделам */}
      <div key={loc.pathname} className="page-in"><Suspense fallback={<PageLoader />}><Outlet /></Suspense></div>
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
            <SectionLink id="how">Как это работает</SectionLink>
            <SectionLink id="strategies">Планы</SectionLink>
            <NavLink to="/calculator">Калькулятор</NavLink>
            <SectionLink id="pricing">Тарифы</SectionLink>
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
            <SectionLink id="faq">Частые вопросы</SectionLink>
            <Link to="/app/support">Написать в поддержку</Link>
          </div>
        </div>
        <p className="legal"><span>
          AutoInvest — технологический сервис автоматизации регулярных покупок. Сделки с цифровыми знаками (токенами) проводит оператор криптоплатформы — резидент Парка высоких технологий, платежи по картам — платёжный агрегатор bePaid. AutoInvest не является банком, не принимает вклады и не гарантирует доход. Стоимость цифровых активов может снижаться, в том числе значительно. Прошлая доходность не означает доходность в будущем.
        </span></p>
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
