// Тонкий клиент к API: cookie-сессия + CSRF-заголовок, ошибки с полем формы.

export class ApiError extends Error {
  status: number;
  field?: string;
  code?: string;
  constructor(status: number, message: string, field?: string, code?: string) {
    super(message);
    this.status = status;
    this.field = field;
    this.code = code;
  }
}

function csrf(): string {
  const m = document.cookie.match(/(?:^|;\s*)ai_csrf=([^;]+)/);
  return m ? decodeURIComponent(m[1]) : "";
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  let payload: BodyInit | undefined;
  if (body instanceof FormData) {
    payload = body;
  } else if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  if (method !== "GET") headers["X-CSRF-Token"] = csrf();
  let res: Response;
  try {
    res = await fetch("/api/v1" + path, { method, headers, body: payload, credentials: "same-origin" });
  } catch {
    throw new ApiError(0, "Нет связи с сервером. Проверьте интернет");
  }
  const text = await res.text();
  let data: any = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = null;
  }
  if (!res.ok) {
    if (res.status === 401 && path !== "/auth/me" && !path.startsWith("/auth/")) {
      window.dispatchEvent(new CustomEvent("ai:unauthorized"));
    }
    // до запуска оплаты закрыты: показываем окно листа ожидания вместо ошибки
    if (data?.code === "waitlist") {
      window.dispatchEvent(new CustomEvent("ai:waitlist", { detail: { position: data.position } }));
      throw new ApiError(res.status, "", undefined, "waitlist");
    }
    throw new ApiError(res.status, data?.error || "Ошибка " + res.status, data?.field, data?.code);
  }
  return data as T;
}

export const api = {
  get: <T,>(p: string) => request<T>("GET", p),
  post: <T,>(p: string, b: unknown = {}) => request<T>("POST", p, b),
  patch: <T,>(p: string, b: unknown) => request<T>("PATCH", p, b),
  del: <T,>(p: string) => request<T>("DELETE", p),
};

export function idemKey(): string {
  return crypto.randomUUID ? crypto.randomUUID() : String(Date.now()) + Math.random().toString(16).slice(2);
}
