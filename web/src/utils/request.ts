import axios, {
  type AxiosError,
  type AxiosInstance,
  type AxiosRequestConfig,
} from 'axios';
import { message } from 'antd';

/** Application codes mirroring internal/pkg/errcode. */
export const Code = {
  OK: 200,
  BadRequest: 400,
  Unauthorized: 401,
  Forbidden: 403,
  NotFound: 404,
  Conflict: 409,
  /** The preview no longer matches current data; re-preview before applying. */
  StaleData: 412,
  /** The capability's risk level exceeds the caller's ceiling. */
  RiskExceeded: 423,
  TooManyRequests: 429,
  InternalError: 500,
} as const;

/** Envelope returned by every endpoint. */
export interface Envelope<T = unknown> {
  code: number;
  msg: string;
  data?: T;
}

export interface PageData<T> {
  rows: T[];
  total: number;
  pageNum: number;
  pageSize: number;
}

/** Detail payload accompanying a 423. */
export interface RiskExceededDetail {
  capability: string;
  risk: string;
  allowed: string;
  hint: string;
  path?: string;
}

/** Error thrown for any non-success envelope, carrying the code and detail. */
export class ApiError extends Error {
  constructor(
    readonly code: number,
    message: string,
    readonly detail?: unknown,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

const TOKEN_KEY = 'intent-admin:token';

export const auth = {
  get: () => localStorage.getItem(TOKEN_KEY) ?? '',
  set: (token: string) => localStorage.setItem(TOKEN_KEY, token),
  clear: () => localStorage.removeItem(TOKEN_KEY),
};

/**
 * In-flight request de-duplication.
 *
 * Double-submitted writes are the common cause of duplicate rows in admin
 * systems: a user double-clicks, or a React effect fires twice under
 * StrictMode. Collapsing identical requests within a short window is cheaper
 * and more reliable than disabling every button by hand.
 */
const inflight = new Map<string, Promise<unknown>>();
const DEDUPE_WINDOW_MS = 500;

function keyOf(config: AxiosRequestConfig): string {
  return [
    config.method,
    config.url,
    JSON.stringify(config.params ?? {}),
    JSON.stringify(config.data ?? {}),
  ].join('|');
}

export const http: AxiosInstance = axios.create({
  baseURL: import.meta.env.VITE_API_BASE ?? '/api',
  timeout: 30_000,
});

http.interceptors.request.use((config) => {
  const token = auth.get();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

http.interceptors.response.use(
  (response) => {
    // Binary payloads (exports) have no envelope.
    if (response.config.responseType === 'blob') {
      return response;
    }
    const body = response.data as Envelope;
    if (body?.code === Code.OK) {
      return body.data as never;
    }
    throw new ApiError(body?.code ?? Code.InternalError, body?.msg ?? '请求失败', body?.data);
  },
  (error: AxiosError<Envelope>) => {
    const status = error.response?.status;
    const body = error.response?.data;
    const code = body?.code ?? status ?? Code.InternalError;
    const msg = body?.msg ?? error.message ?? '网络异常';
    throw new ApiError(code, msg, body?.data);
  },
);

/** Options controlling automatic error reporting. */
export interface CallOptions extends AxiosRequestConfig {
  /** Suppress the automatic message.error toast; the caller handles it. */
  silent?: boolean;
}

/**
 * request performs a call, applies de-duplication and reports errors.
 *
 * 412 and 423 are deliberately NOT toasted generically: both carry structured
 * detail that the caller needs to act on — re-running a preview, or offering a
 * link to the page where a human can perform the operation.
 */
export async function request<T = unknown>(config: CallOptions): Promise<T> {
  const { silent, ...rest } = config;
  const key = keyOf(rest);
  const existing = inflight.get(key);
  if (existing) return existing as Promise<T>;

  const promise = (http.request(rest) as unknown as Promise<T>)
    .catch((err: unknown) => {
      if (err instanceof ApiError) {
        handle(err, silent);
      }
      throw err;
    })
    .finally(() => {
      setTimeout(() => inflight.delete(key), DEDUPE_WINDOW_MS);
    });

  inflight.set(key, promise);
  return promise;
}

function handle(err: ApiError, silent?: boolean) {
  switch (err.code) {
    case Code.Unauthorized:
      auth.clear();
      if (!location.pathname.startsWith('/login')) {
        location.replace(`/login?redirect=${encodeURIComponent(location.pathname)}`);
      }
      return;
    case Code.Forbidden:
      if (!silent) message.error(err.message || '没有权限');
      return;
    case Code.StaleData:
    case Code.RiskExceeded:
      // Handled by the caller, which has the UI context to explain it.
      return;
    default:
      if (!silent) message.error(err.message || '请求失败');
  }
}

export const api = {
  get: <T>(url: string, params?: unknown, opts?: CallOptions) =>
    request<T>({ ...opts, method: 'GET', url, params }),
  post: <T>(url: string, data?: unknown, opts?: CallOptions) =>
    request<T>({ ...opts, method: 'POST', url, data }),
  put: <T>(url: string, data?: unknown, opts?: CallOptions) =>
    request<T>({ ...opts, method: 'PUT', url, data }),
  del: <T>(url: string, opts?: CallOptions) =>
    request<T>({ ...opts, method: 'DELETE', url }),
};

/**
 * download fetches a binary response and saves it, honouring the filename the
 * server put in Content-Disposition (RFC 5987 `filename*`, which is what the
 * export handler emits so Chinese names survive).
 */
export async function download(
  url: string,
  data?: unknown,
  fallbackName = 'export.xlsx',
): Promise<void> {
  const response = await http.request({
    method: 'POST',
    url,
    data,
    responseType: 'blob',
  });
  const disposition = String(response.headers?.['content-disposition'] ?? '');
  const name = parseFilename(disposition) ?? fallbackName;

  const href = URL.createObjectURL(response.data as Blob);
  const a = document.createElement('a');
  a.href = href;
  a.download = name;
  document.body.append(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(href);
}

function parseFilename(disposition: string): string | undefined {
  const star = /filename\*=UTF-8''([^;]+)/i.exec(disposition);
  if (star) return decodeURIComponent(star[1]);
  const plain = /filename="?([^";]+)"?/i.exec(disposition);
  return plain?.[1];
}
