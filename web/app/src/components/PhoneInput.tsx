// Поле белорусского номера: «+375» стоит всегда и не стирается, человек вводит только 9 цифр.
// Вставка понимает любые записи: +375 (29) 123-45-67, +375(29)1234567, 375 29 123 45 67, 8 029 123 45 67, 291234567.
// Backspace стирает цифру, даже если перед курсором скобка, пробел или дефис.
import { useLayoutEffect, useRef, type InputHTMLAttributes, type KeyboardEvent } from "react";

/** Девять цифр номера без кода страны из того, что ввели или вставили. */
export function phoneDigits(raw: string): string {
  let d = raw.replace(/\D/g, "");
  // лишние цифры спереди — это код страны или «8 0»: срезаем, только когда номер длиннее 9 цифр,
  // иначе при наборе по одной цифре «37» превращалось бы в пустоту
  if (d.length > 9 && d.startsWith("375")) d = d.slice(3);
  else if (d.length > 9 && d.startsWith("80")) d = d.slice(2);
  if (d.length > 9 && d.startsWith("0")) d = d.slice(1);
  return d.slice(0, 9);
}

/** (29) 123-45-67 — по мере ввода. */
export function formatNational(d: string): string {
  if (!d) return "";
  let out = "(" + d.slice(0, 2);
  if (d.length >= 2) out += ") ";
  out += d.slice(2, 5);
  if (d.length > 5) out += "-" + d.slice(5, 7);
  if (d.length > 7) out += "-" + d.slice(7, 9);
  return out;
}

/** Номер целиком для сервера: +375291234567. */
export const fullPhone = (d: string) => (d ? "+375" + d : "");

/** Позиция в отформатированной строке сразу после n-й цифры. */
function caretAfter(formatted: string, n: number) {
  if (n <= 0) return formatted.startsWith("(") ? 1 : 0;
  let seen = 0;
  for (let i = 0; i < formatted.length; i++) {
    if (/\d/.test(formatted[i]) && ++seen === n) return i + 1;
  }
  return formatted.length;
}

type Props = Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange"> & {
  value: string; // 9 цифр
  onChange: (digits: string) => void;
  invalid?: boolean;
};

export function PhoneInput({ value, onChange, invalid, ...rest }: Props) {
  const ref = useRef<HTMLInputElement>(null);
  const caret = useRef<number | null>(null); // сколько цифр должно оказаться перед курсором после обновления
  const shown = formatNational(value);

  useLayoutEffect(() => {
    const el = ref.current;
    if (el && caret.current !== null && document.activeElement === el) {
      const p = caretAfter(shown, caret.current);
      el.setSelectionRange(p, p);
    }
    caret.current = null;
  }, [shown]);

  const change = (raw: string, digitsBeforeCaret: number) => {
    const d = phoneDigits(raw);
    // при вставке с кодом страны цифры спереди срезались — курсор ставим в конец вставленного
    caret.current = Math.min(d.length, digitsBeforeCaret - (raw.replace(/\D/g, "").length - d.length));
    onChange(d);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    rest.onKeyDown?.(e);
    const el = e.currentTarget;
    const pos = el.selectionStart ?? 0;
    if (pos !== el.selectionEnd) return; // выделение — стандартное удаление
    if (e.key === "Backspace" && pos > 0 && /\D/.test(el.value[pos - 1])) {
      // перед курсором скобка, пробел или дефис — стираем ближайшую цифру слева
      e.preventDefault();
      const n = el.value.slice(0, pos).replace(/\D/g, "").length;
      if (n === 0) return;
      caret.current = n - 1;
      onChange(value.slice(0, n - 1) + value.slice(n));
    } else if (e.key === "Delete" && pos < el.value.length && /\D/.test(el.value[pos])) {
      e.preventDefault();
      const n = el.value.slice(0, pos).replace(/\D/g, "").length;
      caret.current = n;
      onChange(value.slice(0, n) + value.slice(n + 1));
    }
  };

  return (
    <div className="phone-input">
      <span className="phone-cc" aria-hidden>+375</span>
      <input
        ref={ref}
        className="input"
        type="tel"
        inputMode="tel"
        autoComplete="tel"
        placeholder="(29) 123-45-67"
        aria-invalid={invalid || undefined}
        {...rest}
        value={shown}
        onKeyDown={onKeyDown}
        onChange={(e) => {
          const el = e.target;
          const before = el.value.slice(0, el.selectionStart ?? el.value.length).replace(/\D/g, "").length;
          change(el.value, before);
        }}
      />
    </div>
  );
}
