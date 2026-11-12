// Кнопки «Войти через Google / Apple». Переход полной навигацией: дальше редиректы провайдера и сервера.
import { Link } from "react-router-dom";
import { useSession } from "../lib/session";
import { utmQuery } from "../lib/utm";

export function GoogleMark() {
  return (
    <svg viewBox="0 0 48 48" width="20" height="20" aria-hidden>
      <path fill="#FFC107" d="M43.6 20.5H42V20H24v8h11.3C33.7 32.7 29.2 36 24 36c-6.6 0-12-5.4-12-12s5.4-12 12-12c3.1 0 5.8 1.2 7.9 3.1l5.7-5.7C34 6.1 29.3 4 24 4 12.9 4 4 12.9 4 24s8.9 20 20 20 20-8.9 20-20c0-1.3-.1-2.4-.4-3.5z" />
      <path fill="#FF3D00" d="M6.3 14.7l6.6 4.8C14.7 15.1 19 12 24 12c3.1 0 5.8 1.2 7.9 3.1l5.7-5.7C34 6.1 29.3 4 24 4 16.3 4 9.7 8.3 6.3 14.7z" />
      <path fill="#4CAF50" d="M24 44c5.2 0 9.9-2 13.4-5.2l-6.2-5.2C29.2 35.1 26.7 36 24 36c-5.2 0-9.6-3.3-11.3-8l-6.5 5C9.5 39.6 16.2 44 24 44z" />
      <path fill="#1976D2" d="M43.6 20.5H42V20H24v8h11.3c-.8 2.2-2.2 4.2-4.1 5.6l6.2 5.2C37 39.2 44 34 44 24c0-1.3-.1-2.4-.4-3.5z" />
    </svg>
  );
}

export function AppleMark() {
  return (
    <svg viewBox="0 0 24 24" width="19" height="19" aria-hidden fill="currentColor">
      <path d="M16.37 12.6c-.02-2.34 1.91-3.47 2-3.52-1.09-1.6-2.79-1.82-3.39-1.84-1.44-.15-2.82.85-3.55.85-.74 0-1.86-.83-3.06-.81-1.57.02-3.02.92-3.83 2.33-1.64 2.84-.42 7.04 1.17 9.34.78 1.13 1.7 2.39 2.91 2.35 1.17-.05 1.61-.75 3.02-.75 1.41 0 1.81.75 3.04.73 1.26-.02 2.05-1.14 2.82-2.27.89-1.3 1.25-2.57 1.27-2.63-.03-.01-2.43-.93-2.4-3.78zM14.05 5.7c.64-.78 1.08-1.86.96-2.94-.93.04-2.05.62-2.71 1.39-.6.69-1.12 1.79-.98 2.85 1.04.08 2.09-.53 2.73-1.3z" />
    </svg>
  );
}

/** Кнопки входа через провайдеров. mode=link — привязать к текущему аккаунту. */
export function SocialButtons({ next, compact }: { next?: string; compact?: boolean }) {
  const { config } = useSession();
  const a = config?.auth;
  if (!a || (a.google === "off" && a.apple === "off")) return null;
  const params = [next ? "next=" + encodeURIComponent(next) : "", utmQuery()].filter(Boolean).join("&");
  const q = params ? "?" + params : "";
  return (
    <div className={`social ${compact ? "compact" : ""}`}>
      {a.google !== "off" && <a className="btn btn-secondary btn-lg social-btn" href={`/oauth/google/start${q}`}><GoogleMark />Продолжить с Google</a>}
      {a.apple !== "off" && <a className="btn btn-lg social-btn apple" href={`/oauth/apple/start${q}`}><AppleMark />Продолжить с Apple</a>}
    </div>
  );
}

/** Подпись под кнопками: при первом входе через провайдера аккаунт создаётся сразу. */
export function SocialConsent() {
  return (
    <p className="tiny muted social-consent">
      Входя через Google впервые, вы создаёте аккаунт и принимаете <Link to="/legal/offer" target="_blank">оферту</Link>,{" "}
      <Link to="/legal/risks" target="_blank">предупреждение о рисках</Link> и даёте <Link to="/legal/privacy" target="_blank">согласие на обработку персональных данных</Link>.
    </p>
  );
}

export function OrLine() {
  return <div className="or-line"><span>или</span></div>;
}
