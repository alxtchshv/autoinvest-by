import { Link } from "react-router-dom";
import { useTitle } from "../../lib/hooks";

export default function NotFound({ inApp }: { inApp?: boolean }) {
  useTitle("Страница не найдена");
  return (
    <div className={inApp ? "" : "auth-wrap"}>
      <div className="sheet sheet-pad stack g12" style={{ maxWidth: 480, margin: inApp ? "40px auto" : undefined }}>
        <h1>Такой страницы нет</h1>
        <p className="muted">Возможно, ссылка устарела или в адресе опечатка.</p>
        <Link to={inApp ? "/app" : "/"} className="btn btn-primary">{inApp ? "На главную кабинета" : "На главную"}</Link>
      </div>
    </div>
  );
}
