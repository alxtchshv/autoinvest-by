import { useState } from "react";
import { PlanTile } from "../../components/plans";
import { Link, useNavigate } from "react-router-dom";
import { api } from "../../lib/api";
import { byn, planName } from "../../lib/format";
import { useSubmit, useTitle } from "../../lib/hooks";
import { useSession } from "../../lib/session";
import type { Plan, Strategy } from "../../lib/types";
import { Button, Notice } from "../../components/ui";

const QUESTIONS: [string, string[]][] = [
  ["Какую сумму вам комфортно откладывать в месяц?", ["До 50 ⃅", "50–200 ⃅", "От 200 ⃅"]],
  ["Когда могут понадобиться эти деньги?", ["В ближайшие полгода", "Через 1–3 года", "Не раньше чем через 3 года"]],
  ["Рынок упал на 15% за неделю. Что вы сделаете?", ["Заберу деньги, пока не стало хуже", "Спокойно пережду", "Докуплю ещё по низкой цене"]],
  ["На что копите?", ["Подушка безопасности", "Крупная покупка: авто, техника", "Капитал на будущее, пенсия"]],
];

type Rec = {
  strategy: string; risk_profile: string; amount_kop: number; frequency: string; name: string; icon: string;
  reasons: string[]; monthly_kop: number; goal_comment: string;
};

export default function Onboarding() {
  useTitle("Подбор копилки");
  const { me, reload } = useSession();
  const nav = useNavigate();
  const [step, setStep] = useState(0);
  const [answers, setAnswers] = useState<number[]>([]);
  const [result, setResult] = useState<{ recommendation: Rec; strategy: Strategy } | null>(null);
  const quiz = useSubmit();
  const create = useSubmit();

  const choose = async (value: number) => {
    const next = [...answers.slice(0, step), value];
    setAnswers(next);
    if (step < QUESTIONS.length - 1) {
      setStep(step + 1);
      return;
    }
    try {
      const r = await quiz.submit(() => api.post<{ recommendation: Rec; strategy: Strategy }>("/me/onboarding", { answers: next }));
      if (r) {
        setResult(r);
        reload();
      }
    } catch {
      /* ошибка показана в quiz.error */
    }
  };

  const skip = async () => {
    await api.post("/me/onboarding/skip");
    await reload(); // иначе кабинет снова отправит на анкету
    nav("/app", { replace: true });
  };

  const createPlan = async () => {
    if (!result) return;
    const rec = result.recommendation;
    try {
      const r = await create.submit(() => api.post<{ plan: Plan; waitlist?: { position: number; first?: boolean } }>("/plans", {
        name: rec.name, icon: rec.icon, amount_kop: rec.amount_kop, frequency: rec.frequency, weekday: 5, run_minute: 600, strategy: rec.strategy,
      }));
      if (r?.waitlist) {
        // до запуска: копилка сохранена, человек в листе ожидания — приветственное окно с предложением демо-счёта
        await reload();
        nav(`/app/plans/${r.plan.id}`, { replace: true });
        window.dispatchEvent(new CustomEvent("ai:waitlist", { detail: { position: r.waitlist.position, first: r.waitlist.first } }));
        return;
      }
      if (r) nav(me?.cards ? `/app/plans/${r.plan.id}` : "/app/cards", { replace: true });
    } catch {
      /* ошибка показана в create.error */
    }
  };

  if (result) {
    const { recommendation: rec, strategy } = result;
    return (
      <div className="onb">
        <section className="sheet sheet-pad stack g16">
          <div className="small muted">Подходящая копилка «{rec.name}»</div>
          <h1>{planName(strategy.name)}</h1>
          <PlanTile s={strategy} selected />
          <div className="onb-amount">{byn(rec.amount_kop)} каждую пятницу <span className="muted">≈ {byn(rec.monthly_kop)} в месяц</span></div>
          <ul className="onb-reasons">{rec.reasons.map((r) => <li key={r}>{r}</li>)}</ul>
          {rec.goal_comment && <Notice kind="info">{rec.goal_comment}</Notice>}
          {!me?.cards && <Notice kind="info">Карты ещё нет — копилка создастся на паузе, а мы сразу предложим привязать карту.</Notice>}
          {create.error && <Notice kind="error">{create.error}</Notice>}
          <div className="row g8 wrap">
            <Button variant="primary" size="lg" busy={create.busy} onClick={createPlan}>Создать эту копилку в 1 клик</Button>
            <Link to={`/app/plans/new?strategy=${strategy.id}`} className="btn btn-secondary btn-lg">Настроить самому</Link>
          </div>
          <p className="tiny muted">Это справочный подбор по вашим ответам, а не индивидуальная инвестиционная рекомендация. Сумму, расписание и состав можно поменять в любой момент.</p>
        </section>
      </div>
    );
  }

  const [question, options] = QUESTIONS[step];
  return (
    <div className="onb">
      <section className="sheet sheet-pad stack g16">
        <div className="row between small muted">
          <span>Вопрос {step + 1} из {QUESTIONS.length}</span>
          <button type="button" className="linkish" onClick={skip}>Пропустить</button>
        </div>
        <div className="bar" aria-hidden><span style={{ width: `${(step / QUESTIONS.length) * 100}%`, background: "var(--ink)" }} /></div>
        <h1>{question}</h1>
        <div className="stack g8" role="radiogroup" aria-label={question}>
          {options.map((o, i) => (
            <button key={o} type="button" role="radio" aria-checked={answers[step] === i + 1} className="onb-option" disabled={quiz.busy} onClick={() => choose(i + 1)}>{o}</button>
          ))}
        </div>
        {quiz.error && <Notice kind="error">{quiz.error}</Notice>}
        {step > 0 && <div><Button variant="ghost" size="sm" onClick={() => setStep(step - 1)}>Назад</Button></div>}
      </section>
    </div>
  );
}
