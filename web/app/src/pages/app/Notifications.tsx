import { useNavigate } from "react-router-dom";
import { Bell } from "lucide-react";
import { api } from "../../lib/api";
import { dateTime } from "../../lib/format";
import { useLoad, useTitle } from "../../lib/hooks";
import { useSession, useUserEvent } from "../../lib/session";
import type { Notification } from "../../lib/types";
import { Button, Empty, LoadError, PageLoader } from "../../components/ui";

export default function Notifications() {
  useTitle("Уведомления");
  const nav = useNavigate();
  const { reload: reloadMe } = useSession();
  const { data, error, loading, reload } = useLoad(() => api.get<{ notifications: Notification[]; unread: number }>("/notifications?limit=100"), []);
  useUserEvent("notification", reload);
  if (loading && !data) return <PageLoader />;
  if (error) return <LoadError error={error} retry={reload} />;
  const read = async (id = 0) => { await api.post("/notifications/read", { id }); reload(); reloadMe(); };
  return (
    <div>
      <div className="page-head">
        <div><h1>Уведомления</h1><p>{data!.unread ? `Непрочитанных: ${data!.unread}` : "Всё прочитано"}</p></div>
        {data!.unread > 0 && <Button onClick={() => read()}>Прочитать все</Button>}
      </div>
      <div className="sheet">
        {data!.notifications.length === 0 ? <Empty icon={<Bell className="ico" />} title="Уведомлений нет">Здесь будут чеки о пополнениях, выводах и ответы поддержки.</Empty> : (
          <div className="list">
            {data!.notifications.map((n) => (
              <button key={n.id} className="list-row" style={{ alignItems: "flex-start" }} onClick={() => { read(n.id); if (n.link) nav(n.link); }}>
                {!n.read ? <span className="dot" style={{ background: "var(--brand)", marginTop: 8 }} /> : <span style={{ width: 8 }} />}
                <div className="grow">
                  <div className="title" style={{ fontWeight: n.read ? 500 : 620 }}>{n.title}</div>
                  {n.body && <div className="sub">{n.body}</div>}
                </div>
                <span className="end tiny muted">{dateTime(n.created_at)}</span>
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
