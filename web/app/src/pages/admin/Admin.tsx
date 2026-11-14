import { DatePicker } from "../../components/DatePicker";
import { Suspense, useState } from "react";
import { Link, NavLink, Outlet, useNavigate, useParams, useSearchParams } from "react-router-dom";
import { ArrowLeft, Download, PanelLeftClose, PanelLeftOpen, Gauge, IdCard, LayoutDashboard, ListOrdered, MessageSquare, Search, Users, House } from "lucide-react";
import { api } from "../../lib/api";
import { ago, byn, dateNum, dateTime, kop } from "../../lib/format";
import { useLoad, useSubmit, useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { Card, KYC, Order, Plan, Ticket, User } from "../../lib/types";
import { Accordion, Button, Empty, Field, Input, LoadError, Logo, Modal, Money, Notice, OrderStatus, PageLoader, PlanStatus, Segmented, useToast } from "../../components/ui";
import { ModeBanner } from "../../components/layout";
import { ChatView } from "../app/Support";

const SOURCE_NAMES: Record<string, string> = { nbrb: "Курсы НБРБ", binance: "Binance", bybit: "Bybit", coingecko: "CoinGecko", yahoo: "Yahoo Finance (акции США)" };

const ADMIN_SIDE_KEY = "ai_admin_side_collapsed";

export function AdminLayout() {
  const { me } = useSession();
  const [collapsed, setCollapsed] = useState(() => { try { return localStorage.getItem(ADMIN_SIDE_KEY) === "1"; } catch { return false; } });
  const toggle = () => setCollapsed((c) => { try { localStorage.setItem(ADMIN_SIDE_KEY, c ? "0" : "1"); } catch { /* */ } return !c; });
  const item = (to: string, label: string, Icon: typeof House, end?: boolean) => (
    <NavLink to={to} end={end} title={collapsed ? label : undefined}><Icon /><span className="nav-label">{label}</span></NavLink>
  );
  const name = me?.user?.name || "";
  const ini = name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]).join("").toUpperCase() || "A";
  return (
    <div className={`shell admin-shell ${collapsed ? "collapsed" : ""}`}>
      <aside className="side">
        <div className="side-top">
          <Link to="/admin" className="brand" title="Админка"><Logo /><span className="nav-label">Админка</span></Link>
          <button className="side-toggle" onClick={toggle} aria-label={collapsed ? "Развернуть меню" : "Свернуть меню"} title={collapsed ? "Развернуть меню" : "Свернуть меню"}>
            {collapsed ? <PanelLeftOpen /> : <PanelLeftClose />}
          </button>
        </div>
        <nav className="nav">
          {item("/admin", "Обзор", LayoutDashboard, true)}
          {item("/admin/users", "Пользователи", Users)}
          {item("/admin/kyc", "Верификация", IdCard)}
          {item("/admin/tickets", "Поддержка", MessageSquare)}
          {item("/admin/orders", "Операции", ListOrdered)}
          {item("/admin/system", "Система", Gauge)}
          <div className="nav-sep" />
          {item("/app", "В приложение", House)}
        </nav>
        {/* сотрудник — внизу слева, за тонкой полупрозрачной линией, как в кабинете */}
        <div className="side-foot">
          <div className="side-user" title={collapsed ? `${me?.user?.email} · ${me?.user?.role === "admin" ? "администратор" : "поддержка"}` : undefined}>
            <span className="avatar">{ini}</span>
            <span className="grow nav-label" style={{ minWidth: 0 }}>
              <div style={{ fontWeight: 600, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{me?.user?.email}</div>
              <div className="tiny">{me?.user?.role === "admin" ? "Администратор" : "Поддержка"}</div>
            </span>
          </div>
        </div>
      </aside>
      <div className="main">
        <ModeBanner />
        <header className="topbar"><span className="page-title">Панель управления</span><span className="spacer" /><Link to="/app" className="btn btn-ghost btn-sm mobile-brand">В приложение</Link></header>
        <main className="content"><Suspense fallback={<PageLoader />}><Outlet /></Suspense></main>
      </div>
    </div>
  );
}

type Overview = {
  users: number; users_today: number; users_7d: number; active_plans: number; paused_plans: number; investors: number; pro: number;
  volume_kop: number; volume_30d_kop: number; fees_kop: number; orders_30d: number; failed_orders_7d: number; open_tickets: number;
  pending_kyc: number; pending_payouts: number; monthly_run_kop: number;
};

export function AdminOverview() {
  const { me } = useSession();
  useTitle("Админка");
  const { data, error, loading, reload } = useLoad(() => api.get<{ overview: Overview; series: { day: string; signups: number; volume_kop: number }[] }>("/admin/overview"), []);
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const o = data!.overview;
  const maxV = Math.max(1, ...data!.series.map((s) => s.volume_kop));
  const maxS = Math.max(1, ...data!.series.map((s) => s.signups));
  const M = ({ l, v, sub }: { l: string; v: React.ReactNode; sub?: React.ReactNode }) => <div><div className="l">{l}</div><div className="v num">{v}</div>{sub && <div className="tiny muted">{sub}</div>}</div>;
  return (
    <div className="stack g16">
      <div className="page-head" style={{ marginBottom: 4 }}><h1>Обзор</h1></div>
      <LaunchFunnel />
      {me?.user?.role === "admin" && <LaunchAnnounce />}
      {(o.pending_kyc > 0 || o.open_tickets > 0 || o.pending_payouts > 0) && (
        <div className="row g8 wrap">
          {o.open_tickets > 0 && <Link to="/admin/tickets?status=open" className="btn btn-secondary btn-sm">Обращений без ответа: {o.open_tickets}</Link>}
          {o.pending_kyc > 0 && <Link to="/admin/kyc" className="btn btn-secondary btn-sm">Ждут верификации: {o.pending_kyc}</Link>}
          {o.pending_payouts > 0 && <Link to="/admin/orders?status=payout_pending" className="btn btn-secondary btn-sm">Ручных выплат: {o.pending_payouts}</Link>}
        </div>
      )}
      <section className="sheet">
        <div className="metric-grid">
          <M l="Пользователи" v={o.users} sub={`+${o.users_today} сегодня · +${o.users_7d} за неделю`} />
          <M l="Инвесторы" v={o.investors} sub={o.users ? `${Math.round((o.investors / o.users) * 100)}% конверсия` : ""} />
          <M l="Активные копилки" v={o.active_plans} sub={`${o.paused_plans} на паузе`} />
          <M l="Подписки Pro" v={o.pro} />
          <M l="Объём пополнений" v={<Money k={o.volume_kop} />} sub="за всё время" />
          <M l="За 30 дней" v={<Money k={o.volume_30d_kop} />} sub={`${o.orders_30d} операций`} />
          <M l="Выручка комиссий" v={<Money k={o.fees_kop} />} />
          <M l="Плановый объём в месяц" v={<Money k={o.monthly_run_kop} />} sub={`отказов за неделю: ${o.failed_orders_7d}`} />
        </div>
      </section>
      <div className="grid2">
        <section className="sheet"><div className="sheet-head"><h2>Пополнения, 30 дней</h2></div><div className="sheet-body"><div className="bars">{data!.series.map((s) => <span key={s.day} title={`${s.day}: ${byn(s.volume_kop)}`} style={{ height: (s.volume_kop / maxV) * 100 + "%" }} />)}</div></div></section>
        <section className="sheet"><div className="sheet-head"><h2>Регистрации, 30 дней</h2></div><div className="sheet-body"><div className="bars">{data!.series.map((s) => <span key={s.day} title={`${s.day}: ${s.signups}`} style={{ height: (s.signups / maxS) * 100 + "%", background: "#161a23" }} />)}</div></div></section>
      </div>
      <PartnerMetrics />
    </div>
  );
}

type Metrics = {
  metrics: {
    funnel: { registered: number; email_verified: number; with_card: number; with_plan: number; invested: number };
    retention_30d: number; retention_base: number; avg_ticket_kop: number; active_investors: number;
    asset_volume_30d: { asset: string; amount_kop: number }[];
    cohorts: { month: string; registered: number; invested: number; active_30d: number }[];
  };
  holdings: { asset: string; qty: number; cost_kop: number; value_kop: number }[];
  aum_kop: number; cost_kop: number;
};

// Показатели, которые спрашивают оператор-партнёр, банк или инвестор: воронка, удержание, активы под учётом.
function PartnerMetrics() {
  const { data } = useLoad(() => api.get<Metrics>("/admin/metrics"), []);
  if (!data) return null;
  const m = data.metrics;
  const f = m.funnel;
  const steps: [string, number][] = [["Зарегистрировались", f.registered], ["Подтвердили почту", f.email_verified], ["Привязали карту", f.with_card], ["Создали копилку", f.with_plan], ["Сделали первое пополнение", f.invested]];
  const pctOf = (v: number, base: number) => (base ? `${Math.round((v / base) * 100)}%` : "—");
  const totalVol = m.asset_volume_30d.reduce((a, x) => a + x.amount_kop, 0);
  return (
    <>
      <div className="page-head" style={{ margin: "16px 0 0" }}><div><h2>Для партнёров и инвесторов</h2><p>Воронка, удержание и активы под учётом — по данным базы, без оценок</p></div></div>
      <section className="sheet">
        <div className="metric-grid">
          <div><div className="l">Активы под учётом</div><div className="v num"><Money k={data.aum_kop} /></div><div className="tiny muted">вложено <Money k={data.cost_kop} /></div></div>
          <div><div className="l">Активные инвесторы</div><div className="v num">{m.active_investors}</div><div className="tiny muted">пополняли за 30 дней</div></div>
          <div><div className="l">Удержание 30 дней</div><div className="v num">{m.retention_base ? `${Math.round(m.retention_30d * 100)}%` : "—"}</div><div className="tiny muted">из {m.retention_base} инвесторов старше месяца</div></div>
          <div><div className="l">Средний взнос</div><div className="v num"><Money k={m.avg_ticket_kop} /></div><div className="tiny muted">за 30 дней</div></div>
        </div>
      </section>
      <div className="grid2">
        <section className="sheet">
          <div className="sheet-head"><h2>Воронка</h2></div>
          <div className="sheet-body stack g12">
            {steps.map(([label, v], i) => (
              <div key={label}>
                <div className="row between small"><span>{label}</span><span className="num">{v} <span className="muted">· {i === 0 ? "100%" : pctOf(v, f.registered)}</span></span></div>
                <div className="bar"><span style={{ width: f.registered ? `${(v / f.registered) * 100}%` : "0%" }} /></div>
              </div>
            ))}
          </div>
        </section>
        <section className="sheet">
          <div className="sheet-head"><h2>Пополнения по активам, 30 дней</h2></div>
          <div className="sheet-body stack g12">
            {m.asset_volume_30d.length === 0 && <p className="small muted">Пополнений пока не было.</p>}
            {m.asset_volume_30d.map((a) => (
              <div key={a.asset}>
                <div className="row between small"><span>{a.asset}</span><span className="num">{byn(a.amount_kop)} <span className="muted">· {pctOf(a.amount_kop, totalVol)}</span></span></div>
                <div className="bar"><span style={{ width: totalVol ? `${(a.amount_kop / totalVol) * 100}%` : "0%" }} /></div>
              </div>
            ))}
          </div>
        </section>
      </div>
      <section className="sheet">
        <div className="sheet-head"><h2>Когорты по месяцу регистрации</h2></div>
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>Месяц</th><th className="r">Зарегистрировались</th><th className="r">Начали инвестировать</th><th className="r">Активны последние 30 дней</th></tr></thead>
            <tbody>{m.cohorts.map((c) => (
              <tr key={c.month}><td>{c.month}</td><td className="r num">{c.registered}</td><td className="r num">{c.invested} <span className="muted">· {pctOf(c.invested, c.registered)}</span></td><td className="r num">{c.active_30d} <span className="muted">· {pctOf(c.active_30d, c.registered)}</span></td></tr>
            ))}</tbody>
          </table>
        </div>
      </section>
    </>
  );
}

type AdminUserRow = User & { plans: number; invested_kop: number; cards: number };

export function AdminUsers() {
  useTitle("Пользователи");
  const nav = useNavigate();
  const [q, setQ] = useState("");
  const [query, setQuery] = useState("");
  const [page, setPage] = useState(1);
  const { data, error, loading, reload } = useLoad(() => api.get<{ users: AdminUserRow[]; total: number; per: number }>(`/admin/users?q=${encodeURIComponent(query)}&page=${page}`), [query, page]);
  return (
    <div>
      <div className="page-head"><div><h1>Пользователи</h1>{data && <p>Всего {data.total}</p>}</div>
        <form onSubmit={(e) => { e.preventDefault(); setPage(1); setQuery(q); }} style={{ position: "relative", width: 320, maxWidth: "100%" }}>
          <Search size={16} className="muted" style={{ position: "absolute", left: 12, top: 14 }} />
          <Input placeholder="Почта, имя, телефон или ID" value={q} onChange={(e) => setQ(e.target.value)} style={{ paddingLeft: 36 }} />
        </form>
      </div>
      {loading && !data ? <PageLoader /> : error ? <LoadError error={error} retry={reload} /> : (
        <div className="sheet">
          <div className="table-wrap"><table className="table">
            <thead><tr><th>ID</th><th>Пользователь</th><th>Статус</th><th>KYC</th><th className="r">Копилки</th><th className="r">Вложено</th><th>Регистрация</th></tr></thead>
            <tbody>{data!.users.map((u) => (
              <tr key={u.id} className="click" onClick={() => nav(`/admin/users/${u.id}`)}>
                <td className="num muted">{u.id}</td>
                <td><div style={{ fontWeight: 560 }}>{u.name}{u.role !== "user" && <span className="badge badge-ink" style={{ marginLeft: 6 }}>{u.role}</span>}</div><div className="small muted">{u.email}</div></td>
                <td>{u.status === "blocked" ? <span className="badge badge-down">Заблокирован</span> : u.tier === "pro" && u.pro_until && u.pro_until * 1000 > Date.now() ? <span className="badge badge-ink">Pro</span> : <span className="badge">Старт</span>}</td>
                <td>{kycBadge(u.kyc_status)}</td>
                <td className="r num">{u.plans}</td>
                <td className="r"><Money k={u.invested_kop} /></td>
                <td className="small muted nowrap">{dateNum(u.created_at)}</td>
              </tr>
            ))}</tbody>
          </table></div>
          {data!.total > data!.per && <div className="sheet-foot row between"><Button size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>Назад</Button><span className="small muted">{page} / {Math.ceil(data!.total / data!.per)}</span><Button size="sm" disabled={page * data!.per >= data!.total} onClick={() => setPage(page + 1)}>Дальше</Button></div>}
        </div>
      )}
    </div>
  );
}

function kycBadge(s: string) {
  if (s === "approved") return <span className="badge badge-up">Пройдена</span>;
  if (s === "pending") return <span className="badge badge-warn">На проверке</span>;
  if (s === "rejected") return <span className="badge badge-down">Отказ</span>;
  return <span className="badge">—</span>;
}

export function AdminUser() {
  const { id } = useParams();
  const { me } = useSession();
  const toast = useToast();
  const nav = useNavigate();
  const { data, error, loading, reload } = useLoad(() => api.get<{ user: User; is_pro: boolean; plans: Plan[]; cards: Card[]; orders: Order[]; kyc: KYC | null; audit: { action: string; details: string; ip: string; created_at: number }[]; tickets: Ticket[]; telegram: { id: number; dir: "in" | "out"; kind: string; text: string; created_at: number }[] }>(`/admin/users/${id}`), [id]);
  const [days, setDays] = useState("30");
  useTitle(data?.user.name || "Пользователь");
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const d = data!;
  const u = d.user;
  const isAdmin = me?.user?.role === "admin";
  const act = async (path: string, body: unknown, msg: string) => {
    try { await api.post(`/admin/users/${u.id}/${path}`, body); toast(msg); reload(); } catch (e: any) { toast(e.message, "error"); }
  };
  return (
    <div className="stack g16">
      <div>
        <Link to="/admin/users" className="back"><ArrowLeft />Пользователи</Link>
        <div className="page-head" style={{ marginBottom: 0 }}>
          <div><h1>{u.name}</h1><p>{u.email}{u.phone ? ` · ${u.phone}` : ""} · ID {u.id} · с {dateNum(u.created_at)}</p>
            {u.status === "blocked" && <p className="small down mt4">Заблокирован{u.block_reason ? `: ${u.block_reason}` : " (причина не указана)"}</p>}</div>
          {isAdmin && (
            <div className="row g8 wrap">
              {u.status === "blocked" ? <Button size="sm" onClick={() => act("status", { status: "active" }, "Разблокирован")}>Разблокировать</Button> : <Button size="sm" onClick={() => {
                const r = prompt("Причина блокировки — её увидит пользователь при попытке войти");
                if (r === null) return;
                if (!r.trim()) { toast("Укажите причину блокировки", "error"); return; }
                act("status", { status: "blocked", reason: r.trim() }, "Заблокирован");
              }}>Заблокировать</Button>}
              {u.role === "user" && <Button size="sm" variant="danger" onClick={async () => {
                if (!confirm(`Удалить аккаунт ${u.email}?\n\nКопилки закроются, карты, Google, Face ID и Telegram отвяжутся, личные данные сотрутся. Отменить нельзя. Почтой и Google можно будет зарегистрироваться заново.`)) return;
                try { await api.post(`/admin/users/${u.id}/delete`, {}); toast("Аккаунт удалён"); nav("/admin/users"); } catch (e: any) { toast(e.message, "error"); }
              }}>Удалить аккаунт</Button>}
              <select className="select" style={{ height: 34, width: 150 }} value={u.role} onChange={(e) => act("role", { role: e.target.value }, "Роль изменена")} aria-label="Роль">
                <option value="user">Пользователь</option><option value="support">Поддержка</option><option value="admin">Администратор</option>
              </select>
            </div>
          )}
        </div>
      </div>
      <div className="home-grid">
        <div className="stack g16">
          <section className="sheet">
            <div className="sheet-head"><h2>Копилки</h2></div>
            {d.plans.length === 0 ? <div className="sheet-body small muted">Нет копилок</div> : (
              <div className="table-wrap"><table className="table"><thead><tr><th>Название</th><th>Статус</th><th className="r">Пополнение</th><th className="r">Вложено</th><th className="r">Стоимость</th></tr></thead><tbody>
                {d.plans.map((p) => <tr key={p.id}><td>{p.name}<div className="tiny muted">{p.allocations.map((a) => `${a.asset} ${a.weight_bp / 100}%`).join(" · ")}</div></td><td><PlanStatus s={p.status} /></td><td className="r num">{kop(p.amount_kop)} · {p.frequency}</td><td className="r"><Money k={p.valuation.invested_kop} /></td><td className="r"><Money k={p.valuation.value_kop} /></td></tr>)}
              </tbody></table></div>
            )}
          </section>
          <section className="sheet">
            <div className="sheet-head"><h2>Данные пользователя</h2></div>
            <div className="sheet-body stack g12">
              <span className="muted">Операции, переписку и журнал скачивайте файлом — открываются в Excel. Каждая выгрузка записывается в журнал пользователя.</span>
              <div className="export-grid">
                <a className="export-tile" href={`/api/v1/admin/users/${u.id}/export/operations`} download><Download /><b>Операции</b><span>{d.orders.length ? `${d.orders.length}${d.orders.length >= 50 ? "+" : ""} шт.` : "пока нет"}</span></a>
                <a className="export-tile" href={`/api/v1/admin/users/${u.id}/export/telegram`} download><Download /><b>Переписка в Telegram</b><span>{d.telegram.length ? `${d.telegram.length}${d.telegram.length >= 300 ? "+" : ""} сообщ.` : "нет сообщений"}</span></a>
                <a className="export-tile" href={`/api/v1/admin/users/${u.id}/export/audit`} download><Download /><b>Журнал действий</b><span>входы, карты, копилки, выводы</span></a>
              </div>
            </div>
          </section>
        </div>
        <div className="stack g16">
          <section className="sheet"><div className="sheet-head"><h2>Статус</h2></div><div className="sheet-body"><dl className="kv">
            <dt>Аккаунт</dt><dd>{u.status === "active" ? "Активен" : u.status}</dd>
            <dt>Тариф</dt><dd>{d.is_pro ? `Pro до ${dateNum(u.pro_until!)}` : "Старт"}</dd>
            <dt>Верификация</dt><dd>{kycBadge(u.kyc_status)}</dd>
            <dt>Почта</dt><dd>{u.email_verified ? "Подтверждена" : "Не подтверждена"}</dd>
            <dt>2FA</dt><dd>{u.totp_enabled ? "Включена" : "Нет"}</dd>
            <dt>Telegram</dt><dd>{u.telegram_username ? "@" + u.telegram_username : "—"}</dd>
            <dt>Риск-профиль</dt><dd>{u.risk_profile || "—"}</dd>
            <dt>Лист ожидания</dt><dd>{u.waitlist_at ? dateNum(u.waitlist_at) : "—"}</dd>
            <dt>Источник</dt><dd>{u.utm_source ? `${u.utm_source}${u.utm_campaign ? " · " + u.utm_campaign : ""}` : "прямой заход"}</dd>
            <dt>Pro к запуску</dt><dd>{u.bonus_pro_days ? `+${u.bonus_pro_days} дн.` : "—"}</dd>
            <dt>Последний вход</dt><dd>{u.last_login_at ? ago(u.last_login_at) : "—"}</dd>
            <dt>Карты</dt><dd>{d.cards.map((c) => `${c.brand} •• ${c.last4}`).join(", ") || "—"}</dd>
          </dl>
            {isAdmin && <div className="row g8 mt16"><Input style={{ width: 90, height: 34 }} value={days} onChange={(e) => setDays(e.target.value)} aria-label="Дней Pro" /><Button size="sm" onClick={() => act("pro", { days: Number(days) }, "Pro продлён")}>Выдать Pro (дней)</Button></div>}
          </div></section>
          <UserSupport userId={u.id} />
        </div>
      </div>
    </div>
  );
}

function AdminOrdersTable({ orders, onChange }: { orders: Order[]; onChange: () => void }) {
  const { me } = useSession();
  const toast = useToast();
  if (!orders.length) return <div className="sheet-body small muted">Операций нет</div>;
  return (
    <div className="table-wrap"><table className="table">
      <thead><tr><th>№</th><th>Дата</th><th>Клиент / копилка</th><th>Тип</th><th>Статус</th><th className="r">Сумма</th><th className="r">Комиссия</th><th /></tr></thead>
      <tbody>{orders.map((o) => (
        <tr key={o.id}>
          <td className="num muted">{o.id}</td>
          <td className="small nowrap">{dateTime(o.created_at)}</td>
          <td className="small">{o.user_email && <Link to={`/admin/users/${o.user_id}`}>{o.user_email}</Link>}<div className="muted">{o.plan_name}</div></td>
          <td className="small">{o.side === "buy" ? "Пополнение" : "Вывод"}<div className="tiny muted">{o.trigger}</div></td>
          <td><OrderStatus s={o.status} />{o.error && <div className="tiny muted" style={{ maxWidth: 220 }}>{o.error}</div>}</td>
          <td className="r num">{kop(o.amount_kop)}</td>
          <td className="r num muted">{kop(o.fee_kop)}</td>
          <td>{o.status === "manual" && me?.user?.role === "admin" && (
            <div className="row g4">
              <Button size="sm" onClick={async () => { if (!confirm("Исполнить покупку по текущим ценам?")) return; try { await api.post(`/admin/orders/${o.id}/fill`); toast("Покупка исполнена"); onChange(); } catch (e: any) { toast(e.message, "error"); } }}>Исполнить</Button>
              <Button size="sm" variant="ghost" onClick={async () => { if (!confirm(`Вернуть ${byn(o.amount_kop)} на карту клиента?`)) return; try { await api.post(`/admin/orders/${o.id}/refund`); toast("Возврат оформлен"); onChange(); } catch (e: any) { toast(e.message, "error"); } }}>Вернуть</Button>
            </div>
          )}{o.status === "payout_pending" && me?.user?.role === "admin" && <Button size="sm" onClick={async () => { if (!confirm(`Подтвердить, что ${byn(o.amount_kop)} выплачено на карту •• ${o.card_last4}?`)) return; try { await api.post(`/admin/orders/${o.id}/payout-done`); toast("Выплата отмечена"); onChange(); } catch (e: any) { toast(e.message, "error"); } }}>Выплачено</Button>}</td>
        </tr>
      ))}</tbody>
    </table></div>
  );
}

// На экране — только то, что требует действия сотрудника; полная история — файлом за период.
export function AdminOrders() {
  useTitle("Операции");
  const [sp, setSp] = useSearchParams();
  const status = sp.get("status") || "payout_pending";
  const [page, setPage] = useState(1);
  const [period, setPeriod] = useState({ from: "", to: "", status: "" });
  const { data, error, loading, reload } = useLoad(() => api.get<{ orders: Order[]; total: number; per: number }>(`/admin/orders?page=${page}&status=${status}`), [status, page]);
  const qs = new URLSearchParams(Object.entries(period).filter(([, v]) => v)).toString();
  const today = new Date().toISOString().slice(0, 10);
  return (
    <div className="stack g16">
      <div className="page-head" style={{ marginBottom: 0 }}><div><h1>Операции</h1><p>Здесь — только то, что ждёт действия. Всю историю удобнее смотреть в Excel.</p></div></div>
      <section className="sheet sheet-pad">
        <div className="row wrap g12" style={{ alignItems: "flex-end" }}>
          <div className="stack g4 small export-date"><span className="muted">С</span><DatePicker value={period.from} min="2020-01-01" max={period.to || today} onChange={(v) => setPeriod({ ...period, from: v })} placeholder="С начала" /></div>
          <div className="stack g4 small export-date"><span className="muted">По</span><DatePicker value={period.to} min={period.from || "2020-01-01"} max={today} onChange={(v) => setPeriod({ ...period, to: v })} placeholder="По сегодня" /></div>
          <select className="select" style={{ width: 200 }} value={period.status} onChange={(e) => setPeriod({ ...period, status: e.target.value })} aria-label="Статус">
            <option value="">Все статусы</option><option value="filled">Исполненные</option><option value="failed">Отклонённые</option><option value="payout_pending">Ручные выплаты</option><option value="manual">На проверке</option>
          </select>
          <a className="btn btn-primary" href={`/api/v1/admin/orders/export.csv${qs ? "?" + qs : ""}`} download><Download />Скачать операции</a>
        </div>
      </section>
      <Segmented label="Требуют действия" value={status} onChange={(v) => { setPage(1); setSp({ status: v }); }} options={[
        { value: "payout_pending", label: "Ручные выплаты" }, { value: "manual", label: "На проверке" }, { value: "pending", label: "Зависли в обработке" }, { value: "failed", label: "Отклонены" },
      ]} />
      {loading && !data ? <PageLoader /> : error ? <LoadError error={error} retry={reload} /> : (
        <div className="sheet">
          {data!.orders.length === 0 ? <Empty title="Ничего не ждёт действия">Новые заявки появятся здесь автоматически.</Empty> : <AdminOrdersTable orders={data!.orders} onChange={reload} />}
          {data!.total > data!.per && <div className="sheet-foot row between"><Button size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>Назад</Button><span className="small muted">{page} / {Math.ceil(data!.total / data!.per)}</span><Button size="sm" disabled={page * data!.per >= data!.total} onClick={() => setPage(page + 1)}>Дальше</Button></div>}
        </div>
      )}
    </div>
  );
}

export function AdminKYC() {
  useTitle("Верификация");
  const toast = useToast();
  const [status, setStatus] = useState("pending");
  const { data, error, loading, reload } = useLoad(() => api.get<{ items: KYC[] }>(`/admin/kyc?status=${status}`), [status]);
  const [open, setOpen] = useState<KYC | null>(null);
  const [reason, setReason] = useState("");
  const s = useSubmit();
  const review = async (approve: boolean) => {
    try { await s.submit(() => api.post(`/admin/kyc/${open!.user_id}/review`, { approve, reason, submitted_at: open!.submitted_at })); toast(approve ? "Верификация подтверждена" : "Отказ отправлен"); setOpen(null); setReason(""); reload(); } catch { /* */ }
  };
  return (
    <div>
      <div className="page-head"><h1>Верификация</h1></div>
      <div style={{ marginBottom: 16 }}><Segmented label="Статус" value={status} onChange={setStatus} options={[{ value: "pending", label: "Ждут проверки" }, { value: "approved", label: "Подтверждены" }, { value: "rejected", label: "Отказы" }, { value: "all", label: "Все" }]} /></div>
      {loading && !data ? <PageLoader /> : error ? <LoadError error={error} retry={reload} /> : (
        <div className="sheet">
          {data!.items.length === 0 ? <Empty title="Заявок нет" /> : (
            <div className="list">{data!.items.map((k) => (
              <button key={k.user_id} className="list-row" onClick={() => setOpen(k)}>
                <div className="grow"><div className="title">{k.last_name} {k.first_name} {k.middle_name}</div><div className="sub">{k.user_email} · отправлено {ago(k.submitted_at)}</div></div>
                {kycBadge(k.status)}
              </button>
            ))}</div>
          )}
        </div>
      )}
      <Modal open={!!open} onClose={() => setOpen(null)} title="Проверка документов" wide>
        {open && (
          <div className="stack g16">
            <dl className="kv">
              <dt>ФИО</dt><dd>{open.last_name} {open.first_name} {open.middle_name}</dd>
              <dt>Дата рождения</dt><dd>{open.birth_date}</dd>
              <dt>Паспорт</dt><dd>{open.doc_number}, выдан {open.doc_issued_at || "—"}</dd>
              <dt>Личный номер</dt><dd>{open.personal_number}</dd>
              <dt>Гражданство</dt><dd>{open.citizenship}</dd>
              <dt>Адрес</dt><dd>{open.address || "—"}</dd>
            </dl>
            <div className="row g12 wrap">
              {open.has_doc && <a className="btn btn-secondary btn-sm" href={`/api/v1/admin/kyc/${open.user_id}/file/doc`} target="_blank" rel="noopener">Открыть паспорт</a>}
              {open.has_selfie && <a className="btn btn-secondary btn-sm" href={`/api/v1/admin/kyc/${open.user_id}/file/selfie`} target="_blank" rel="noopener">Открыть селфи</a>}
            </div>
            {open.status === "pending" ? (
              <>
                <Field label="Причина отказа" hint="Обязательна при отказе — её увидит пользователь" error={s.fieldError("reason")}><Input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="Например: фото нечитаемое" /></Field>
                {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
                <div className="row g8" style={{ justifyContent: "flex-end" }}><Button variant="danger" busy={s.busy} onClick={() => review(false)}>Отказать</Button><Button variant="primary" busy={s.busy} onClick={() => review(true)}>Подтвердить</Button></div>
              </>
            ) : open.reject_reason && <Notice kind="error">Отказ: {open.reject_reason}</Notice>}
          </div>
        )}
      </Modal>
    </div>
  );
}

export function AdminTickets() {
  useTitle("Поддержка");
  const [sp, setSp] = useSearchParams();
  const status = sp.get("status") || "";
  const { data, error, loading, reload } = useLoad(() => api.get<{ tickets: Ticket[]; categories: Record<string, string> }>(`/admin/tickets${status ? "?status=" + status : ""}`), [status]);
  return (
    <div>
      <div className="page-head"><h1>Поддержка</h1></div>
      <div style={{ marginBottom: 16 }}><Segmented label="Статус" value={status} onChange={(v) => setSp(v ? { status: v } : {})} options={[{ value: "", label: "Все" }, { value: "open", label: "Без ответа" }, { value: "answered", label: "Отвечено" }, { value: "closed", label: "Закрыто" }]} /></div>
      {loading && !data ? <PageLoader /> : error ? <LoadError error={error} retry={reload} /> : (
        <div className="sheet">
          {data!.tickets.length === 0 ? <Empty title="Обращений нет" /> : (
            <div className="list">{data!.tickets.map((t) => (
              <Link key={t.id} to={`/admin/tickets/${t.id}`} className="list-row">
                <div className="grow"><div className="title">#{t.id} {t.subject}</div><div className="sub">{t.user_email} · {data!.categories[t.category]} · {sourceLabel(t)} · {t.last_message}</div></div>
                <div className="end stack g4" style={{ alignItems: "flex-end" }}>{t.status === "open" ? <span className="badge badge-warn">Ждёт ответа</span> : t.status === "answered" ? <span className="badge badge-brand">Отвечено</span> : <span className="badge">Закрыто</span>}<span className="tiny muted">{ago(t.updated_at)}</span></div>
              </Link>
            ))}</div>
          )}
        </div>
      )}
    </div>
  );
}

const TICKET_CAT: Record<string, string> = {
  payments: "Платежи и карты", plans: "Копилки и пополнения", withdraw: "Вывод денег", account: "Аккаунт и вход",
  kyc: "Верификация", idea: "Идея по улучшению", bug: "Нашёл ошибку", other: "Другое",
};

/** Статус обращения по-русски: кто закрыл — тоже видно. */
function TicketBadge({ t }: { t: Ticket }) {
  if (t.status === "open") return <span className="badge badge-warn">Ждёт ответа</span>;
  if (t.status === "answered") return <span className="badge badge-up">Отвечено</span>;
  return <span className="badge">{t.closed_by === "client" ? "Закрыто клиентом" : t.closed_by === "staff" ? "Закрыто поддержкой" : "Закрыто"}</span>;
}

const sourceLabel = (t: Ticket) => (t.source === "telegram" ? "из Telegram" : "с сайта");

/** Вся переписка клиента с поддержкой: каждое обращение раскрывается целиком, можно открыть несколько. */
function SupportHistory({ tickets, exclude }: { tickets: Ticket[]; exclude?: number }) {
  const list = tickets.filter((t) => t.id !== exclude);
  if (!list.length) return <div className="sheet-body small muted">Других обращений нет.</div>;
  return (
    <div className="sheet-body faq-compact" style={{ paddingTop: 0, paddingBottom: 0 }}>
      <Accordion first={null} items={list.map((t) => [
        `#${t.id} · ${t.subject} · ${dateNum(t.created_at)} · ${sourceLabel(t)}`,
        <div className="stack g8" key={t.id}>
          <div className="row g8 wrap small muted"><TicketBadge t={t} /><span>{TICKET_CAT[t.category] || t.category}</span><Link to={`/admin/tickets/${t.id}`}>Открыть</Link></div>
          <ChatView ticket={t} mineIsStaff />
        </div>,
      ])} />
    </div>
  );
}

export function AdminTicket() {
  const { id } = useParams();
  const toast = useToast();
  const { data, error, loading, reload } = useLoad(() => api.get<{ ticket: Ticket; user: User; history: Ticket[]; plans: number }>(`/admin/tickets/${id}`), [id]);
  const [body, setBody] = useState("");
  const s = useSubmit();
  useTitle(data ? `#${data.ticket.id}` : "Обращение");
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const t = data!.ticket;
  const u = data!.user;
  const others = (data!.history || []).filter((x) => x.id !== t.id);
  const setStatus = async (status: string) => {
    try { await api.post(`/admin/tickets/${t.id}/status`, { status }); toast(status === "closed" ? "Обращение закрыто — клиенту отправлено уведомление" : "Обращение снова открыто"); reload(); } catch (e: any) { toast(e.message, "error"); }
  };
  return (
    <div className="stack g16 admin-ticket">
      <div>
        <Link to="/admin/tickets" className="back"><ArrowLeft />Поддержка</Link>
        <div className="page-head" style={{ marginBottom: 0 }}>
          <div>
            <h1>#{t.id} {t.subject}</h1>
            <p className="row g8 wrap"><TicketBadge t={t} /><span>{TICKET_CAT[t.category] || t.category}</span><span>· {sourceLabel(t)}</span><span>· создано {dateTime(t.created_at)}</span></p>
          </div>
          <Button size="sm" variant={t.status === "closed" ? "secondary" : "primary"} onClick={() => setStatus(t.status === "closed" ? "open" : "closed")}>{t.status === "closed" ? "Открыть снова" : "Закрыть обращение"}</Button>
        </div>
      </div>
      {(t.category === "idea" || t.category === "bug") && <RewardBar userId={t.user_id} category={t.category} />}
      <div className="home-grid">
        <div className="stack g16">
          <section className="sheet sheet-pad"><ChatView ticket={t} mineIsStaff /></section>
          <form className="sheet sheet-pad stack g12" onSubmit={async (e) => { e.preventDefault(); try { await s.submit(() => api.post(`/admin/tickets/${t.id}/reply`, { body })); setBody(""); reload(); } catch { /* */ } }}>
            <textarea className="textarea" placeholder="Ответ клиенту — придёт в кабинет, на почту и в Telegram (с кнопками «Ответить» и «Закрыть»)" value={body} onChange={(e) => setBody(e.target.value)} aria-label="Ответ" />
            {s.error && <Notice kind="error">{s.error}</Notice>}
            <div className="row" style={{ justifyContent: "flex-end" }}><Button type="submit" variant="primary" busy={s.busy} disabled={!body.trim()}>Ответить</Button></div>
          </form>
        </div>
        <div className="stack g16">
          <section className="sheet">
            <div className="sheet-head"><h2>Клиент</h2><Link to={`/admin/users/${u.id}`} className="small">Карточка</Link></div>
            <dl className="sheet-body kv">
              <dt>Имя</dt><dd>{u.name}</dd>
              <dt>Почта</dt><dd>{u.email}</dd>
              {u.phone && <><dt>Телефон</dt><dd>{u.phone}</dd></>}
              <dt>Telegram</dt><dd>{u.telegram_username ? `@${u.telegram_username}` : "не подключён"}</dd>
              <dt>Тариф</dt><dd>{u.tier === "pro" ? "Pro" : "Старт"}</dd>
              <dt>Копилок</dt><dd>{data!.plans}</dd>
              <dt>Лист ожидания</dt><dd>{u.waitlist_at ? `с ${dateNum(u.waitlist_at)}` : "нет"}</dd>
              <dt>Обращений</dt><dd>{(data!.history || []).length}</dd>
            </dl>
          </section>
          <section className="sheet">
            <div className="sheet-head"><h2>Вся переписка с клиентом</h2><span className="small muted">{others.length ? `ещё ${others.length}` : ""}</span></div>
            <SupportHistory tickets={data!.history || []} exclude={t.id} />
          </section>
        </div>
      </div>
    </div>
  );
}

/** Карточка пользователя: вся его переписка с поддержкой. */
function UserSupport({ userId }: { userId: number }) {
  const { data } = useLoad(() => api.get<{ tickets: Ticket[] }>(`/admin/users/${userId}/support`), [userId]);
  if (!data?.tickets.length) return null;
  return (
    <section className="sheet">
      <div className="sheet-head"><h2>Переписка с поддержкой</h2><span className="small muted">{data.tickets.length} обращ.</span></div>
      <SupportHistory tickets={data.tickets} />
    </section>
  );
}

// Награда за полезную идею или найденную ошибку: дни Pro начислятся при запуске.
function RewardBar({ userId, category }: { userId: number; category: string }) {
  const toast = useToast();
  const [busy, setBusy] = useState(0);
  const give = async (days: number) => {
    setBusy(days);
    try {
      const r = await api.post<{ bonus_pro_days: number }>(`/admin/users/${userId}/bonus`, { days, reason: category === "idea" ? "идея" : "ошибка" });
      toast(`Подарено +${days} дн. Pro, всего к запуску ${r.bonus_pro_days} дн.`);
    } catch (e: any) { toast(e.message, "error"); } finally { setBusy(0); }
  };
  return (
    <section className="sheet sheet-pad row between wrap g12 reward-bar">
      <span>{category === "idea" ? "Идея по улучшению" : "Сообщение об ошибке"} — если полезно, подарите время Pro к запуску:</span>
      <div className="row g8">{[7, 14, 30].map((d) => <Button key={d} size="sm" busy={busy === d} onClick={() => give(d)}>+{d} дн.</Button>)}</div>
    </section>
  );
}

type Funnel = { visits_30d: number; visits_today: number; registered: number; onboarded: number; plan_created: number; waitlist: number; demo_users: number; demo_invested: number; feedback: number; telegram_links: number; sources: { source: string; campaign: string; visits: number; registered: number; waitlist: number }[] };

// Воронка запуска: сколько людей дошло от посещения до листа ожидания — по ней оцениваем рекламу.
/** Рассылка о запуске: всем из листа ожидания — в кабинет, на почту и в Telegram. Сначала — проверка себе. */
function LaunchAnnounce() {
  const toast = useToast();
  const [title, setTitle] = useState("AutoInvest запущен — копилки открыты");
  const [body, setBody] = useState("Пополнения с карты работают. Как обещали, всем из списка ожидания — месяц Pro бесплатно. Зайдите в AutoInvest: ваша копилка уже настроена, осталось привязать карту.");
  const [busy, setBusy] = useState<"" | "test" | "all">("");
  const send = async (test: boolean, force = false) => {
    if (!test && !force && !confirm("Отправить новость о запуске всем из листа ожидания — в кабинет, на почту и в Telegram?")) return;
    setBusy(test ? "test" : "all");
    try {
      const r = await api.post<{ recipients: number; telegram: number; email: number }>("/admin/launch/announce", { title, body, test, force });
      toast(test ? "Отправлено вам — проверьте почту, Telegram и колокольчик" : `Отправляем ${r.recipients} людям: на почту ${r.email}, в Telegram ${r.telegram}`);
    } catch (e: any) {
      if (e?.code === "already_sent" && confirm(e.message)) { setBusy(""); return send(false, true); }
      toast(e.message, "error");
    } finally {
      setBusy("");
    }
  };
  return (
    <section className="sheet">
      <div className="sheet-head"><h2>Рассылка о запуске</h2><span className="small muted">всем из листа ожидания: кабинет, почта, Telegram</span></div>
      <div className="sheet-body stack g12">
        <Field label="Заголовок" htmlFor="ann-title"><Input id="ann-title" value={title} onChange={(e) => setTitle(e.target.value)} maxLength={120} /></Field>
        <Field label="Текст" htmlFor="ann-body"><textarea id="ann-body" className="textarea" rows={3} value={body} onChange={(e) => setBody(e.target.value)} maxLength={2000} /></Field>
        <div className="row g8 wrap">
          <Button busy={busy === "test"} onClick={() => send(true)}>Отправить себе для проверки</Button>
          <Button variant="primary" busy={busy === "all"} onClick={() => send(false)}>Отправить всем из списка</Button>
        </div>
        <span className="tiny muted">Telegram получат те, кто подключил бота; письма — все, у кого не выключены письма. Отправка идёт по одному сообщению, чтобы почта не посчитала это спамом.</span>
      </div>
    </section>
  );
}

function LaunchFunnel() {
  const { data } = useLoad(() => api.get<{ funnel: Funnel; mode: string }>("/admin/launch"), []);
  if (!data) return null;
  const f = data.funnel;
  const pct = (a: number, b: number) => (b ? `${Math.round((a / b) * 100)}%` : "—");
  const steps: [string, number, string][] = [
    ["Посетители за 30 дней", f.visits_30d, `сегодня ${f.visits_today}`],
    ["Зарегистрировались", f.registered, pct(f.registered, f.visits_30d) + " от посетителей"],
    ["Прошли анкету", f.onboarded, pct(f.onboarded, f.registered)],
    ["Настроили копилку", f.plan_created, pct(f.plan_created, f.registered)],
    ["В листе ожидания", f.waitlist, pct(f.waitlist, f.registered)],
    ["Открыли демо-счёт", f.demo_users, `пополняли: ${f.demo_invested}`],
    ["Подключили Telegram", f.telegram_links, pct(f.telegram_links, f.registered)],
    ["Идеи и ошибки", f.feedback, "обращений"],
  ];
  return (
    <section className="sheet">
      <div className="sheet-head"><h2>Запуск: воронка</h2><span className="small muted">{data.mode === "waitlist" ? "режим листа ожидания" : "сервис открыт"}</span></div>
      <div className="funnel-grid">
        {steps.map(([l, v, sub]) => <div key={l} className="funnel-step"><span className="l">{l}</span><b className="num">{v}</b><span className="tiny muted">{sub}</span></div>)}
      </div>
      {f.sources.length > 0 && (
        <div className="table-wrap"><table className="table">
          <thead><tr><th>Источник (utm_source)</th><th>Кампания</th><th className="r">Посетители</th><th className="r">Регистрации</th><th className="r">В листе</th><th className="r">Конверсия</th></tr></thead>
          <tbody>{f.sources.map((x, i) => <tr key={i}><td>{x.source || "прямые заходы"}</td><td className="muted">{x.campaign || "—"}</td><td className="r num">{x.visits}</td><td className="r num">{x.registered}</td><td className="r num">{x.waitlist}</td><td className="r num">{pct(x.registered, x.visits)}</td></tr>)}</tbody>
        </table></div>
      )}
      <div className="sheet-foot small muted">Ориентир по опыту: 3–10% посетителей регистрируются. Для рекламы добавляйте к ссылке ?utm_source=instagram&utm_campaign=nov — здесь появится разбивка.</div>
    </section>
  );
}

export function AdminSystem() {
  useTitle("Система");
  const { data, error, loading, reload } = useLoad(() => api.get<any>("/admin/system"), []);
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const d = data;
  const up = (s: number) => `${Math.floor(s / 86400)} д ${Math.floor((s % 86400) / 3600)} ч ${Math.floor((s % 3600) / 60)} мин`;
  return (
    <div className="stack g16">
      <div className="page-head" style={{ marginBottom: 4 }}><h1>Система</h1><Button size="sm" onClick={reload}>Обновить</Button></div>
      {d.market_demo && <Notice kind="warn">Рынок в демо-режиме (MARKET=fixture). Для запуска с пользователями уберите эту настройку.</Notice>}
      {d.payments === "sandbox" && <Notice kind="info">Платежи в тестовом режиме: деньги не списываются. Для приёма платежей подключите bePaid (см. README).</Notice>}
      <div className="grid2">
        <section className="sheet"><div className="sheet-head"><h2>Конфигурация</h2></div><div className="sheet-body"><dl className="kv">
          <dt>Версия</dt><dd>{d.version}</dd>
          <dt>Работает</dt><dd>{up(d.uptime_sec)}</dd>
          <dt>Адрес</dt><dd>{d.public_url}</dd>
          <dt>Платежи</dt><dd>{d.payments}{d.payments_test ? " (тест)" : " (боевой)"}</dd>
          <dt>Исполнение</dt><dd>{d.broker === "paper" ? "учебный счёт" : d.broker}</dd>
          <dt>Telegram-бот</dt><dd>{d.telegram ? "подключён" : "нет"}</dd>
          <dt>Почта (SMTP)</dt><dd>{d.smtp ? "настроена" : "письма пишутся в лог"}</dd>
          <dt>Обязательная верификация</dt><dd>{d.require_kyc ? "да" : "нет"}</dd>
          <dt>База данных</dt><dd>{(d.db_bytes / 1024 / 1024).toFixed(1)} МБ</dd>
          <dt>Память</dt><dd>{d.mem_mb} МБ · {d.goroutines} горутин</dd>
        </dl></div></section>
        <section className="sheet"><div className="sheet-head"><h2>Источники данных</h2></div>
          <div className="list">{d.sources.map((s: any) => (
            <div key={s.name} className="list-row"><span className="dot" style={{ background: s.ok ? "var(--up)" : s.warn ? "var(--warn)" : s.error_count ? "var(--down)" : "var(--faint)" }} /><div className="grow"><div className="title">{SOURCE_NAMES[s.name] || s.name}</div><div className="sub">{s.ok ? `Работает, обновлено ${ago(Math.floor(new Date(s.last_ok).getTime() / 1000))}` : s.warn ? <>{s.last_error}. {s.note}</> : s.last_error || "не использовался"}</div></div><span className="small muted">{s.error_count ? `сбоев: ${s.error_count}` : ""}</span></div>
          ))}</div>
          {d.rates?.usd > 0 && <div className="sheet-foot small muted">Курс НБРБ на {d.rates.date}: USD {d.rates.usd}</div>}
        </section>
      </div>
    </div>
  );
}
