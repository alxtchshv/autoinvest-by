import { useEffect, useRef, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../../lib/api";
import { useSubmit, useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { Me } from "../../lib/types";
import { Button, Field, Input, Notice, Segmented } from "../../components/ui";
import { OrLine, SocialButtons, SocialConsent } from "../../components/SocialLogin";
import { getUtm } from "../../lib/utm";
import { Fingerprint } from "lucide-react";
import { fullPhone, PhoneInput } from "../../components/PhoneInput";
import { deviceHasPasskey, passkeyError, passkeysSupported, usePasskey } from "../../lib/webauthn";

function safeNext(n: string | null) {
  return n && n.startsWith("/") && !n.startsWith("//") ? n : "/app";
}

function strength(p: string): [number, string] {
  let s = 0;
  if (p.length >= 8) s++;
  if (p.length >= 12) s++;
  if (/[a-zа-я]/.test(p) && /[A-ZА-Я]/.test(p)) s++;
  if (/\d/.test(p) && /[^\w\s]/.test(p)) s++;
  return [s, ["Слишком короткий", "Слабый", "Нормальный", "Хороший", "Надёжный"][s]];
}

/** Причина блокировки после входа через Google: сервер кладёт её в короткую cookie, читаем один раз. */
function blockedReason(): string | null {
  try {
    const m = document.cookie.match(/(?:^|; )ai_block=([^;]*)/);
    if (!m) return null;
    document.cookie = "ai_block=; Max-Age=0; Path=/";
    return decodeURIComponent(m[1].replace(/\+/g, " "));
  } catch {
    return null;
  }
}

const OAUTH_ERRORS: Record<string, string> = {
  cancel: "Вход отменён.",
  state: "Сеанс входа устарел. Попробуйте ещё раз.",
  provider: "Не удалось получить данные от Google или Apple. Попробуйте ещё раз или войдите по почте.",
  email: "Провайдер не передал подтверждённую почту, а аккаунт с ней уже есть. Войдите по паролю и привяжите Google или Apple в профиле.",
  blocked: "Аккаунт заблокирован. Напишите в поддержку: support@autoinvest.by",
  off: "Этот способ входа пока не подключён.",
  limit: "Слишком много попыток. Подождите 15 минут.",
};

export function Login() {
  useTitle("Вход");
  const [sp] = useSearchParams();
  const nav = useNavigate();
  const { setMe, me, config } = useSession();
  const [how, setHow] = useState<"password" | "phone">("password");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [phone, setPhone] = useState("");
  const [sent, setSent] = useState<null | { test_code?: string; via?: string }>(null);
  const [sms, setSms] = useState("");
  const [code, setCode] = useState("");
  const [mfa, setMfa] = useState(sp.get("step") === "2fa");
  const [mfaWays, setMfaWays] = useState<{ totp: boolean; passkey: boolean; telegram?: boolean; email?: boolean }>({ totp: true, passkey: false });
  const [mfaSent, setMfaSent] = useState<null | { via: string; test_code?: string; mail_failed?: boolean }>(null);
  const [keyBusy, setKeyBusy] = useState(false);
  const s = useSubmit();
  const phoneOn = !!config?.auth && config.auth.phone !== "off";
  // Face ID / отпечаток вместо кода: если ключ есть на этом устройстве, системное окно открывается само —
  // сразу после «Войти» или после возврата от Google. Не открылось (Safari просит касание) или отменили — остаётся кнопка.
  const autoKey = useRef(false);
  const runKey = async (auto: boolean) => {
    setKeyBusy(true); s.reset();
    try { setMe(await usePasskey("login")); } catch (e: any) { if (!auto) s.setError(passkeyError(e)); } finally { setKeyBusy(false); }
  };
  const maybeAutoKey = (ways?: { passkey: boolean }) => {
    if (autoKey.current || !ways?.passkey || !passkeysSupported() || !deviceHasPasskey()) return;
    autoKey.current = true;
    runKey(true);
  };
  useEffect(() => {
    if (me?.user) nav(safeNext(sp.get("next")), { replace: true });
    if (me?.mfa_required) { setMfa(true); if (me.mfa) { setMfaWays(me.mfa); maybeAutoKey(me.mfa); } }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [me, nav, sp]);
  // Chrome, Edge и Android предлагают сохранить пароль через Credential Management API; Safari и iOS — сами по отправке формы
  const savePassword = () => {
    try {
      const PC = (window as any).PasswordCredential;
      if (PC && how === "password" && email && password) navigator.credentials.store(new PC({ id: email, password, name: email }));
    } catch { /* не поддерживается — не страшно */ }
  };
  const finish = (r: Me | undefined) => {
    if (r && (r.user || r.mfa_required)) savePassword();
    if (r?.mfa_required) { setMfa(true); if (r.mfa) { setMfaWays(r.mfa); maybeAutoKey(r.mfa); } return; }
    if (r) setMe(r);
  };
  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      if (mfa) finish(await s.submit(() => api.post<Me>("/auth/login/2fa", { code })));
      else if (how === "password") finish(await s.submit(() => api.post<Me>("/auth/login", { email, password })));
      else if (!sent) setSent((await s.submit(() => api.post<{ test_code?: string; via?: string }>("/auth/phone/code", { phone: fullPhone(phone) }))) || null);
      else finish(await s.submit(() => api.post<Me>("/auth/phone/login", { phone: fullPhone(phone), code: sms })));
    } catch {
      /* ошибка уже в s.error */
    }
  };
  const oauthErr = sp.get("oauth_error");
  const [blockMsg] = useState(() => (oauthErr === "blocked" ? blockedReason() : null));
  return (
    <div className="auth-wrap">
      <form className="auth-card sheet sheet-pad stack g16" method="post" action="/login" autoComplete="on" onSubmit={onSubmit} noValidate>
        <div>
          <h1>{mfa ? "Подтвердите вход" : "Вход"}</h1>
          <p className="muted mt8">{mfa ? (mfaWays.totp ? "Введите 6 цифр из Google Authenticator или код восстановления" : "Подтвердите, что это вы: Face ID, отпечаток или код") : "Рады видеть вас снова"}</p>
        </div>
        {oauthErr && !mfa && <Notice kind={oauthErr === "cancel" ? "info" : "error"}>{(oauthErr === "blocked" && blockMsg) || OAUTH_ERRORS[oauthErr] || OAUTH_ERRORS.provider}</Notice>}
        {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
        {mfa ? (
          <>
            {mfaWays.passkey && passkeysSupported() && (
              <Button variant="primary" size="lg" block icon={<Fingerprint />} busy={keyBusy} onClick={() => runKey(false)}>Подтвердить Face ID или отпечатком</Button>
            )}
            {mfaWays.totp && (
              <Field label="Код из Google Authenticator" error={s.fieldError("code")} htmlFor="code">
                <Input id="code" name="one-time-code" inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} autoFocus={!mfaWays.passkey} invalid={!!s.fieldError("code")} />
              </Field>
            )}
            {!mfaWays.totp && (mfaWays.telegram || mfaWays.email) && (
              mfaSent ? (
                <div className="stack g12 fade-in">
                  <Field label={mfaSent.via === "telegram" ? "Код из Telegram" : "Код из письма"} error={s.fieldError("code")} htmlFor="code">
                    <Input id="code" name="one-time-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))} autoFocus invalid={!!s.fieldError("code")} />
                  </Field>
                  {mfaSent.test_code && <Notice kind="warn">{mfaSent.mail_failed ? "Письмо не ушло: почта недоступна с этого компьютера." : "Тестовый режим:"} Код — <b className="num">{mfaSent.test_code}</b></Notice>}
                </div>
              ) : (
                <div className="stack g8">
                  <span className="small muted">Нет Face ID на этом устройстве? Пришлём код:</span>
                  <div className="row g8 wrap">
                    {mfaWays.telegram && <Button onClick={async () => { try { const r = await s.submit(() => api.post<{ via: string; test_code?: string }>("/auth/login/code", { via: "telegram" })); if (r) setMfaSent(r); } catch { /* */ } }}>В Telegram</Button>}
                    {mfaWays.email && <Button onClick={async () => { try { const r = await s.submit(() => api.post<{ via: string; test_code?: string; mail_failed?: boolean }>("/auth/login/code", { via: "email" })); if (r) setMfaSent(r); } catch { /* */ } }}>На почту</Button>}
                  </div>
                </div>
              )
            )}
          </>
        ) : (
          <>
            <SocialButtons next={sp.get("next") || undefined} />
            {config?.auth && (config.auth.google !== "off" || config.auth.apple !== "off") && <OrLine />}
            {phoneOn && (
              <Segmented fill label="Способ входа" value={how} onChange={(v) => { setHow(v); s.reset(); }} options={[{ value: "password", label: "Пароль" }, { value: "phone", label: "Код в Telegram" }]} />
            )}
            {how === "password" ? (
              <div key="pw" className="stack g16 fade-in">
                <Field label="Почта или телефон" htmlFor="email">
                  <Input id="email" name="username" type="text" inputMode="email" autoComplete="username" autoCapitalize="none" autoCorrect="off" spellCheck={false} value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus placeholder="name@mail.by или +375 29 …" />
                </Field>
                <Field label={<span className="row between">Пароль<Link to="/forgot-password" className="small">Забыли пароль?</Link></span>} htmlFor="password">
                  <Input id="password" name="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
                </Field>
              </div>
            ) : (
              <div key="ph" className="stack g16 fade-in">
                <Field label="Номер телефона" error={s.fieldError("phone")} htmlFor="phone" hint={sent ? undefined : "Код придёт в Telegram. Номер подключается в профиле"}>
                  <PhoneInput id="phone" name="tel" value={phone} disabled={!!sent} onChange={setPhone} invalid={!!s.fieldError("phone")} autoFocus />
                </Field>
                {sent && (
                  <>
                    <Field label="Код из Telegram" error={s.fieldError("code")} htmlFor="sms">
                      <Input id="sms" name="one-time-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={sms} onChange={(e) => setSms(e.target.value.replace(/\D/g, ""))} autoFocus invalid={!!s.fieldError("code")} />
                    </Field>
                    {sent.test_code ? <Notice kind="warn">Тестовый Telegram: код — <b className="num">{sent.test_code}</b></Notice> : <Notice kind="info">Отправили код в Telegram, в чат с ботом AutoInvest.</Notice>}
                    <button type="button" className="link-btn small" onClick={() => { setSent(null); setSms(""); s.reset(); }}>Изменить номер или отправить код ещё раз</button>
                  </>
                )}
              </div>
            )}
          </>
        )}
        {(!mfa || mfaWays.totp || mfaSent) && <Button type="submit" variant={mfa && mfaWays.passkey ? "secondary" : "primary"} size="lg" block busy={s.busy}>{mfa ? "Подтвердить код" : how === "phone" && !sent ? "Получить код в Telegram" : "Войти"}</Button>}
        {mfa && (
          <button type="button" className="link-btn small" style={{ alignSelf: "center" }} onClick={async () => {
            try { await api.post("/auth/logout"); } catch { /* сеанс уже истёк */ }
            autoKey.current = false; setMfa(false); setMfaSent(null); setCode(""); s.reset(); setMe({ user: null });
          }}>Войти в другой аккаунт</button>
        )}
        {!mfa && <SocialConsent />}
        {!mfa && <p className="small muted" style={{ textAlign: "center" }}>Нет аккаунта? <Link to={"/register" + (sp.get("next") ? "?next=" + encodeURIComponent(sp.get("next")!) : "")}>Зарегистрироваться</Link></p>}
      </form>
    </div>
  );
}

export function Register() {
  useTitle("Регистрация");
  const [sp] = useSearchParams();
  const nav = useNavigate();
  const { setMe, me } = useSession();
  const [f, setF] = useState({ name: "", email: "", password: "", offer: false, privacy: false });
  const [sent, setSent] = useState<null | { test_code?: string; mail_failed?: boolean }>(null);
  const [code, setCode] = useState("");
  const s = useSubmit();
  const ref = sp.get("ref") || "";
  // После регистрации переход в анкету делает onSubmit. Роутер применяет навигацию как переход
  // с низким приоритетом, поэтому без этого флага эффект «уже вошёл → /app» успевает его перебить.
  const justRegistered = useRef(false);
  useEffect(() => {
    if (me?.user && !justRegistered.current) nav("/app", { replace: true });
  }, [me, nav]);
  const [score, label] = strength(f.password);
  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      if (!sent) {
        const r = await s.submit(() => api.post<{ test_code?: string; mail_failed?: boolean }>("/auth/register/start", { name: f.name, email: f.email, password: f.password, ref, accept_offer: f.offer, accept_privacy: f.privacy, ...getUtm() }));
        if (r) setSent(r);
        return;
      }
      const r = await s.submit(() => api.post<Me>("/auth/register/confirm", { email: f.email, code }));
      if (r) {
        justRegistered.current = true;
        setMe(r);
        nav(safeNext(sp.get("next") || "/app/onboarding"), { replace: true });
      }
    } catch {
      /* */
    }
  };
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) => setF({ ...f, [k]: e.target.type === "checkbox" ? e.target.checked : e.target.value });
  return (
    <div className="auth-wrap">
      <form className="auth-card auth-compact sheet sheet-pad stack g12" method="post" action="/register" autoComplete="on" onSubmit={onSubmit} noValidate>
        <div>
          <h1>{sent ? "Подтвердите почту" : "Регистрация"}</h1>
          {sent && <p className="muted mt4">Остался один шаг</p>}
        </div>
        {ref && <Notice kind="ok">Вы пришли по приглашению — после первого пополнения вам и другу включим месяц Pro.</Notice>}
        {sent ? (
          <div className="stack g16 fade-in">
            <Notice kind="info">Отправили код на <b>{f.email}</b>. Введите его, чтобы закончить регистрацию.</Notice>
            <Field label="Код из письма" error={s.fieldError("code")} htmlFor="code">
              <Input id="code" name="one-time-code" inputMode="numeric" autoComplete="one-time-code" maxLength={6} value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))} autoFocus invalid={!!s.fieldError("code")} />
            </Field>
            {sent.test_code && <Notice kind="warn">{sent.mail_failed ? "Письмо не ушло: почта недоступна с этого компьютера (VPN или провайдер)." : "Тестовый режим: почта не настроена."} Код — <b className="num">{sent.test_code}</b></Notice>}
            {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
            <button type="button" className="link-btn small" onClick={() => { setSent(null); setCode(""); s.reset(); }}>Изменить почту или отправить код ещё раз</button>
          </div>
        ) : (
          <>
        <SocialButtons next={sp.get("next") || "/app/onboarding"} />
        <OrLine />
        {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
        <Field label="Имя" error={s.fieldError("name")} htmlFor="name">
          <Input id="name" name="name" autoComplete="name" value={f.name} onChange={set("name")} invalid={!!s.fieldError("name")} autoFocus />
        </Field>
        <Field label="Почта" error={s.fieldError("email")} htmlFor="email">
          <Input id="email" name="email" type="email" autoComplete="username" autoCapitalize="none" spellCheck={false} value={f.email} onChange={set("email")} invalid={!!s.fieldError("email")} />
        </Field>
        <Field label="Пароль" error={s.fieldError("password")} htmlFor="password" hint={f.password ? `Надёжность: ${label}` : "От 8 символов, буквы и цифры"}>
          <Input id="password" name="new-password" type="password" autoComplete="new-password" value={f.password} onChange={set("password")} invalid={!!s.fieldError("password")} />
          {f.password && <div className="bar" aria-hidden><span style={{ width: score * 25 + "%", background: score < 2 ? "var(--down)" : score < 3 ? "var(--warn)" : "var(--up)" }} /></div>}
        </Field>
        <div className="stack g8 auth-checks">
          <label className="check"><input type="checkbox" checked={f.offer} onChange={set("offer")} /><span>Принимаю <Link to="/legal/offer" target="_blank">оферту</Link> и <Link to="/legal/risks" target="_blank">предупреждение о рисках</Link></span></label>
          {s.fieldError("accept_offer") && <div className="small down">{s.fieldError("accept_offer")}</div>}
          <label className="check"><input type="checkbox" checked={f.privacy} onChange={set("privacy")} /><span>Согласен на <Link to="/legal/privacy" target="_blank">обработку персональных данных</Link></span></label>
          {s.fieldError("accept_privacy") && <div className="small down">{s.fieldError("accept_privacy")}</div>}
        </div>
          </>
        )}
        <Button type="submit" variant="primary" size="lg" block busy={s.busy} disabled={!!sent && code.length !== 6}>{sent ? "Подтвердить и создать аккаунт" : "Получить код на почту"}</Button>
        {!sent && <p className="tiny muted auth-google-note">Через Google: при первом входе вы принимаете <Link to="/legal/offer" target="_blank">оферту</Link>, <Link to="/legal/risks" target="_blank">риски</Link> и <Link to="/legal/privacy" target="_blank">обработку данных</Link>.</p>}
        <p className="small muted" style={{ textAlign: "center" }}>Уже есть аккаунт? <Link to="/login">Войти</Link></p>
      </form>
    </div>
  );
}

