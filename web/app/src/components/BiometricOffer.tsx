// Предложение включить вход по Face ID / отпечатку: появляется в кабинете, если на этом устройстве ключа ещё нет.
// «Не сейчас» прячет его на 30 дней (запоминается в браузере).
import { useEffect, useState } from "react";
import { Fingerprint, X } from "lucide-react";
import { useSession } from "../lib/session";
import { addPasskey, deviceHasPasskey, deviceName, markDevicePasskey, passkeyError, platformBiometrics } from "../lib/webauthn";
import { Button, useToast } from "./ui";

const KEY = "ai_pk_offer";

function dismissedRecently() {
  try { return Date.now() - Number(localStorage.getItem(KEY) || 0) < 30 * 86400_000; } catch { return true; }
}

export function BiometricOffer() {
  const { me, reload } = useSession();
  const toast = useToast();
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  // предлагаем, только если на этом устройстве есть датчик (Face ID, Touch ID, отпечаток, Windows Hello)
  // и ключа на нём ещё нет; если биометрия включена на другом устройстве — предлагаем добавить и это
  useEffect(() => {
    if (!me?.user || deviceHasPasskey() || dismissedRecently()) return;
    let alive = true, t = 0;
    platformBiometrics().then((ok) => { if (ok && alive) t = window.setTimeout(() => setShow(true), 1500); });
    return () => { alive = false; clearTimeout(t); };
  }, [me?.user]);
  const elsewhere = (me?.passkeys || 0) > 0;
  if (!show) return null;
  const later = () => { try { localStorage.setItem(KEY, String(Date.now())); } catch { /* */ } setShow(false); };
  return (
    <div className="bio-offer" role="dialog" aria-label="Вход по Face ID">
      <button type="button" className="bio-x" aria-label="Закрыть" onClick={later}><X /></button>
      <span className="bio-ic"><Fingerprint /></span>
      <div className="stack g4">
        <b>{elsewhere ? "Включите Face ID и на этом устройстве" : "Подтверждайте вход по Face ID или отпечатку"}</b>
        <span>{elsewhere ? "Сейчас на нём придётся ждать код. С биометрией — одно касание после пароля." : "После пароля — одно касание вместо кода из Telegram или письма. Так мы точно знаем, что это вы."}</span>
      </div>
      <div className="row g8">
        <Button variant="primary" size="sm" busy={busy} onClick={async () => {
          setBusy(true);
          try { await addPasskey(deviceName()); reload(); setShow(false); toast("Готово — теперь вход одним касанием"); }
          catch (e: any) {
            // ключ этого устройства уже есть в аккаунте (добавлен раньше) — просто запоминаем и не предлагаем
            if (e?.name === "InvalidStateError") { markDevicePasskey(); setShow(false); toast("Face ID на этом устройстве уже включён"); }
            else toast(passkeyError(e), "error");
          }
          finally { setBusy(false); }
        }}>Включить</Button>
        <Button size="sm" variant="ghost" onClick={later}>Не сейчас</Button>
      </div>
    </div>
  );
}
