import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError } from "./api";

/** Загрузка данных с повтором и перезапросом. */
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [loading, setLoading] = useState(true);
  const seq = useRef(0);
  const run = useCallback(async (quiet = false) => {
    const id = ++seq.current;
    if (!quiet) setLoading(true);
    try {
      const d = await fn();
      if (id === seq.current) {
        setData(d);
        setError(null);
      }
    } catch (e) {
      if (id === seq.current) setError(e instanceof ApiError ? e : new ApiError(0, String(e)));
    } finally {
      if (id === seq.current) setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  useEffect(() => {
    run();
  }, [run]);
  return { data, error, loading, reload: () => run(true), setData };
}

/** Состояние отправки формы с ошибками по полям. */
export function useSubmit() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [field, setField] = useState<string | null>(null);
  const submit = useCallback(async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
    setBusy(true);
    setError(null);
    setField(null);
    try {
      return await fn();
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError(0, String(e));
      setError(err.message);
      setField(err.field || null);
      throw err;
    } finally {
      setBusy(false);
    }
  }, []);
  const fieldError = (name: string) => (field === name ? error : null);
  const reset = () => {
    setError(null);
    setField(null);
  };
  return { busy, error, field, submit, fieldError, reset, setError };
}

export function useTitle(t: string) {
  useEffect(() => {
    document.title = t ? `${t} — AutoInvest` : "AutoInvest";
  }, [t]);
}
