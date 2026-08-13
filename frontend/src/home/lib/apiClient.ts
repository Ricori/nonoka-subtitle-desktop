import { beginLoading } from './notify';

// 后端请求的唯一入口

let backend = "";
let taskKey = "";
let onUnauthorized: ((message: string) => void) | null = null;

const API_LOADING: Record<string, string> = {
  "/edit/video/stop": "正在停止任务…",
  "/edit/video/rename": "正在重命名…",
  "/edit/video/delete": "正在删除云端字幕…",
};

/** cfg 变化时调用，重设后端和请求 key */
export function configureApi(cfg: { backend?: string; taskKey?: string } | null) {
  backend = (cfg?.backend || "").replace(/\/+$/, "");
  taskKey = cfg?.taskKey || "";
}

export const apiBase = () => backend;
export const hasKey = () => !!taskKey;

/** 401/403 的统一去向（sessionStore 注册为「退回登录页」） */
export function setUnauthorizedHandler(fn: (message: string) => void) {
  onUnauthorized = fn;
}

export const UNAUTHORIZED_MESSAGE = "key 已失效，请重新输入。";

export class ApiError extends Error {
  /** 0 表示压根没收到响应 */
  status: number;
  kind: "network" | "timeout" | "http";
  constructor(message: string, kind: ApiError["kind"], status = 0) {
    super(message);
    this.name = "ApiError";
    this.kind = kind;
    this.status = status;
  }
}

export const isUnauthorized = (e: unknown) =>
  e instanceof ApiError && (e.status === 401 || e.status === 403);

export interface ApiOptions {
  method?: "GET" | "POST" | "PUT";
  body?: unknown;
  /** 超时时间（毫秒） */
  timeout?: number;
  /** 覆盖当前 key（登录校验时拿用户刚输的） */
  key?: string;
  /** 命中 utils.API_LOADING 的路径会自动挂忙碌遮罩，传 false 关掉 */
  loading?: boolean;
  /** 传 false 时 401 只抛错、不触发退回登录页（调用方自己决定怎么处理） */
  handleUnauthorized?: boolean;
  keepalive?: boolean;
}

export async function api<T = any>(path: string, opts: ApiOptions = {}): Promise<T> {
  const { method = "GET", body, timeout, key, loading = true, handleUnauthorized = true, keepalive } = opts;

  const headers: Record<string, string> = { Authorization: "Bearer " + (key ?? taskKey) };
  if (body !== undefined) headers["Content-Type"] = "application/json";

  const endLoading = loading && API_LOADING[path] ? beginLoading(API_LOADING[path]) : null;
  let r: Response;
  try {
    r = await fetch(backend + path, {
      method,
      headers,
      keepalive,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: timeout ? AbortSignal.timeout(timeout) : undefined,
    });
  } catch (e: any) {
    if (e?.name === "TimeoutError") {
      throw new ApiError(`连接后端超时（${Math.round((timeout || 0) / 1000)} 秒），请检查网络后重试。`, "timeout");
    }
    throw new ApiError("连不上后端，请检查网络", "network");
  } finally {
    endLoading?.();
  }

  if (r.status === 401 || r.status === 403) {
    if (handleUnauthorized) onUnauthorized?.(UNAUTHORIZED_MESSAGE);
    throw new ApiError(UNAUTHORIZED_MESSAGE, "http", r.status);
  }

  const d = await r.json().catch(() => ({} as any));
  if (!r.ok) throw new ApiError(d?.detail || `HTTP ${r.status}`, "http", r.status);
  return d as T;
}

export const apiGet = <T = any>(path: string, opts: Omit<ApiOptions, "method" | "body"> = {}) =>
  api<T>(path, { ...opts, method: "GET" });

export const apiPost = <T = any>(path: string, body: unknown, opts: Omit<ApiOptions, "method" | "body"> = {}) =>
  api<T>(path, { ...opts, method: "POST", body });

export const apiPut = <T = any>(path: string, body: unknown, opts: Omit<ApiOptions, "method" | "body"> = {}) =>
  api<T>(path, { ...opts, method: "PUT", body });
