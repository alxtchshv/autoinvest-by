import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { NavLink, useNavigate, useParams, useSearchParams } from "react-router-dom";
import QRCode from "qrcode";
import { Check, Copy, Download, Fingerprint, LogOut, Mail, Monitor, Smartphone } from "lucide-react";
import { api } from "../../lib/api";
import { ago, byn, date, dateTime, phoneFmt } from "../../lib/format";
import { useLoad, useSubmit, useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { AuditEvent, Consent, KYC, Me, Payment, Session } from "../../lib/types";
import { Button, Field, Input, LoadError, Modal, Notice, PageLoader, Switch, useToast } from "../../components/ui";
import { DatePicker, isoDate } from "../../components/DatePicker";
import { VerificationPanel } from "./Verify";
import { useConfirm } from "../../components/Confirm";
import { useDemoSwitch } from "../../components/Waitlist";
import { addPasskey, deviceName, passkeyError, passkeysSupported } from "../../lib/webauthn";
import { AppleMark, GoogleMark } from "../../components/SocialLogin";
import { LogoutButton } from "../../components/layout";

const TABS = [
  ["personal", "Личные данные"], ["verification", "Верификация"], ["security", "Безопасность"],
  ["notifications", "Уведомления"], ["billing", "Тариф"], ["referral", "Пригласить друга"], ["risk", "Риск-профиль"],
] as const;

export default function Profile() {
  const { tab = "personal" } = useParams();
  const { me, config } = useSession();
  const title = TABS.find((t) => t[0] === tab)?.[1] || "Профиль";
  useTitle(title);
  // до запуска проверка личности не нужна — вкладку не показываем; на демо-счёте настройки живут в основном аккаунте
  const tabs = TABS.filter(([k]) => !(k === "verification" && config?.launch === "waitlist"));
  if (me?.demo) return <DemoProfile />;
  return (
    <div>
      <div className="page-head"><h1>Профиль</h1><LogoutButton /></div>
      <div className="profile-layout">
        <nav className="profile-nav" aria-label="Разделы профиля">
          {tabs.map(([k, l]) => <NavLink key={k} to={`/app/profile/${k}`}>{l}{k === "verification" && <KycBadge />}</NavLink>)}
        </nav>
        <div key={tab} className="stack g16 tab-anim">
          {tab === "personal" && <Personal />}
          {tab === "verification" && <Verification />}
          {tab === "security" && <Security />}
          {tab === "password" && <PasswordTab />}
          {tab === "notifications" && <NotifySettings />}
          {tab === "billing" && <Billing />}
          {tab === "referral" && <Referral />}
          {tab === "risk" && <RiskQuiz />}
        </div>
      </div>
    </div>
  );
}

function DemoProfile() {
  const demo = useDemoSwitch();
  return (
    <div>
      <div className="page-head"><h1>Профиль</h1></div>
      <section className="sheet sheet-pad stack g12" style={{ maxWidth: 640 }}>
        <h2 style={{ margin: 0 }}>Это демо-счёт</h2>
        <p className="muted" style={{ margin: 0 }}>Пароль, вход, Telegram и личные данные настраиваются в вашем основном аккаунте. Здесь — только виртуальные копилки.</p>
        <div><Button variant="primary" busy={demo.busy} onClick={demo.exit}>Вернуться в мой аккаунт</Button></div>
      </section>
    </div>
  );
}

function Personal() {
  const { me, setMe } = useSession();
  const toast = useToast();
  const u = me!.user!;
  const [name, setName] = useState(u.name);
  const s = useSubmit();
  const [delOpen, setDelOpen] = useState(false);
  return (
    <>
      <form className="sheet" onSubmit={async (e) => { e.preventDefault(); try { const r = await s.submit(() => api.patch<Me>("/me", { name })); if (r) { setMe(r); toast("Сохранено"); } } catch { /* */ } }}>
        <div className="sheet-head"><h2>Личные данные</h2></div>
        <div className="sheet-body stack g16" style={{ maxWidth: 520 }}>
          <Field label="Имя" error={s.fieldError("name")}><Input value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <Field label="Почта" hint={u.email_verified ? "Подтверждена" : undefined}>
            <Input value={u.email} disabled />
          </Field>
          {!u.email_verified && <ResendVerification />}
          <div><Button type="submit" variant="primary" busy={s.busy}>Сохранить</Button></div>
        </div>
      </form>
      <Journal />
      <section className="sheet">
        <div className="sheet-head"><h2>Аккаунт</h2></div>
        <div className="sheet-body stack g12 small">
          <div className="row between"><span className="muted">Зарегистрирован</span><span>{date(u.created_at)}</span></div>
          <div className="row between"><span className="muted">Номер клиента</span><span className="num">{String(u.id).padStart(6, "0")}</span></div>
          <div className="divider" />
          <div className="row between wrap g12">
            <span className="muted">Удалить аккаунт и персональные данные. Сначала выведите деньги из копилок.</span>
            <Button variant="danger" size="sm" onClick={() => setDelOpen(true)}>Удалить аккаунт</Button>
          </div>
        </div>
      </section>
      <DeleteAccount open={delOpen} onClose={() => setDelOpen(false)} />
    </>
  );
}

function ResendVerification() {
  const toast = useToast();
  const s = useSubmit();
  return (
    <Notice kind="warn" title="Почта не подтверждена">
      Мы отправили письмо со ссылкой при регистрации.{" "}
      <button className="link-btn" disabled={s.busy} onClick={async () => { try { await s.submit(() => api.post("/auth/resend-verification")); toast("Письмо отправлено"); } catch (e: any) { toast(e.message, "error"); } }}>Отправить ещё раз</button>
    </Notice>
  );
}

function DeleteAccount({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { reload } = useSession();
  const nav = useNavigate();
  const conf = useConfirm();
  const s = useSubmit();
  return (
    <Modal open={open} onClose={onClose} title="Удалить аккаунт?">
      <form className="stack g16" onSubmit={async (e) => { e.preventDefault(); try { await s.submit(() => api.post("/me/delete", conf.body)); await reload(); nav("/"); } catch { /* */ } }}>
        <p>Копилки будут закрыты, карты отвязаны, персональные данные удалены. История операций сохранится обезличенно, как требует закон. Действие нельзя отменить.</p>
        {conf.view(s.fieldError)}
        {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
        <div className="row g8" style={{ justifyContent: "flex-end" }}><Button onClick={onClose}>Отмена</Button><Button type="submit" variant="danger" busy={s.busy}>Удалить навсегда</Button></div>
      </form>
    </Modal>
  );
}

// ---------------------------------------------------------------- верификация

function Verification() {
  return <VerificationPanel manualForm={(initial, done) => <KYCForm initial={initial} onDone={done} />} />;
}

function KYCForm({ initial, onDone }: { initial: KYC | null; onDone: () => void }) {
  const toast = useToast();
  const [f, setF] = useState({
    last_name: initial?.last_name || "", first_name: initial?.first_name || "", middle_name: initial?.middle_name || "",
    birth_date: initial?.birth_date || "", personal_number: initial?.personal_number || "", doc_number: initial?.doc_number || "",
    doc_issued_at: initial?.doc_issued_at || "", address: initial?.address || "",
  });
  const [doc, setDoc] = useState<File | null>(null);
  const [selfie, setSelfie] = useState<File | null>(null);
  const s = useSubmit();
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) => setF({ ...f, [k]: e.target.value });
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const fd = new FormData();
    Object.entries(f).forEach(([k, v]) => fd.append(k, v));
    if (doc) fd.append("doc_file", doc);
    if (selfie) fd.append("selfie_file", selfie);
    try { await s.submit(() => api.post("/kyc", fd)); toast("Документы отправлены на проверку"); onDone(); } catch { /* */ }
  };
  return (
    <form className="sheet" onSubmit={submit}>
      <div className="sheet-head"><h2>Верификация личности</h2></div>
      <div className="sheet-body stack g16" style={{ maxWidth: 640 }}>
        {initial?.status === "rejected" && <Notice kind="error" title="Верификация не пройдена">{initial.reject_reason}</Notice>}
        <p className="small muted">По требованиям законодательства о предотвращении легализации доходов операции с токенами доступны после идентификации. Данные видит только сотрудник проверки и передаются оператору криптоплатформы.</p>
        {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
        <div className="grid3">
          <Field label="Фамилия" error={s.fieldError("last_name")}><Input value={f.last_name} onChange={set("last_name")} /></Field>
          <Field label="Имя" error={s.fieldError("first_name")}><Input value={f.first_name} onChange={set("first_name")} /></Field>
          <Field label="Отчество"><Input value={f.middle_name} onChange={set("middle_name")} /></Field>
        </div>
        <div className="grid2">
          <Field label="Дата рождения" error={s.fieldError("birth_date")}><Input type="date" value={f.birth_date} onChange={set("birth_date")} /></Field>
          <Field label="Серия и номер паспорта" error={s.fieldError("doc_number")}><Input value={f.doc_number} onChange={(e) => setF({ ...f, doc_number: e.target.value.toUpperCase() })} placeholder="MP1234567" /></Field>
        </div>
        <div className="grid2">
          <Field label="Идентификационный номер" error={s.fieldError("personal_number")} hint="14 символов, на странице паспорта с фото"><Input value={f.personal_number} onChange={(e) => setF({ ...f, personal_number: e.target.value.toUpperCase() })} maxLength={14} placeholder="3010195A001PB7" /></Field>
          <Field label="Дата выдачи"><Input type="date" value={f.doc_issued_at} onChange={set("doc_issued_at")} /></Field>
        </div>
        <Field label="Адрес регистрации"><Input value={f.address} onChange={set("address")} placeholder="г. Минск, ул. …" /></Field>
        <div className="grid2">
          <Field label="Фото разворота паспорта" error={s.fieldError("doc_file")} hint={initial?.has_doc ? "Загружено. Можно заменить" : "JPG, PNG или PDF до 8 МБ"}>
            <input type="file" accept="image/jpeg,image/png,application/pdf" onChange={(e) => setDoc(e.target.files?.[0] || null)} />
          </Field>
          <Field label="Селфи с паспортом" error={s.fieldError("selfie_file")} hint="Необязательно, ускоряет проверку">
            <input type="file" accept="image/jpeg,image/png" onChange={(e) => setSelfie(e.target.files?.[0] || null)} />
          </Field>
        </div>
        <div><Button type="submit" variant="primary" busy={s.busy}>Отправить на проверку</Button></div>
      </div>
    </form>
  );
}

// ---------------------------------------------------------------- безопасность

function Security() {
  const { me, reload } = useSession();
  const toast = useToast();
  const sessions = useLoad(() => api.get<{ sessions: Session[] }>("/me/sessions"), []);
  const logins = useLoad(() => api.get<{ events: AuditEvent[] }>("/me/logins"), []);
  const consents = useLoad(() => api.get<{ consents: Consent[]; current_version: string }>("/me/consents"), []);
  const { setMe } = useSession();
  const nav = useNavigate();
  const [allOut, setAllOut] = useState(false);
  const [endS, setEndS] = useState<Session | null>(null);
  const [gone, setGone] = useState<string[]>([]);
  const [tfa, setTfa] = useState<null | { secret: string; url: string; qr: string }>(null);
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const [disable, setDisable] = useState(false);
  const tfaS = useSubmit();
  const u = me!.user!;
  const startTfa = async () => {
    const r = await api.post<{ secret: string; url: string }>("/me/2fa/setup");
    setTfa({ ...r, qr: await QRCode.toDataURL(r.url, { margin: 1, width: 320 }) });
  };
  const ACTIONS: Record<string, string> = {
    "auth.login": "Вход", "auth.login_failed": "Неудачная попытка входа", "auth.register": "Регистрация", "security.password_changed": "Смена пароля",
    "security.2fa_enabled": "Включена 2FA", "security.2fa_disabled": "Отключена 2FA", "card.added": "Привязана карта", "card.removed": "Удалена карта",
    "auth.password_reset": "Восстановление пароля", "security.sessions_revoked": "Завершены сеансы", "security.session_revoked": "Завершён сеанс",
    "plan.created": "Создана копилка", "plan.withdraw": "Вывод", "plan.closed": "Закрыта копилка", "telegram.link": "Подключён Telegram", "kyc.submitted": "Отправлены документы",
    "auth.logout": "Выход", "security.sessions_revoked_all": "Выход на всех устройствах", "card.default": "Основная карта изменена", "plan.updated": "Изменена копилка",
    "plan.paused": "Копилка на паузе", "plan.resumed": "Копилка включена", "kyc.review": "Проверка документов", "statement.export": "Выгружена выписка",
    "privacy.data_export": "Выгружены персональные данные", "profile.onboarding": "Подобран план", "profile.risk": "Пройдена анкета", "billing.pro": "Оплачен Pro",
    "auth.email_verified": "Подтверждена почта", "auth.recovery_code_used": "Использован код восстановления", "telegram.unlink": "Отключён Telegram",
  };
  const CONSENT: Record<string, [string, string]> = {
    offer: ["Публичная оферта", "/legal/offer"], risks: ["Уведомление о рисках", "/legal/risks"], privacy: ["Обработка персональных данных", "/legal/privacy"],
  };
  return (
    <>
      <LoginMethods />

      <section className="sheet">
        <div className="sheet-head"><h2>Код из Google Authenticator</h2>{u.totp_enabled ? <span className="badge badge-up">Включён</span> : <span className="badge">Выключен</span>}</div>
        <div className="sheet-body stack g16">
          {codes ? (
            <>
              <Notice kind="ok" title="Защита включена">Сохраните коды восстановления — каждый можно использовать один раз, если телефон потеряется.</Notice>
              <div className="codes">{codes.map((c) => <span key={c}>{c}</span>)}</div>
              <div><Button onClick={() => { navigator.clipboard?.writeText(codes.join("\n")); toast("Коды скопированы"); }} icon={<Copy />}>Скопировать</Button> <Button variant="ghost" onClick={() => setCodes(null)}>Готово</Button></div>
            </>
          ) : u.totp_enabled ? (
            <div className="row between wrap g12"><span className="muted">Спрашиваем код при входе и перед выводом денег, закрытием копилки и сменой пароля.</span><Button variant="danger" size="sm" onClick={() => setDisable(true)}>Отключить</Button></div>
          ) : tfa ? (
            <div className="row g24 wrap" style={{ alignItems: "flex-start" }}>
              <img className="qr" src={tfa.qr} alt="QR-код для приложения-аутентификатора" />
              <form className="stack g12 grow" style={{ minWidth: 240 }} onSubmit={async (e) => { e.preventDefault(); try { const r = await tfaS.submit(() => api.post<{ recovery_codes: string[] }>("/me/2fa/enable", { code })); if (r) { setCodes(r.recovery_codes); setTfa(null); setCode(""); reload(); } } catch { /* */ } }}>
                <ol className="small" style={{ margin: 0, paddingLeft: 18, lineHeight: 1.7 }}>
                  <li>Откройте Google Authenticator, Яндекс Ключ или 1Password</li>
                  <li>Отсканируйте QR-код или введите ключ <code className="num" style={{ wordBreak: "break-all" }}>{tfa.secret}</code></li>
                  <li>Введите 6 цифр из приложения</li>
                </ol>
                <Field error={tfaS.fieldError("code")}><Input inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} placeholder="123456" style={{ maxWidth: 180 }} /></Field>
                <div className="row g8"><Button type="submit" variant="primary" busy={tfaS.busy}>Включить</Button><Button variant="ghost" onClick={() => setTfa(null)}>Отмена</Button></div>
              </form>
            </div>
          ) : (
            <div className="row between wrap g12"><span className="muted">Даже если пароль узнают, без телефона в аккаунт не войти.</span><Button variant="primary" size="sm" onClick={startTfa}>Включить</Button></div>
          )}
        </div>
      </section>

      <PasskeysSection />

      <section className="sheet">
        <div className="sheet-head">
          <h2>Активные сеансы</h2>
          <div className="row g8 wrap">
            {(sessions.data?.sessions.length || 0) > 1 && <Button size="sm" onClick={async () => { await api.post("/me/sessions/revoke-others"); sessions.reload(); toast("Другие сеансы завершены"); }}>Завершить остальные</Button>}
            <Button size="sm" variant="danger" onClick={() => setAllOut(true)}>Выйти на всех устройствах</Button>
          </div>
        </div>
        <div className="list">
          {sessions.data?.sessions.map((x) => (
            <div key={x.id} className={`list-row sess-row ${gone.includes(x.id) ? "leaving" : ""}`}>
              <span className="sess-ic">{/iPhone|Android|iPad|телефон/i.test(x.user_agent) ? <Smartphone /> : <Monitor />}</span>
              <div className="grow"><div className="title">{x.user_agent}{x.current && <span className="badge badge-up" style={{ marginLeft: 8 }}>Это устройство</span>}</div><div className="sub">IP {x.ip} · активен {ago(x.last_seen_at)}</div></div>
              {!x.current && <Button size="sm" icon={<LogOut />} onClick={() => setEndS(x)}>Завершить</Button>}
            </div>
          ))}
        </div>
      </section>

      <section className="sheet">
        <div className="sheet-head"><h2>История входов</h2><span className="small muted">входы, неудачные попытки и выходы</span></div>
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th>Событие</th><th>Устройство</th><th>IP-адрес</th><th className="r">Когда</th></tr></thead>
            <tbody>
              {logins.data?.events.slice(0, 30).map((e, i) => (
                <tr key={i} className={e.action === "auth.login_failed" ? "warn-row" : ""}><td>{ACTIONS[e.action] || e.action}</td><td className="small">{e.user_agent || "—"}</td><td className="muted small num">{e.ip || "—"}</td><td className="r small muted nowrap">{dateTime(e.created_at)}</td></tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="sheet-foot small muted">Не узнаёте вход? Смените пароль и нажмите «Выйти на всех устройствах».</div>
      </section>

      <section className="sheet">
        <div className="sheet-head"><h2>Мои данные и согласия</h2></div>
        <div className="sheet-body stack g12">
          <div className="list">
            {consents.data?.consents.map((c, i) => (
              <div key={i} className="list-row">
                <div className="grow"><div className="title"><a href={CONSENT[c.kind]?.[1]} target="_blank" rel="noreferrer">{CONSENT[c.kind]?.[0] || c.kind}</a></div><div className="sub">редакция {c.version.split("-").reverse().join(".")} · принято {dateTime(c.accepted_at)}{c.ip ? ` · IP ${c.ip}` : ""}</div></div>
                {c.revoked_at ? <span className="badge">Отозвано</span> : <span className="badge badge-up">Действует</span>}
              </div>
            ))}
            {consents.data && consents.data.consents.length === 0 && <p className="small muted">Аккаунт создан до появления раздельных согласий — условия приняты при регистрации.</p>}
          </div>
          <div className="row between wrap g12">
            <span className="small muted">По Закону № 99-З вы вправе получить все свои данные. Отозвать согласие можно, удалив аккаунт во вкладке «Личные данные».</span>
            <a className="btn btn-secondary btn-sm" href="/api/v1/me/export" download>Скачать мои данные</a>
          </div>
        </div>
      </section>

      <Modal open={allOut} onClose={() => setAllOut(false)} title="Выйти на всех устройствах?">
        <p className="muted">Все сеансы, включая этот, будут завершены. Войти снова можно с паролем{u.totp_enabled ? " и кодом из приложения" : ""}.</p>
        <div className="row g8 mt16" style={{ justifyContent: "flex-end" }}>
          <Button variant="ghost" onClick={() => setAllOut(false)}>Отмена</Button>
          <Button variant="danger" onClick={async () => { await api.post("/me/sessions/revoke-all"); setMe({ user: null }); nav("/login", { replace: true }); }}>Выйти везде</Button>
        </div>
      </Modal>

      <Modal open={!!endS} onClose={() => setEndS(null)} title="Завершить сеанс?">
        <p className="muted">{endS?.user_agent}, IP {endS?.ip}. На этом устройстве нужно будет войти заново.</p>
        <div className="row g8 mt16" style={{ justifyContent: "flex-end" }}>
          <Button variant="ghost" onClick={() => setEndS(null)}>Отмена</Button>
          <Button variant="danger" onClick={async () => {
            const x = endS!;
            setEndS(null);
            try {
              await api.del(`/me/sessions/${x.id}`);
              setGone((g) => [...g, x.id]);
              toast("Сеанс завершён");
              setTimeout(() => { sessions.reload(); setGone((g) => g.filter((id) => id !== x.id)); }, 320);
            } catch (e: any) { toast(e.message, "error"); }
          }}>Завершить</Button>
        </div>
      </Modal>

      <Disable2FA open={disable} onClose={() => setDisable(false)} onDone={() => { setDisable(false); reload(); toast("Двухфакторная защита отключена"); }} />
    </>
  );
}

function PasswordTab() {
  return <section className="sheet"><div className="sheet-head"><h2>Пароль</h2></div><div className="sheet-body"><PasswordForm /></div></section>;
}

function PasswordForm({ onDone }: { onDone?: () => void }) {
  const toast = useToast();
  const { me, reload } = useSession();
  const hasPw = me?.user?.has_password !== false;
  const [pw, setPw] = useState({ current: "", next: "", again: "", code: "" });
  const totp = !!me?.user?.totp_enabled;
  const s = useSubmit();
  const mismatch = !!pw.again && pw.next !== pw.again;
  return (
    <form className="stack g16" onSubmit={async (e) => {
      e.preventDefault();
      if (pw.next !== pw.again) return;
      try {
        await s.submit(() => api.post("/me/password", { current: pw.current, new: pw.next, code: pw.code }));
        setPw({ current: "", next: "", again: "", code: "" });
        toast(hasPw ? "Пароль изменён, другие устройства вышли из аккаунта" : "Пароль задан — теперь можно входить и по почте");
        reload();
        onDone?.();
      } catch { /* */ }
    }}>
      {!hasPw && <Notice kind="info">Вы входите через Google или по телефону. Пароль нужен для вывода денег и как запасной способ входа.</Notice>}
      {/* скрытый логин — чтобы менеджер паролей понял, для какого аккаунта сохранить новый пароль */}
      <input type="text" name="username" autoComplete="username" value={me?.user?.email || ""} readOnly hidden />
      {hasPw && <Field label="Текущий пароль" error={s.fieldError("current")}><Input name="current-password" type="password" autoComplete="current-password" value={pw.current} onChange={(e) => setPw({ ...pw, current: e.target.value })} /></Field>}
      <Field label="Новый пароль" error={s.fieldError("new")} hint="Не короче 8 символов, буквы и цифры"><Input name="new-password" type="password" autoComplete="new-password" value={pw.next} onChange={(e) => setPw({ ...pw, next: e.target.value })} /></Field>
      <Field label="Новый пароль ещё раз" error={mismatch ? "Пароли не совпадают" : undefined}><Input name="confirm-password" type="password" autoComplete="new-password" value={pw.again} onChange={(e) => setPw({ ...pw, again: e.target.value })} /></Field>
      {totp && <Field label="Код из Google Authenticator" error={s.fieldError("code")}><Input inputMode="numeric" autoComplete="one-time-code" maxLength={12} value={pw.code} onChange={(e) => setPw({ ...pw, code: e.target.value.replace(/\s/g, "") })} /></Field>}
      {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
      {hasPw && <p className="small muted" style={{ margin: 0 }}>После смены пароля все другие устройства выйдут из аккаунта.</p>}
      <div className="row g8" style={{ justifyContent: "flex-end" }}><Button type="submit" variant="primary" busy={s.busy} disabled={(hasPw && !pw.current) || !pw.next || pw.next !== pw.again}>{hasPw ? "Изменить пароль" : "Задать пароль"}</Button></div>
    </form>
  );
}

// Способы входа: почта и пароль, телефон с кодом, Google, Apple.
type Ident = { provider: string; email: string; created_at: number };
type IdentResp = { identities: Ident[]; has_password: boolean; phone: string; phone_verified: boolean; providers: { google: string; apple: string; phone: string } };

function LoginMethods() {
  const toast = useToast();
  const { reload: reloadMe } = useSession();
  const [sp, setSp] = useSearchParams();
  const d = useLoad(() => api.get<IdentResp>("/me/identities"), []);
  useEffect(() => { const f = () => d.reload(); window.addEventListener("focus", f); return () => window.removeEventListener("focus", f); }, [d]);
  const [pwOpen, setPwOpen] = useState(false);
  const { me } = useSession();
  const nav = useNavigate();
  // номер подтверждается кнопкой «Поделиться номером» в нашем боте — без SMS
  const askPhone = async () => {
    try {
      const r = await api.post<{ open: string }>("/me/phone/telegram");
      toast("Бот прислал кнопку «Поделиться номером»");
      if (r.open.startsWith("/")) nav(r.open); else window.open(r.open, "_blank", "noopener");
    } catch (e: any) { toast(e.message, "error"); }
  };
  useEffect(() => {
    const ok = sp.get("oauth"), bad = sp.get("oauth_error");
    if (ok === "linked") toast("Способ входа привязан");
    if (bad) toast(bad === "taken" ? "Этот аккаунт Google/Apple уже привязан к другому пользователю" : bad === "cancel" ? "Привязка отменена" : "Не удалось привязать, попробуйте ещё раз", bad === "cancel" ? "ok" : "error");
    if (ok || bad) setSp({}, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  if (!d.data) return null;
  const x = d.data;
  const has = (p: string) => x.identities.find((i) => i.provider === p);
  const unlink = async (p: string) => {
    try { await api.del(`/me/identities/${p}`); d.reload(); toast("Отвязано"); } catch (e: any) { toast(e.message, "error"); }
  };
  const Row = ({ icon, title, sub, action }: { icon: ReactNode; title: string; sub: string; action: ReactNode }) => (
    <div className="list-row lm-row"><span className="lm-ic">{icon}</span><div className="grow"><div className="title">{title}</div><div className="sub">{sub}</div></div>{action}</div>
  );
  return (
    <section className="sheet">
      <div className="sheet-head"><h2>Способы входа</h2></div>
      <div className="list">
        <Row icon={<Mail />} title="Почта и пароль" sub={x.has_password ? "Пароль задан" : "Пароль не задан"} action={<Button size="sm" onClick={() => setPwOpen(true)}>{x.has_password ? "Изменить" : "Задать"}</Button>} />
        {x.providers.phone !== "off" && (
          <Row icon={<Smartphone />} title="Телефон и код в Telegram"
            sub={x.phone_verified ? `${phoneFmt(x.phone)} · коды для входа приходят в Telegram` : me?.telegram_linked ? "Подтвердите номер кнопкой в боте" : "Сначала подключите Telegram — коды будут приходить туда"}
            action={x.phone_verified
              ? <Button size="sm" variant="ghost" onClick={async () => { try { await api.del("/me/phone"); d.reload(); reloadMe(); toast("Номер отвязан"); } catch (e: any) { toast(e.message, "error"); } }}>Отвязать</Button>
              : me?.telegram_linked
                ? <Button size="sm" variant="primary" onClick={askPhone}>Подтвердить</Button>
                : <Button size="sm" onClick={() => nav("/app/profile/notifications")}>Подключить Telegram</Button>} />
        )}
        {(["google", "apple"] as const).filter((p) => x.providers[p] !== "off").map((p) => {
          const i = has(p);
          return (
            <Row key={p} icon={p === "google" ? <GoogleMark /> : <AppleMark />} title={p === "google" ? "Google" : "Apple ID"} sub={i ? (i.email || "Привязан") : "Не привязан"}
              action={i ? <Button size="sm" variant="ghost" onClick={() => unlink(p)}>Отвязать</Button> : <a className="btn btn-secondary btn-sm" href={`/oauth/${p}/start?mode=link`}>Привязать</a>} />
          );
        })}
      </div>
      <Modal open={pwOpen} onClose={() => setPwOpen(false)} title={x.has_password ? "Изменить пароль" : "Задать пароль"}>
        <PasswordForm onDone={() => { setPwOpen(false); d.reload(); }} />
      </Modal>
    </section>
  );
}


// Журнал действий не показываем списком: выгружаем файлом, только когда пользователь сам попросит.
function Journal() {
  const [period, setPeriod] = useState({ from: "", to: "" });
  const today = isoDate(new Date());
  const qs = new URLSearchParams(Object.entries(period).filter(([, v]) => v)).toString();
  return (
    <section className="sheet">
      <div className="sheet-head"><h2>Журнал действий</h2></div>
      <div className="sheet-body stack g16">
        <p style={{ margin: 0 }}>Всё, что происходило в аккаунте: входы, смена пароля, карты, копилки, выводы и выгрузки — с датой, IP-адресом и устройством. Файл открывается в Excel.</p>
        <div className="row wrap g12" style={{ alignItems: "flex-end" }}>
          <div className="stack g4 small export-date"><span className="muted">С</span><DatePicker value={period.from} min="2020-01-01" max={period.to || today} onChange={(v) => setPeriod({ ...period, from: v })} placeholder="С регистрации" /></div>
          <div className="stack g4 small export-date"><span className="muted">По</span><DatePicker value={period.to} min={period.from || "2020-01-01"} max={today} onChange={(v) => setPeriod({ ...period, to: v })} placeholder="По сегодня" /></div>
          <a className="btn btn-primary" href={`/api/v1/me/audit/export.csv${qs ? "?" + qs : ""}`} download><Download />Скачать журнал</a>
        </div>
      </div>
      <div className="sheet-foot small muted">Скачивание тоже попадает в журнал. Не узнаёте какое-то действие — смените пароль и завершите чужие сеансы во вкладке «Безопасность».</div>
    </section>
  );
}

// Face ID / Touch ID / отпечаток (WebAuthn): второй шаг входа и подтверждение опасных действий одним касанием.
function PasskeysSection() {
  const toast = useToast();
  const { reload: reloadMe } = useSession();
  const d = useLoad(() => api.get<{ passkeys: { id: number; name: string; created_at: number; last_used_at: number | null }[] }>("/me/passkeys"), []);
  const [busy, setBusy] = useState(false);
  const supported = passkeysSupported();
  const add = async () => {
    setBusy(true);
    try { await addPasskey(deviceName()); d.reload(); reloadMe(); toast("Устройство добавлено — теперь можно подтверждать Face ID или отпечатком"); }
    catch (e: any) { toast(passkeyError(e), "error"); }
    finally { setBusy(false); }
  };
  const list = d.data?.passkeys || [];
  return (
    <section className="sheet">
      <div className="sheet-head"><h2>Face ID или отпечаток вместо кода</h2>{list.length ? <span className="badge badge-up">Включено</span> : <span className="badge">Выключено</span>}</div>
      <div className="sheet-body stack g12">
        <span className="muted">После логина и пароля — одно касание вместо кода из Telegram или письма; перед выводом денег — вместо пароля. Так мы уверены, что это именно вы. Биометрия остаётся на устройстве — мы храним только ключ.</span>
        {list.length > 0 && (
          <div className="list pk-list">
            {list.map((k) => (
              <div key={k.id} className="list-row">
                <span className="sess-ic"><Fingerprint /></span>
                <div className="grow"><div className="title">{k.name}</div><div className="sub">добавлено {date(k.created_at)}{k.last_used_at ? ` · использовано ${ago(k.last_used_at)}` : ""}</div></div>
                <Button size="sm" variant="ghost" onClick={async () => { await api.del(`/me/passkeys/${k.id}`); d.reload(); reloadMe(); toast("Устройство удалено"); }}>Удалить</Button>
              </div>
            ))}
          </div>
        )}
        {supported
          ? <div><Button variant={list.length ? "secondary" : "primary"} size="sm" icon={<Fingerprint />} busy={busy} onClick={add}>{list.length ? "Добавить ещё устройство" : "Включить на этом устройстве"}</Button></div>
          : <Notice kind="info">Этот браузер не поддерживает вход по Face ID и отпечатку. Используйте Google Authenticator.</Notice>}
      </div>
    </section>
  );
}

function Disable2FA({ open, onClose, onDone }: { open: boolean; onClose: () => void; onDone: () => void }) {
  const [p, setP] = useState("");
  const [c, setC] = useState("");
  const s = useSubmit();
  return (
    <Modal open={open} onClose={onClose} title="Отключить двухфакторную защиту">
      <form className="stack g16" onSubmit={async (e) => { e.preventDefault(); try { await s.submit(() => api.post("/me/2fa/disable", { password: p, code: c })); onDone(); } catch { /* */ } }}>
        <Field label="Пароль" error={s.fieldError("password")}><Input name="password" type="password" autoComplete="current-password" value={p} onChange={(e) => setP(e.target.value)} /></Field>
        <Field label="Код из приложения или код восстановления" error={s.fieldError("code")}><Input value={c} onChange={(e) => setC(e.target.value)} /></Field>
        <div className="row g8" style={{ justifyContent: "flex-end" }}><Button onClick={onClose}>Отмена</Button><Button type="submit" variant="danger" busy={s.busy}>Отключить</Button></div>
      </form>
    </Modal>
  );
}

// ---------------------------------------------------------------- уведомления

function NotifySettings() {
  const { me, setMe, config } = useSession();
  const toast = useToast();
  const u = me!.user!;
  // переключатель двигается сразу, запрос идёт следом; при ошибке — откат
  const upd = async (k: string, v: boolean) => {
    const prev = me!;
    setMe({ ...prev, user: { ...prev.user!, [k]: v } });
    try { setMe(await api.patch<Me>("/me/notifications", { [k]: v })); } catch (e: any) { setMe(prev); toast(e.message, "error"); }
  };
  const nav = useNavigate();
  const link = async () => {
    try {
      const r = await api.post<{ url: string; test?: string }>("/telegram/link");
      if (r.test) { nav(r.url); return; } // тестовый Telegram открывается прямо в кабинете
      window.open(r.url, "_blank", "noopener");
      toast("Откройте Telegram и нажмите «Старт»");
    } catch (e: any) { toast(e.message, "error"); }
  };
  return (
    <>
      <section className="sheet">
        <div className="sheet-head"><h2>Telegram</h2>{me?.telegram_linked ? <span className="badge badge-up">Подключён</span> : <span className="badge">Не подключён</span>}</div>
        <div className="sheet-body stack g12">
          {me?.telegram_linked ? (
            <div className="row between wrap g12">
              <span>Уведомления приходят в Telegram{u.telegram_username ? ` @${u.telegram_username}` : ""}. В боте: баланс, копилки, пауза, операции и поддержка.</span>
              <div className="row g8">
                {config?.telegram_test && <Button size="sm" variant="primary" onClick={() => nav("/telegram-sandbox")}>Открыть чат с ботом</Button>}
                <Button size="sm" onClick={async () => setMe(await api.del<Me>("/telegram/link"))}>Отключить</Button>
              </div>
            </div>
          ) : config?.telegram_bot ? (
            <div className="row between wrap g12">
              <span className="muted">Баланс, копилки, пауза одной кнопкой, чеки о пополнениях и ответы поддержки — в боте @{config.telegram_bot}.{config.telegram_test ? " Сейчас тестовый режим: бот откроется прямо здесь." : ""}</span>
              <Button variant="primary" size="sm" onClick={link}>Подключить Telegram</Button>
            </div>
          ) : (
            <span className="muted">Бот ещё не настроен администратором сервиса.</span>
          )}
        </div>
      </section>
      <section className="sheet">
        <div className="sheet-head"><h2>Что присылать</h2></div>
        <div className="sheet-body">
          <div className="setting-row"><div><div style={{ fontWeight: 560 }}>Письма на почту</div><div className="small muted">Отказы в списании, выводы, безопасность, ответы поддержки</div></div><Switch label="Письма" checked={u.notify_email} onChange={(v) => upd("notify_email", v)} /></div>
          <div className="setting-row"><div><div style={{ fontWeight: 560 }}>Сообщения в Telegram</div><div className="small muted">Все события по копилкам</div></div><Switch label="Telegram" checked={u.notify_telegram} onChange={(v) => upd("notify_telegram", v)} /></div>
          <div className="setting-row"><div><div style={{ fontWeight: 560 }}>Резкие движения рынка</div><div className="small muted">Когда копилка докупила на просадке</div></div><Switch label="Рынок" checked={u.notify_market} onChange={(v) => upd("notify_market", v)} /></div>
        </div>
      </section>
    </>
  );
}

// ---------------------------------------------------------------- тариф

function Billing() {
  const { config, reload: reloadMe } = useSession();
  const toast = useToast();
  const { data, error, loading, reload } = useLoad(() => api.get<{ tier: string; is_pro: boolean; pro_until: number | null; autorenew: boolean; period: string; month_kop: number; year_kop: number; payments: Payment[] }>("/billing"), []);
  const [period, setPeriod] = useState<"month" | "year">("month");
  const s = useSubmit();
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const d = data!;
  const buy = async (per: "month" | "year" = period) => {
    try {
      const r = await s.submit(() => api.post<{ code?: string; error?: string }>("/billing/pro", { period: per }));
      toast(r?.code === "pending" ? r.error! : "Pro подключён");
      reload(); reloadMe();
    } catch (e: any) { toast(e.message, "error"); }
  };
  return (
    <>
      <section className="sheet">
        <div className="sheet-head"><h2>Ваш тариф</h2>{d.is_pro ? <span className="badge badge-ink">Pro</span> : <span className="badge">Старт</span>}</div>
        <div className="sheet-body stack g16">
          {d.is_pro ? (
            <>
              <p>Pro действует до <b>{date(d.pro_until!)}</b>. {d.autorenew ? `Продлится автоматически: ${byn(d.period === "year" ? d.year_kop : d.month_kop)} с основной карты.` : "Автопродление выключено."}</p>
              <div className="row g8">
                {d.autorenew
                  ? <Button size="sm" onClick={async () => { await api.post("/billing/autorenew", { on: false }); reload(); toast("Автопродление отключено"); }}>Отключить автопродление</Button>
                  : <Button size="sm" variant="primary" onClick={async () => { await api.post("/billing/autorenew", { on: true }); reload(); }}>Включить автопродление</Button>}
              </div>
            </>
          ) : (
            <>
              <div className="tariff-cards">
                {(["month", "year"] as const).map((per) => {
                  const price = per === "year" ? d.year_kop : d.month_kop;
                  const save = Math.round((1 - d.year_kop / (d.month_kop * 12)) * 100);
                  return (
                    <div key={per} className={`tariff-card ${per === "year" ? "best" : ""}`}>
                      <div className="tc-head">
                        <span className="tc-name">Pro на {per === "year" ? "год" : "месяц"}</span>
                        {per === "year" && <span className="badge badge-up">−{save}%</span>}
                      </div>
                      <div className="tc-price"><b>{byn(price)}</b><span className="muted">{per === "year" ? ` · ${byn(Math.round(d.year_kop / 12))} в месяц` : " в месяц"}</span></div>
                      <ul className="tc-list">
                        <li><Check />До {config?.pro_plan_limit} копилок вместо {config?.free_plan_limit}</li>
                        <li><Check />Свой состав из любых активов</li>
                        <li><Check />Докупка при падении цены</li>
                        <li><Check />Комиссия {(config!.fee_pro_bp / 100).toString().replace(".", ",")}% вместо {(config!.fee_free_bp / 100).toString().replace(".", ",")}%</li>
                        {per === "year" && <li><Check />Платите раз в год — экономия {byn(d.month_kop * 12 - d.year_kop)}</li>}
                      </ul>
                      <Button variant={per === "year" ? "primary" : "secondary"} block busy={s.busy && period === per} onClick={() => { setPeriod(per); buy(per); }}>Подключить за {byn(price)}</Button>
                    </div>
                  );
                })}
              </div>
              <p className="tiny muted">Списание с основной карты. Отменить автопродление можно в любой момент в этом разделе.</p>
            </>
          )}
        </div>
      </section>
      {d.payments.length > 0 && (
        <section className="sheet">
          <div className="sheet-head"><h2>Платежи за подписку</h2></div>
          <div className="table-wrap"><table className="table"><tbody>
            {d.payments.map((p) => <tr key={p.id}><td>{dateTime(p.created_at)}</td><td>{p.card_last4 ? "•• " + p.card_last4 : ""}</td><td>{p.status === "successful" ? <span className="badge badge-up">Оплачено</span> : <span className="badge badge-down">{p.message || "Отклонено"}</span>}</td><td className="r num">{byn(p.amount_kop)}</td></tr>)}
          </tbody></table></div>
        </section>
      )}
    </>
  );
}

// ---------------------------------------------------------------- рефералка

function Referral() {
  const toast = useToast();
  const { data, error, loading, reload } = useLoad(() => api.get<{ code: string; link: string; stats: { invited: number; active: number; rewarded: number }; friends: { name: string; created_at: number; active: boolean }[] }>("/referral"), []);
  const [qr, setQr] = useState("");
  useEffect(() => { if (data) QRCode.toDataURL(data.link, { margin: 1, width: 320 }).then(setQr); }, [data]);
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const d = data!;
  const copy = () => { navigator.clipboard?.writeText(d.link); toast("Ссылка скопирована"); };
  return (
    <>
      <section className="sheet">
        <div className="sheet-head"><h2>Пригласить друга</h2></div>
        <div className="sheet-body row g24 wrap" style={{ alignItems: "flex-start" }}>
          <div className="stack g16 grow" style={{ minWidth: 260 }}>
            <p>Когда друг зарегистрируется по вашей ссылке и сделает первое пополнение, вы оба получите месяц Pro.</p>
            <div className="row g8"><Input readOnly value={d.link} onFocus={(e) => e.target.select()} aria-label="Ваша ссылка" /><Button icon={<Copy />} onClick={copy}>Копировать</Button></div>
            <div className="row g8 wrap">
              <a className="btn btn-secondary btn-sm" href={`https://t.me/share/url?url=${encodeURIComponent(d.link)}&text=${encodeURIComponent("Коплю в долларе и золоте через AutoInvest — попробуй, по моей ссылке месяц Pro бесплатно")}`} target="_blank" rel="noopener">Отправить в Telegram</a>
            </div>
          </div>
          {qr && <img className="qr" src={qr} alt="QR-код ссылки-приглашения" />}
        </div>
      </section>
      <section className="sheet">
        <div className="sheet-head"><h2>Приглашённые</h2><span className="small muted">{d.stats.invited} зарегистрировались · {d.stats.active} начали копить</span></div>
        {d.friends.length === 0 ? <div className="sheet-body small muted">Пока никого. Поделитесь ссылкой.</div> : (
          <div className="list">{d.friends.map((f, i) => <div key={i} className="list-row"><div className="grow"><div className="title">{f.name}</div><div className="sub">{date(f.created_at)}</div></div>{f.active ? <span className="badge badge-up">Копит</span> : <span className="badge">Зарегистрирован</span>}</div>)}</div>
        )}
      </section>
    </>
  );
}

// ---------------------------------------------------------------- риск-профиль

const QUIZ: [string, string[]][] = [
  ["На какой срок вы готовы отложить эти деньги?", ["До года", "1–3 года", "3–5 лет", "Больше 5 лет"]],
  ["Копилка подешевела на 30% за месяц. Что сделаете?", ["Выведу всё", "Поставлю на паузу", "Ничего не буду менять", "Пополню больше обычного"]],
  ["Какая доля ваших сбережений пойдёт в копилки?", ["Больше половины", "Около половины", "Около четверти", "Меньше 10%"]],
  ["Есть ли у вас запас на 3 месяца расходов?", ["Нет", "Почти", "Да", "Да, и больше"]],
  ["Что для вас важнее?", ["Не потерять", "Обогнать инфляцию", "Заметный рост", "Максимальный рост"]],
  ["Был ли у вас опыт с криптовалютой или акциями?", ["Нет", "Немного читал", "Покупал сам", "Регулярно инвестирую"]],
];
const PROFILE: Record<string, [string, string]> = {
  conservative: ["Осторожный", "Подходят планы «Доллар» и «Резерв» — риск не выше 2 из 5."],
  moderate: ["Умеренный", "Подходят планы до «Баланса» — риск до 3 из 5."],
  aggressive: ["Готовый к риску", "Подходят любые планы, включая «Рост» и «Максимум»."],
};

function RiskQuiz() {
  const { me, reload } = useSession();
  const [ans, setAns] = useState<number[]>(Array(6).fill(0));
  const [again, setAgain] = useState(false);
  const s = useSubmit();
  const current = me?.user?.risk_profile;
  if (current && !again) {
    const [t, d] = PROFILE[current];
    return (
      <section className="sheet">
        <div className="sheet-head"><h2>Ваш риск-профиль</h2></div>
        <div className="sheet-body stack g12"><div className="money-lg">{t}</div><p className="muted">{d}</p><div><Button size="sm" onClick={() => setAgain(true)}>Пройти заново</Button></div></div>
      </section>
    );
  }
  return (
    <section className="sheet">
      <div className="sheet-head"><h2>Анкета: какой риск вам подходит</h2><span className="small muted">6 вопросов</span></div>
      <div className="sheet-body stack g24">
        {QUIZ.map(([q, opts], i) => (
          <fieldset key={i} style={{ border: 0, padding: 0, margin: 0 }}>
            <legend style={{ fontWeight: 600, marginBottom: 8 }}>{q}</legend>
            <div className="row g8 wrap">{opts.map((o, j) => <button key={j} type="button" className="chip" aria-pressed={ans[i] === j + 1} onClick={() => setAns(ans.map((a, k) => (k === i ? j + 1 : a)))}>{o}</button>)}</div>
          </fieldset>
        ))}
        {s.error && <Notice kind="error">{s.error}</Notice>}
        <div><Button variant="primary" disabled={ans.includes(0)} busy={s.busy} onClick={async () => { try { await s.submit(() => api.post("/me/risk", { answers: ans })); await reload(); setAgain(false); } catch { /* */ } }}>Узнать профиль</Button></div>
      </div>
    </section>
  );
}

// KycBadge — статус верификации рядом с вкладкой: не пройдена → на проверке → подтверждена / отклонена.
function KycBadge() {
  const { me } = useSession();
  const st = me?.user?.kyc_status || "none";
  const [label, cls] = ({ none: ["не пройдена", ""], pending: ["на проверке", "badge-warn"], approved: ["подтверждена", "badge-up"], rejected: ["отклонена", "badge-down"] } as Record<string, [string, string]>)[st] || ["", ""];
  return label ? <span className={`badge ${cls}`} style={{ marginLeft: 8 }}>{label}</span> : null;
}
