import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { api } from "./api";
import type { Config, Me, Notification, Quote, Rates } from "./types";

// ---------------------------------------------------------------- сессия и конфиг

type SessionCtx = {
  config: Config | null;
  me: Me | null;
  loading: boolean;
  reload: () => Promise<Me | null>;
  setMe: (m: Me) => void;
};

const SCtx = createContext<SessionCtx>(null!);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [config, setConfig] = useState<Config | null>(null);
  const [me, setMe] = useState<Me | null>(null);
  const [loading, setLoading] = useState(true);

  const reload = useCallback(async () => {
    try {
      const m = await api.get<Me>("/auth/me");
      setMe(m);
      return m;
    } catch {
      setMe({ user: null });
      return null;
    }
  }, []);

  useEffect(() => {
    (async () => {
      try {
        setConfig(await api.get<Config>("/config"));
      } catch {
        /* покажем ошибку на страницах */
      }
      await reload();
      setLoading(false);
    })();
    const onUnauth = () => setMe({ user: null });
    window.addEventListener("ai:unauthorized", onUnauth);
    return () => window.removeEventListener("ai:unauthorized", onUnauth);
  }, [reload]);

  const value = useMemo(() => ({ config, me, loading, reload, setMe }), [config, me, loading, reload]);
  return <SCtx.Provider value={value}>{children}</SCtx.Provider>;
}

export const useSession = () => useContext(SCtx);

// ---------------------------------------------------------------- живые котировки (SSE)

type MarketCtx = {
  quotes: Record<string, Quote>;
  rates: Rates | null;
  connected: boolean;
  lastTick: number;
  onUserEvent: (fn: (type: string, data: any) => void) => () => void;
};

const MCtx = createContext<MarketCtx>(null!);

export function MarketProvider({ children, userId }: { children: ReactNode; userId: number | null }) {
  const [quotes, setQuotes] = useState<Record<string, Quote>>({});
  const [rates, setRates] = useState<Rates | null>(null);
  const [connected, setConnected] = useState(false);
  const [lastTick, setLastTick] = useState(0);
  const listeners = useRef(new Set<(type: string, data: any) => void>());

  useEffect(() => {
    let es: EventSource | null = null;
    let retry: number | undefined;
    const open = () => {
      es = new EventSource("/api/v1/stream");
      es.onopen = () => setConnected(true);
      es.onerror = () => {
        setConnected(false);
        es?.close();
        retry = window.setTimeout(open, 4000);
      };
      es.addEventListener("quotes", (e) => {
        const d = JSON.parse((e as MessageEvent).data) as { quotes: Quote[]; rates: Rates };
        const m: Record<string, Quote> = {};
        for (const q of d.quotes || []) m[q.symbol] = q;
        setQuotes(m);
        if (d.rates?.usd) setRates(d.rates);
        setLastTick(Date.now());
      });
      for (const t of ["notification", "plans"]) {
        es.addEventListener(t, (e) => {
          const data = (e as MessageEvent).data ? JSON.parse((e as MessageEvent).data) : null;
          listeners.current.forEach((fn) => fn(t, data));
        });
      }
    };
    open();
    return () => {
      window.clearTimeout(retry);
      es?.close();
    };
  }, [userId]);

  const onUserEvent = useCallback((fn: (type: string, data: any) => void) => {
    listeners.current.add(fn);
    return () => {
      listeners.current.delete(fn);
    };
  }, []);

  const value = useMemo(() => ({ quotes, rates, connected, lastTick, onUserEvent }), [quotes, rates, connected, lastTick, onUserEvent]);
  return <MCtx.Provider value={value}>{children}</MCtx.Provider>;
}

export const useMarket = () => useContext(MCtx);

/** Подписка на личное событие (notification | plans). */
export function useUserEvent(type: string, fn: (data: any) => void) {
  const { onUserEvent } = useMarket();
  const ref = useRef(fn);
  ref.current = fn;
  useEffect(() => onUserEvent((t, d) => t === type && ref.current(d)), [onUserEvent, type]);
}

export type { Notification };