export function Forgot() {
  useTitle("Восстановление пароля");
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const s = useSubmit();
  return (
    <div className="auth-wrap">
      <form className="auth-card sheet sheet-pad stack g16" onSubmit={async (e) => { e.preventDefault(); try { await s.submit(() => api.post("/auth/forgot", { email })); setSent(true); } catch { /* */ } }}>
        <h1>Восстановление пароля</h1>
        {sent ? (
          <Notice kind="ok">Если аккаунт с адресом {email} существует, мы отправили на него ссылку для смены пароля. Она действует 1 час.</Notice>
        ) : (
          <>
            <p className="muted">Отправим ссылку для смены пароля на почту, указанную при регистрации.</p>
            {s.error && <Notice kind="error">{s.error}</Notice>}
            <Field label="Почта" htmlFor="email"><Input id="email" name="email" type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus required /></Field>
            <Button type="submit" variant="primary" size="lg" block busy={s.busy}>Отправить ссылку</Button>
          </>
        )}
        <Link to="/login" className="small" style={{ textAlign: "center" }}>Вернуться ко входу</Link>
      </form>
    </div>
  );
}

export function Reset() {
  useTitle("Новый пароль");
  const [sp] = useSearchParams();
  const [p, setP] = useState("");
  const [done, setDone] = useState(false);
  const s = useSubmit();
  return (
    <div className="auth-wrap">
      <form className="auth-card sheet sheet-pad stack g16" onSubmit={async (e) => { e.preventDefault(); try { await s.submit(() => api.post("/auth/reset", { token: sp.get("token"), password: p })); setDone(true); } catch { /* */ } }}>
        <h1>Новый пароль</h1>
        {done ? (
          <>
            <Notice kind="ok">Пароль изменён. Все устройства вышли из аккаунта — войдите с новым паролем.</Notice>
            <Link to="/login" className="btn btn-primary btn-lg btn-block">Войти</Link>
          </>
        ) : (
          <>
            {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
            <Field label="Новый пароль" error={s.fieldError("password")} hint="Не короче 8 символов, буквы и цифры" htmlFor="np">
              <Input id="np" name="new-password" type="password" autoComplete="new-password" value={p} onChange={(e) => setP(e.target.value)} autoFocus />
            </Field>
            <Button type="submit" variant="primary" size="lg" block busy={s.busy}>Сохранить пароль</Button>
          </>
        )}
      </form>
    </div>
  );
}

export function VerifyEmail() {
  useTitle("Подтверждение почты");
  const [sp] = useSearchParams();
  const [state, setState] = useState<"wait" | "ok" | "err">("wait");
  const [msg, setMsg] = useState("");
  const { reload } = useSession();
  useEffect(() => {
    api.post("/auth/verify-email", { token: sp.get("token") })
      .then(() => { setState("ok"); reload(); })
      .catch((e) => { setState("err"); setMsg(e.message); });
  }, [sp, reload]);
  return (
    <div className="auth-wrap">
      <div className="auth-card sheet sheet-pad stack g16">
        <h1>Подтверждение почты</h1>
        {state === "wait" && <p className="muted">Проверяем ссылку…</p>}
        {state === "ok" && <Notice kind="ok">Почта подтверждена. Теперь чеки и важные уведомления будут приходить на неё.</Notice>}
        {state === "err" && <Notice kind="error">{msg}</Notice>}
        <Link to="/app" className="btn btn-primary btn-block">Перейти в кабинет</Link>
      </div>
    </div>
  );
}
