// Предложение включить вход по Face ID / отпечатку: появляется в кабинете, если на этом устройстве ключа ещё нет.
// «Не сейчас» прячет его на 30 дней (запоминается в браузере).
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { useLocation } from "react-router-dom";
import { Fingerprint, X } from "lucide-react";
import { useSession } from "../lib/session";
import { setOfferHeight, tgWanted, useOffersTick, welcomePending } from "../lib/offers";
import { addPasskey, deviceHasPasskey, deviceName, markDevicePasskey, passkeyError, platformBiometrics } from "../lib/webauthn";
import { Button, useToast } from "./ui";

const KEY = "ai_pk_offer";

function dismissedRecently(uid: number) {
  try { return Date.now() - Number(localStorage.getItem(`${KEY}:${uid}`) || 0) < 30 * 86400_000; } catch { return true; }
}

export function BiometricOffer() {
  useOffersTick();
  const { me, config, reload } = useSession();
  const box = useRef<HTMLDivElement>(null);
  const toast = useToast();
  const [show, setShow] = useState(false);
  const [busy, setBusy] = useState(false);
  const { pathname } = useLocation();
  // не мешаем первому пути: предлагаем, когда человек уже выбрал копилку (и прошёл лист ожидания),
  // не на анкете и не в мастере копилки; на демо-счёте биометрия не настраивается
  // очередь: сначала приветствие, потом Telegram, потом Face ID — по одной карточке
  const ready = !!me?.user && !me.demo && (me.plans || 0) > 0 && !/^\/app\/(onboarding|plans\/new)/.test(pathname)
    && !welcomePending() && !tgWanted(me, config);
  // предлагаем, только если на этом устройстве есть датчик (Face ID, Touch ID, отпечаток, Windows Hello)
  // и ключа на нём ещё нет; если биометрия включена на другом устройстве — предлагаем добавить и это
  useEffect(() => {
    const uid = me?.user?.id || 0;
    if (!ready || deviceHasPasskey(uid) || dismissedRecently(uid)) return;
    let alive = true, t = 0;
    platformBiometrics().then((ok) => { if (ok && alive) t = window.setTimeout(() => setShow(true), 1500); });
    return () => { alive = false; clearTimeout(t); };
  }, [ready]);
  const elsewhere = (me?.passkeys || 0) > 0;
  useLayoutEffect(() => {
    if (show && ready) setOfferHeight(box.current?.offsetHeight || 0);
    return () => setOfferHeight(0);
  }, [show, ready]);
  if (!show || !ready) return null;
  const later = () => { try { localStorage.setItem(`${KEY}:${me?.user?.id}`, String(Date.now())); } catch { /* */ } setShow(false); };
  return (
    <div className="bio-offer" ref={box} role="dialog" aria-label="Вход по Face ID">
      <button type="button" className="bio-x" aria-label="Закрыть" onClick={later}><X /></button>
      <span className="bio-ic"><Fingerprint /></span>
      <div className="stack g4">
        <b>{elsewhere ? "Включите Face ID и на этом устройстве" : "Подтверждайте вход по Face ID или отпечатку"}</b>
        <span>{elsewhere ? "Сейчас на нём придётся ждать код. С биометрией — одно касание после пароля." : "После пароля — одно касание вместо кода из Telegram или письма. Так мы точно знаем, что это вы."}</span>
      </div>
      <div className="row g8">
        <Button variant="primary" size="sm" busy={busy} onClick={async () => {
          setBusy(true);
          try { await addPasskey(deviceName(), me?.user?.id); reload(); setShow(false); toast("Готово — теперь вход одним касанием"); }
          catch (e: any) {
            // ключ этого устройства уже есть в аккаунте (добавлен раньше) — просто запоминаем и не предлагаем
            if (e?.name === "InvalidStateError") { markDevicePasskey(me?.user?.id); setShow(false); toast("Face ID на этом устройстве уже включён"); }
            else toast(passkeyError(e), "error");
          }
          finally { setBusy(false); }
        }}>Включить</Button>
        <Button size="sm" variant="ghost" onClick={later}>Не сейчас</Button>
      </div>
    </div>
  );
}
