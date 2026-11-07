// Face ID / Touch ID / отпечаток через WebAuthn. Сервер присылает параметры в JSON (base64url),
// браузеру нужны ArrayBuffer — переводим туда и обратно вручную, чтобы работало и в старых Safari.
import { api } from "./api";

const b64 = (s: string) => {
  const p = s.replace(/-/g, "+").replace(/_/g, "/");
  const bin = atob(p + "===".slice((p.length + 3) % 4));
  return Uint8Array.from(bin, (c) => c.charCodeAt(0)).buffer;
};
const ub64 = (buf: ArrayBuffer | null) => {
  if (!buf) return null;
  let s = "";
  new Uint8Array(buf).forEach((b) => (s += String.fromCharCode(b)));
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
};

export const passkeysSupported = () => typeof window !== "undefined" && !!window.PublicKeyCredential && !!navigator.credentials;

/** Добавить ключ на этом устройстве. */
export async function addPasskey(name: string) {
  const opts = await api.post<{ publicKey: any }>("/me/passkeys/begin");
  const pk = opts.publicKey;
  pk.challenge = b64(pk.challenge);
  pk.user.id = b64(pk.user.id);
  pk.excludeCredentials = (pk.excludeCredentials || []).map((c: any) => ({ ...c, id: b64(c.id) }));
  const cred = (await navigator.credentials.create({ publicKey: pk })) as PublicKeyCredential;
  const r = cred.response as AuthenticatorAttestationResponse;
  const body = {
    id: cred.id, rawId: ub64(cred.rawId), type: cred.type,
    response: { clientDataJSON: ub64(r.clientDataJSON), attestationObject: ub64(r.attestationObject), transports: r.getTransports?.() || [] },
  };
  const r2 = await api.post(`/me/passkeys/finish?name=${encodeURIComponent(name)}`, body);
  markDevicePasskey();
  return r2;
}

/** Подтвердить ключом: при входе (второй шаг) или перед опасным действием. */
export async function usePasskey(kind: "login" | "stepup") {
  const base = kind === "login" ? "/auth/login/passkey" : "/me/stepup";
  const opts = await api.post<{ publicKey: any }>(`${base}/begin`);
  const pk = opts.publicKey;
  pk.challenge = b64(pk.challenge);
  pk.allowCredentials = (pk.allowCredentials || []).map((c: any) => ({ ...c, id: b64(c.id) }));
  const cred = (await navigator.credentials.get({ publicKey: pk })) as PublicKeyCredential;
  const r = cred.response as AuthenticatorAssertionResponse;
  const body = {
    id: cred.id, rawId: ub64(cred.rawId), type: cred.type,
    response: { clientDataJSON: ub64(r.clientDataJSON), authenticatorData: ub64(r.authenticatorData), signature: ub64(r.signature), userHandle: ub64(r.userHandle) },
  };
  const res = await api.post<any>(`${base}/finish`, body);
  markDevicePasskey();
  return res;
}

// На этом устройстве уже есть ключ входа — показываем «Войти по Face ID» на странице входа.
const PK_FLAG = "ai_pk";
export const deviceHasPasskey = () => { try { return localStorage.getItem(PK_FLAG) === "1"; } catch { return false; } };
export const markDevicePasskey = (on = true) => { try { on ? localStorage.setItem(PK_FLAG, "1") : localStorage.removeItem(PK_FLAG); } catch { /* */ } };

/** Вход одним касанием: без почты, пароля и кода. */
export async function loginWithPasskey() {
  const opts = await api.post<{ publicKey: any; flow: string }>("/auth/passkey/begin");
  const pk = opts.publicKey;
  pk.challenge = b64(pk.challenge);
  pk.allowCredentials = (pk.allowCredentials || []).map((c: any) => ({ ...c, id: b64(c.id) }));
  const cred = (await navigator.credentials.get({ publicKey: pk })) as PublicKeyCredential;
  const r = cred.response as AuthenticatorAssertionResponse;
  const body = {
    id: cred.id, rawId: ub64(cred.rawId), type: cred.type,
    response: { clientDataJSON: ub64(r.clientDataJSON), authenticatorData: ub64(r.authenticatorData), signature: ub64(r.signature), userHandle: ub64(r.userHandle) },
  };
  const me = await api.post<any>(`/auth/passkey/finish?flow=${encodeURIComponent(opts.flow)}`, body);
  markDevicePasskey();
  return me;
}

/** Понятный текст ошибки браузера (отмена, нет датчика и т. п.). */
export function passkeyError(e: any) {
  if (e?.name === "NotAllowedError") return "Проверка отменена или время вышло";
  if (e?.name === "InvalidStateError") return "Это устройство уже добавлено";
  if (e?.name === "SecurityError") return "Ключи входа работают только на защищённом адресе сайта";
  return e?.message || "Не получилось";
}

/** Название устройства для списка ключей. */
export function deviceName() {
  const ua = navigator.userAgent;
  const os = /iPhone/.test(ua) ? "iPhone" : /iPad/.test(ua) ? "iPad" : /Android/.test(ua) ? "Android" : /Mac/.test(ua) ? "Mac" : /Windows/.test(ua) ? "Windows" : "Устройство";
  const br = /Edg\//.test(ua) ? "Edge" : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : /Firefox\//.test(ua) ? "Firefox" : "";
  return br ? `${os} · ${br}` : os;
}
