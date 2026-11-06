// Подтверждение личности: через банк (МСИ) или по паспорту и селфи (Sumsub). Без ручного ввода паспортных данных.
// Здесь же — тестовая страница МСИ для стенда (MSI_MODE=sandbox) и тестовая проверка документов (KYC_DOCS=sandbox).
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { BadgeCheck, Camera, CheckCircle2, FileImage, Landmark, Lock, ScanFace, ShieldCheck, Smartphone } from "lucide-react";
import { api } from "../../lib/api";
import { dateTime } from "../../lib/format";
import { useLoad, useSubmit, useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { KYC, KYCMethods } from "../../lib/types";
import { Button, Field, Input, LoadError, Notice, PageLoader, useToast } from "../../components/ui";

const METHOD_TEXT: Record<KYC["method"], string> = {
  manual: "по анкете", msi: "через банк (МСИ)", "msi-sandbox": "через банк (МСИ), тестовый режим", docs: "по паспорту и селфи", "docs-sandbox": "по паспорту и селфи, тестовый режим",
};

/** Вкладка «Верификация» в профиле. manualForm — запасная анкета для режима KYC_DOCS=manual. */
export function VerificationPanel({ manualForm }: { manualForm: (initial: KYC | null, done: () => void) => ReactNode }) {
  const { reload: reloadMe } = useSession();
  const toast = useToast();
  const [sp, setSp] = useSearchParams();
  const { data, error, loading, reload } = useLoad(() => api.get<{ kyc: KYC | null; methods: KYCMethods }>("/kyc"), []);
  const [docs, setDocs] = useState<null | "sandbox" | "sumsub" | "manual">(null);
  const [sumsubToken, setSumsubToken] = useState("");
  const msi = useSubmit();
  const start = useSubmit();
  const nav = useNavigate();

  // возврат со страницы МСИ
  useEffect(() => {
    if (sp.get("done") === "msi") { toast("Личность подтверждена через банк"); reloadMe(); }
    if (sp.get("error") === "msi") toast("Не удалось получить данные из банка. Попробуйте ещё раз или выберите другой способ", "error");
    if (sp.get("done") || sp.get("error")) setSp({}, { replace: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // пока документы на проверке — тихо опрашиваем статус
  const k = data?.kyc || null;
  useEffect(() => {
    if (k?.status !== "pending" || k.method === "manual") return;
    const t = setInterval(async () => {
      const r = await api.get<{ kyc: KYC | null }>("/kyc").catch(() => null);
      if (r?.kyc && r.kyc.status !== "pending") { reload(); reloadMe(); }
    }, 2500);
    return () => clearInterval(t);
  }, [k?.status, k?.method, reload, reloadMe]);

  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const methods = data!.methods;
  const done = () => { setDocs(null); reload(); reloadMe(); };

  if (k?.status === "approved") return <Approved k={k} />;
  if (k?.status === "pending" && k.method !== "manual") return <Checking k={k} />;
  if (docs === "manual" || (k?.status === "pending" && k.method === "manual")) return <>{manualForm(k, done)}</>;
  if (docs === "sandbox") return <DocsSandbox onCancel={() => setDocs(null)} onDone={done} />;
  if (docs === "sumsub") return <SumsubWidget token={sumsubToken} onCancel={() => setDocs(null)} onSubmitted={done} />;

  const goMsi = async () => {
    try {
      const r = await msi.submit(() => api.post<{ redirect_url: string }>("/kyc/msi/start"));
      if (!r) return;
      if (r.redirect_url.startsWith("/")) nav(r.redirect_url);
      else window.location.href = r.redirect_url;
    } catch (e: any) { toast(e.message, "error"); }
  };
  const goDocs = async () => {
    try {
      const r = await start.submit(() => api.post<{ provider: "sandbox" | "sumsub" | "manual"; token?: string }>("/kyc/docs/start"));
      if (!r) return;
      if (r.token) setSumsubToken(r.token);
      setDocs(r.provider);
    } catch (e: any) { toast(e.message, "error"); }
  };

  return (
    <section className="sheet">
      <div className="sheet-head"><h2>Подтвердите личность</h2></div>
      <div className="sheet-body stack g16">
        {k?.status === "rejected" && <Notice kind="error" title="Проверка не пройдена">{k.reject_reason}</Notice>}
        <p style={{ margin: 0 }}>Так требует закон о предотвращении отмывания денег: покупать токены можно только после идентификации. Займёт пару минут.</p>
        <div className="kyc-ways">
          {methods.msi !== "off" && (
            <div className="kyc-way best">
              <span className="kyc-tag">Быстрее всего</span>
              <span className="kyc-ic"><Landmark /></span>
              <h3>Через банк — МСИ</h3>
              <p>Межбанковская система идентификации: банк, где у вас есть счёт, сам подтвердит личность.</p>
              <ul>
                <li><CheckCircle2 />Идентификационный номер и код из SMS</li>
                <li><CheckCircle2 />Без фото паспорта и селфи</li>
                <li><CheckCircle2 />Около минуты</li>
              </ul>
              <Button variant="primary" block busy={msi.busy} onClick={goMsi}>Подтвердить через банк</Button>
            </div>
          )}
          <div className="kyc-way">
            <span className="kyc-ic"><ScanFace /></span>
            <h3>По паспорту и селфи</h3>
            <p>{methods.docs === "manual" ? "Загрузите фото паспорта и заполните данные — проверит сотрудник." : "Фото страницы паспорта и короткое селфи. Данные распознаются автоматически."}</p>
            <ul>
              <li><CheckCircle2 />Паспорт РБ или вид на жительство</li>
              <li><CheckCircle2 />{methods.docs === "manual" ? "До одного рабочего дня" : "Несколько минут"}</li>
            </ul>
            <Button block busy={start.busy} onClick={goDocs}>Продолжить с паспортом</Button>
          </div>
        </div>
      </div>
      <div className="sheet-foot kyc-foot"><Lock />Данные передаются по защищённому каналу и используются только для идентификации.</div>
    </section>
  );
}

function Approved({ k }: { k: KYC }) {
  const hasData = !!k.last_name;
  return (
    <section className="sheet">
      <div className="kyc-ok">
        <span className="kyc-ok-ic"><BadgeCheck /></span>
        <div>
          <h2>Личность подтверждена</h2>
          <p>{METHOD_TEXT[k.method] ? `Способ — ${METHOD_TEXT[k.method]}` : ""}{k.reviewed_at ? `, ${dateTime(k.reviewed_at)}` : ""}</p>
        </div>
      </div>
      {hasData && (
        <div className="sheet-body" style={{ paddingTop: 0 }}>
          <dl className="kv">
            <dt>ФИО</dt><dd>{k.last_name} {k.first_name} {k.middle_name}</dd>
            {k.birth_date && <><dt>Дата рождения</dt><dd>{k.birth_date.split("-").reverse().join(".")}</dd></>}
            {k.doc_number && <><dt>Паспорт</dt><dd>{k.doc_number.slice(0, 2)}•••••{k.doc_number.slice(-2)}</dd></>}
            {k.personal_number && <><dt>Идентификационный номер</dt><dd>{k.personal_number.slice(0, 3)}•••••••••{k.personal_number.slice(-2)}</dd></>}
          </dl>
        </div>
      )}
    </section>
  );
}

function Checking({ k }: { k: KYC }) {
  return (
    <section className="sheet">
      <div className="kyc-check">
        <span className="kyc-pulse"><ShieldCheck /></span>
        <h2>Проверяем документы</h2>
        <p>Обычно это занимает несколько минут — страница обновится сама, а ещё пришлём уведомление.</p>
        <span className="small muted">Отправлено {dateTime(k.submitted_at)}</span>
      </div>
    </section>
  );
}

// ---------------------------------------------------------------- паспорт и селфи (тест)

/** Тестовая проверка документов: по шагам, как виджет Sumsub — фото паспорта, селфи, отправка. */
function DocsSandbox({ onCancel, onDone }: { onCancel: () => void; onDone: () => void }) {
  const [step, setStep] = useState<0 | 1>(0);
  const [doc, setDoc] = useState<File | null>(null);
  const [selfie, setSelfie] = useState<File | null>(null);
  const s = useSubmit();
  const send = async () => {
    const fd = new FormData();
    if (doc) fd.append("doc_file", doc);
    if (selfie) fd.append("selfie_file", selfie);
    try { await s.submit(() => api.post("/kyc/docs/sandbox", fd)); onDone(); } catch { /* */ }
  };
  return (
    <section className="sheet">
      <div className="sheet-head"><h2>Паспорт и селфи</h2><span className="badge badge-warn">Тестовый режим</span></div>
      <div className="sheet-body stack g16">
        <ol className="kyc-steps">
          <li className={step === 0 ? "on" : "done"}><span>1</span>Паспорт</li>
          <li className={step === 1 ? "on" : ""}><span>2</span>Селфи</li>
        </ol>
        {step === 0 ? (
          <Shot icon={<FileImage />} title="Страница паспорта с фото" hint="Положите паспорт на ровную поверхность, без бликов, все углы в кадре" file={doc} onFile={setDoc} capture="environment" error={s.fieldError("doc_file")} />
        ) : (
          <Shot icon={<Camera />} title="Селфи" hint="Лицо целиком в кадре, без очков и головного убора" file={selfie} onFile={setSelfie} capture="user" error={s.fieldError("selfie_file")} />
        )}
        {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
        <Notice kind="info">На стенде подойдёт любое фото — через несколько секунд проверка одобрит его автоматически. С подключённым Sumsub здесь откроется его виджет с проверкой «живости».</Notice>
        <div className="row between g8">
          <Button variant="ghost" onClick={step === 0 ? onCancel : () => setStep(0)}>{step === 0 ? "Другой способ" : "Назад"}</Button>
          {step === 0
            ? <Button variant="primary" disabled={!doc} onClick={() => setStep(1)}>Дальше</Button>
            : <Button variant="primary" disabled={!selfie} busy={s.busy} onClick={send}>Отправить на проверку</Button>}
        </div>
      </div>
    </section>
  );
}

function Shot({ icon, title, hint, file, onFile, capture, error }: { icon: ReactNode; title: string; hint: string; file: File | null; onFile: (f: File | null) => void; capture: "user" | "environment"; error?: string | null }) {
  const [url, setUrl] = useState("");
  useEffect(() => {
    if (!file || !file.type.startsWith("image/")) { setUrl(""); return; }
    const u = URL.createObjectURL(file);
    setUrl(u);
    return () => URL.revokeObjectURL(u);
  }, [file]);
  return (
    <label className={`kyc-shot ${file ? "has" : ""} ${error ? "err" : ""}`}>
      <input type="file" accept="image/jpeg,image/png" capture={capture} onChange={(e) => onFile(e.target.files?.[0] || null)} />
      {url ? <img src={url} alt="" /> : <span className="kyc-shot-ic">{icon}</span>}
      <b>{file ? "Готово — нажмите, чтобы переснять" : title}</b>
      <span className="muted small">{error || hint}</span>
    </label>
  );
}

// ---------------------------------------------------------------- Sumsub

declare global {
  interface Window { snsWebSdk?: any }
}
const SUMSUB_SDK = "https://static.sumsub.com/idensic/static/sns-websdk-builder.js";

/** Виджет Sumsub в странице: фото документа, селфи с проверкой «живости», итог придёт вебхуком. */
function SumsubWidget({ token, onCancel, onSubmitted }: { token: string; onCancel: () => void; onSubmitted: () => void }) {
  const box = useRef<HTMLDivElement>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    let cancelled = false;
    const launch = () => {
      if (cancelled || !window.snsWebSdk || !box.current) return;
      const refresh = () => api.post<{ token: string }>("/kyc/docs/start").then((r) => r.token);
      window.snsWebSdk.init(token, refresh)
        .withConf({ lang: "ru" })
        .withOptions({ addViewportTag: false, adaptIframeHeight: true })
        .on("idCheck.onApplicantSubmitted", async () => { await api.post("/kyc/docs/submitted").catch(() => null); onSubmitted(); })
        .on("idCheck.onError", () => setErr("Виджет проверки не загрузился. Обновите страницу или выберите другой способ."))
        .build()
        .launch("#sumsub-websdk");
    };
    if (window.snsWebSdk) launch();
    else {
      const sc = document.createElement("script");
      sc.src = SUMSUB_SDK;
      sc.async = true;
      sc.onload = launch;
      sc.onerror = () => setErr("Не удалось загрузить проверку документов. Проверьте интернет и попробуйте снова.");
      document.body.appendChild(sc);
    }
    return () => { cancelled = true; };
  }, [token, onSubmitted]);
  return (
    <section className="sheet">
      <div className="sheet-head"><h2>Паспорт и селфи</h2><Button size="sm" variant="ghost" onClick={onCancel}>Другой способ</Button></div>
      <div className="sheet-body">
        {err && <Notice kind="error">{err}</Notice>}
        <div id="sumsub-websdk" ref={box} style={{ minHeight: 480 }} />
      </div>
    </section>
  );
}

// ---------------------------------------------------------------- МСИ (тестовая страница)

/** Имитация страницы МСИ для стенда: в боевом режиме здесь страница оператора ЕРИП с выбором банка. */
export default function MsiSandbox() {
  useTitle("Идентификация через МСИ");
  const nav = useNavigate();
  const [sp] = useSearchParams();
  const state = sp.get("state") || "";
  const { me } = useSession();
  const [step, setStep] = useState<0 | 1>(0);
  const [pn, setPn] = useState("");
  const [code, setCode] = useState("");
  const s = useSubmit();
  const back = (q = "") => nav("/app/profile/verification" + q, { replace: true });
  const finish = async () => {
    try {
      await s.submit(() => api.post("/kyc/msi/sandbox", { state, personal_number: pn, code }));
      back("?done=msi");
    } catch (e: any) {
      if (e.field === "personal_number") setStep(0);
    }
  };
  const pnOk = /^[0-9]{7}[A-Z][0-9]{3}[A-Z]{2}[0-9]$/.test(pn);
  return (
    <div className="paypage">
      <div className="box stack g16">
        <div className="row between small muted"><span className="row g8"><Lock size={14} />Защищённое соединение</span><span>Тестовая страница МСИ</span></div>
        <div className="sheet sheet-pad stack g16">
          <div className="msi-head">
            <span className="msi-logo"><Landmark /></span>
            <div><b>Межбанковская система идентификации</b><span className="small muted">Запрос от AutoInvest · {me?.user?.email}</span></div>
          </div>
          <Notice kind="warn">Тестовый режим: банк не запрашивается. Подойдёт любой корректный идентификационный номер и любой код из 6 цифр.</Notice>
          {!state ? (
            <Notice kind="error">Ссылка неполная. Вернитесь в профиль и начните заново.</Notice>
          ) : step === 0 ? (
            <form className="stack g16" onSubmit={(e) => { e.preventDefault(); if (pnOk) setStep(1); }}>
              <Field label="Идентификационный номер" hint="14 символов, на странице паспорта с фото" error={s.fieldError("personal_number")}>
                <Input value={pn} onChange={(e) => setPn(e.target.value.toUpperCase().replace(/[^0-9A-Z]/g, "").slice(0, 14))} placeholder="3010195A001PB7" autoFocus autoComplete="off" />
              </Field>
              <Button type="submit" variant="primary" size="lg" block disabled={!pnOk}>Получить код</Button>
            </form>
          ) : (
            <form className="stack g16" onSubmit={(e) => { e.preventDefault(); finish(); }}>
              <div className="msi-sms"><Smartphone /><span>Код отправлен на телефон, привязанный к вашему банку</span></div>
              <Field label="Код из SMS" error={s.fieldError("code")}>
                <Input inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))} placeholder="000000" autoFocus />
              </Field>
              {s.error && !s.field && <Notice kind="error">{s.error}</Notice>}
              <Button type="submit" variant="primary" size="lg" block busy={s.busy} disabled={code.length !== 6}>Подтвердить и передать данные</Button>
              <button type="button" className="link-btn small" onClick={() => setStep(0)}>Изменить номер</button>
            </form>
          )}
          <p className="small muted" style={{ margin: 0 }}>AutoInvest получит ФИО, дату рождения, данные паспорта и адрес регистрации.</p>
          <button type="button" className="link-btn small" onClick={() => back()}>Отменить и вернуться в AutoInvest</button>
        </div>
      </div>
    </div>
  );
}
