import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../../lib/api";
import { useSubmit, useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { Me } from "../../lib/types";
import { Button, Field, Input, Notice } from "../../components/ui";

function safeNext(n: string | null) {
  return n && n.startsWith("/") && !n.startsWith("//") ? n : "/app";
}

function formatPhone(v: string) {
  const d = v.replace(/\D/g, "").replace(/^80/, "375");
  const x = (d.startsWith("375") ? d.slice(3) : d).slice(0, 9);
  let out = "+375";
  if (x.length) out += " (" + x.slice(0, 2);
  if (x.length >= 2) out += ")";
  if (x.length > 2) out += " " + x.slice(2, 5);
  if (x.length > 5) out += "-" + x.slice(5, 7);
  if (x.length > 7) out += "-" + x.slice(7, 9);
  return out;
}

function strength(p: string): [number, string] {
  let s = 0;
  if (p.length >= 8) s++;
  if (p.length >= 12) s++;
  if (/[a-zа-я]/.test(p) && /[A-ZА-Я]/.test(p)) s++;
  if (/\d/.test(p) && /[^\w\s]/.test(p)) s++;
  return [s, ["Слишком короткий", "Слабый", "Нормальный", "Хороший", "Надёжный"][s]];
}

export function Login() {
  useTitle("Вход");
  const [sp] = useSearchParams();
  const nav = useNavigate();
  const { setMe, me } = useSession();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [mfa, setMfa] = useState(false);
  const s = useSubmit();
  useEffect(() => {
    if (me?.user) nav(safeNext(sp.get("next")), { replace: true });
    if (me?.mfa_required) setMfa(true);
  }, [me, nav, sp]);
  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      if (!mfa) {
        const r = await s.submit(() => api.post<Me>("/auth/login", { email, password }));
        if (r?.mfa_required) {
          setMfa(true);
          return;
        }
        if (r) setMe(r);
      } else {
        const r = await s.submit(() => api.post<Me>("/auth/login/2fa", { code }));
        if (r) setMe(r);
      }
    } catch {
      /* ошибка уже в s.error */
    }
  };
  return (
    <div className="auth-wrap">
      <form className="auth-card sheet sheet-pad stack g16" onSubmit={onSubmit} noValidate>
        <div>
          <h1>{mfa ? "Код из приложения" : "Вход"}</h1>
          <p className="muted mt8">{mfa ? "Введите 6 цифр из Google Authenticator или код восстановления" : "Рады видеть вас снова"}</p>
        </div>
        {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
        {!mfa ? (
          <>
            <Field label="Почта" htmlFor="email">
              <Input id="email" type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus />
            </Field>
            <Field label={<span className="row between">Пароль<Link to="/forgot-password" className="small">Забыли пароль?</Link></span>} htmlFor="password">
              <Input id="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
            </Field>
          </>
        ) : (
          <Field label="Код" error={s.fieldError("code")} htmlFor="code">
            <Input id="code" inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} autoFocus invalid={!!s.fieldError("code")} />
          </Field>
        )}
        <Button type="submit" variant="primary" size="lg" block busy={s.busy}>{mfa ? "Подтвердить" : "Войти"}</Button>
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
  const [f, setF] = useState({ name: "", email: "", phone: "", password: "", offer: false, privacy: false });
  const s = useSubmit();
  const ref = sp.get("ref") || "";
  useEffect(() => {
    if (me?.user) nav("/app", { replace: true });
  }, [me, nav]);
  const [score, label] = strength(f.password);
  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      const r = await s.submit(() => api.post<Me>("/auth/register", { name: f.name, email: f.email, phone: f.phone, password: f.password, ref, accept_offer: f.offer, accept_privacy: f.privacy }));
      if (r) {
        // сначала уходим со страницы, иначе эффект «уже вошёл → /app» перебьёт переход в анкету
        nav(safeNext(sp.get("next") || "/app/onboarding"), { replace: true });
        setMe(r);
      }
    } catch {
      /* */
    }
  };
  const set = (k: keyof typeof f) => (e: React.ChangeEvent<HTMLInputElement>) => setF({ ...f, [k]: e.target.type === "checkbox" ? e.target.checked : e.target.value });
  return (
    <div className="auth-wrap">
      <form className="auth-card sheet sheet-pad stack g16" onSubmit={onSubmit} noValidate>
        <div>
          <h1>Регистрация</h1>
          <p className="muted mt8">Займёт минуту. Привязать карту и настроить копилку можно сразу после.</p>
        </div>
        {ref && <Notice kind="ok">Вы пришли по приглашению — после первого пополнения вам и другу включим месяц Pro.</Notice>}
        {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
        <Field label="Имя" error={s.fieldError("name")} htmlFor="name">
          <Input id="name" autoComplete="given-name" value={f.name} onChange={set("name")} invalid={!!s.fieldError("name")} autoFocus />
        </Field>
        <Field label="Почта" error={s.fieldError("email")} hint="На неё придут чеки и ссылка для подтверждения" htmlFor="email">
          <Input id="email" type="email" autoComplete="email" value={f.email} onChange={set("email")} invalid={!!s.fieldError("email")} />
        </Field>
        <Field label="Телефон" error={s.fieldError("phone")} hint="Необязательно" htmlFor="phone">
          <Input id="phone" type="tel" autoComplete="tel" placeholder="+375 (29) 123-45-67" value={f.phone} onChange={(e) => setF({ ...f, phone: e.target.value ? formatPhone(e.target.value) : "" })} invalid={!!s.fieldError("phone")} />
        </Field>
        <Field label="Пароль" error={s.fieldError("password")} htmlFor="password" hint={f.password ? `Надёжность: ${label}` : "Не короче 8 символов, буквы и цифры"}>
          <Input id="password" type="password" autoComplete="new-password" value={f.password} onChange={set("password")} invalid={!!s.fieldError("password")} />
          {f.password && <div className="bar" aria-hidden><span style={{ width: score * 25 + "%", background: score < 2 ? "var(--down)" : score < 3 ? "var(--warn)" : "var(--up)" }} /></div>}
        </Field>
        <div className="stack g12">
          <label className="check"><input type="checkbox" checked={f.offer} onChange={set("offer")} /><span>Ознакомлен с <Link to="/legal/offer" target="_blank">публичной офертой</Link> и <Link to="/legal/risks" target="_blank">предупреждением о рисках</Link>, принимаю условия оферты</span></label>
          {s.fieldError("accept_offer") && <div className="small down">{s.fieldError("accept_offer")}</div>}
          <label className="check"><input type="checkbox" checked={f.privacy} onChange={set("privacy")} /><span>Даю согласие на обработку персональных данных в соответствии с <Link to="/legal/privacy" target="_blank">Политикой обработки персональных данных</Link> (Закон Республики Беларусь № 99-З)</span></label>
          {s.fieldError("accept_privacy") && <div className="small down">{s.fieldError("accept_privacy")}</div>}
        </div>
        <Button type="submit" variant="primary" size="lg" block busy={s.busy}>Зарегистрироваться</Button>
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
            <Field label="Почта" htmlFor="email"><Input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus required /></Field>
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
              <Input id="np" type="password" autoComplete="new-password" value={p} onChange={(e) => setP(e.target.value)} autoFocus />
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
