// Подтверждение опасного действия (вывод, закрытие копилки, удаление аккаунта):
// пароль + код из Google Authenticator (если включён) — или одним касанием Face ID / отпечатка.
import { useState } from "react";
import { Fingerprint } from "lucide-react";
import { useSession } from "../lib/session";
import { passkeyError, passkeysSupported, usePasskey } from "../lib/webauthn";
import { Button, Field, Input, Notice } from "./ui";

type FieldErr = (name: string) => string | null;

export function useConfirm() {
  const { me } = useSession();
  const u = me?.user;
  const hasPw = u?.has_password !== false;
  const totp = !!u?.totp_enabled;
  const keys = (me?.passkeys || 0) > 0 && passkeysSupported();
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [byKey, setByKey] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const ready = byKey || ((!hasPw || !!password) && (!totp || code.length >= 6) && (hasPw || totp));
  const reset = () => { setPassword(""); setCode(""); setByKey(false); setErr(""); };
  const view = (fieldError: FieldErr, hint?: string) => byKey ? (
    <Notice kind="ok">Подтверждено Face ID или отпечатком</Notice>
  ) : (
    <div className="stack g12">
      {hasPw && (
        <Field label="Пароль от аккаунта" error={fieldError("password")} hint={hint}>
          <Input name="password" type="password" autoComplete="current-password" value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
      )}
      {totp && (
        <Field label="Код из Google Authenticator" error={fieldError("code")}>
          <Input inputMode="numeric" autoComplete="one-time-code" maxLength={12} value={code} onChange={(e) => setCode(e.target.value.replace(/\s/g, ""))} placeholder="123456" />
        </Field>
      )}
      {!hasPw && !totp && <Notice kind="warn">Чтобы подтверждать вывод денег, задайте пароль в Профиль → Безопасность.</Notice>}
      {keys && (
        <Button icon={<Fingerprint />} busy={busy} onClick={async () => {
          setBusy(true); setErr("");
          try { await usePasskey("stepup"); setByKey(true); } catch (e: any) { setErr(passkeyError(e)); } finally { setBusy(false); }
        }}>{hasPw || totp ? "Или подтвердить Face ID / отпечатком" : "Подтвердить Face ID / отпечатком"}</Button>
      )}
      {err && <div className="small down">{err}</div>}
    </div>
  );
  return { view, body: { password, code }, ready, reset };
}
