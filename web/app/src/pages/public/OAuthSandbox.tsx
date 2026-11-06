// Тестовая страница входа через Google/Apple (OAUTH_*=sandbox): имитирует экран провайдера на стенде.
// В боевом режиме сюда не попасть — сервер сразу отправляет на accounts.google.com / appleid.apple.com.
import { useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Lock } from "lucide-react";
import { useTitle } from "../../lib/hooks";
import { Button, Field, Input, Notice } from "../../components/ui";
import { AppleMark, GoogleMark } from "../../components/SocialLogin";

export default function OAuthSandbox() {
  const [sp] = useSearchParams();
  const provider = sp.get("provider") === "apple" ? "apple" : "google";
  const state = sp.get("state") || "";
  const title = provider === "google" ? "Google" : "Apple";
  useTitle("Тестовый вход через " + title);
  const [email, setEmail] = useState(provider === "google" ? "test.google@example.com" : "test.apple@example.com");
  const [name, setName] = useState("Тестовый Пользователь");
  return (
    <div className="paypage">
      <div className="box stack g16">
        <div className="row between small muted"><span className="row g8"><Lock size={14} />Защищённое соединение</span><span>Тестовая страница входа</span></div>
        <form className="sheet sheet-pad stack g16" method="post" action="/oauth/sandbox/callback">
          <input type="hidden" name="provider" value={provider} />
          <input type="hidden" name="state" value={state} />
          <div className="oauth-sb-head">
            <span className={`oauth-sb-logo ${provider}`}>{provider === "google" ? <GoogleMark /> : <AppleMark />}</span>
            <div><b>Вход через {title}</b><span className="small muted">в AutoInvest</span></div>
          </div>
          <Notice kind="warn">Тестовый режим: настоящий {title} не запрашивается. Укажите любую почту — с ней войдёт или зарегистрируется пользователь. Если аккаунт с такой почтой уже есть, {title} к нему привяжется.</Notice>
          <Field label="Почта"><Input name="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} autoFocus /></Field>
          <Field label="Имя"><Input name="name" value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <Button type="submit" variant="primary" size="lg" block>Продолжить</Button>
          <button type="submit" name="error" value="access_denied" className="link-btn small">Отменить вход</button>
        </form>
      </div>
    </div>
  );
}
