import { createStore } from '../lib/createStore';
import { ApiError, apiGet, configureApi, setUnauthorizedHandler } from '../lib/apiClient';
import { confirm, toast } from '../lib/notify';
import { closeAllTransient, setSettingsOpen } from './uiStore';
import { refresh, refreshCached, resetLibrary, stopPolling } from './libraryStore';
import type { AppConfig, AppPhase, KeybarState } from '../types';


interface SessionState {
  /** App配置 */
  cfg: AppConfig | null;
  /** 主页加载状态 */
  appPhase: AppPhase;
  /** 登录界面提示 */
  loginHint: string | null;
  /** 已登录 */
  loggingIn: boolean;
  /** 是否有有效的 Key */
  keyOK: boolean;
  /** 重新登录提示条 */
  keybar: KeybarState | null;
}

export const sessionStore = createStore<SessionState>({
  cfg: null,
  appPhase: "boot",
  loginHint: null,
  loggingIn: false,
  keyOK: false,
  keybar: null,
});

// 三态切换靠 body 的 data-app 驱动 CSS（#boot / .topbar / .wall / #drop 的显隐都读它），
// 这些元素不受 React 条件渲染控制，属性得手动同步
document.body.dataset.app = sessionStore.get().appPhase;

export function setCfg(cfg: AppConfig | null) {
  configureApi(cfg);
  sessionStore.set({ cfg });
}

/** 设置主页加载阶段 */
export function setAppPhase(appPhase: AppPhase) {
  if (sessionStore.get().appPhase === appPhase) return;
  sessionStore.set({ appPhase });
  document.body.dataset.app = appPhase;
  // 非 ready 时收起所有临时弹窗
  if (appPhase !== "ready") closeAllTransient();
}

/** 设置 Key 有效状态 */
export const setKeyOK = (keyOK: boolean) => sessionStore.set({ keyOK });

/** 显示登录界面 */
export function showLogin(message?: string | null) {
  sessionStore.set({ loginHint: message || null });
  setAppPhase("login");
}

/** 显示重新登录按钮 */
export const showKeybar = (msg: string | null, fix: boolean) =>
  sessionStore.set({ keybar: msg ? { msg, fix: !!fix } : null });

/** 主页是否是登录后阶段 */
export const isReady = () => sessionStore.get().appPhase === "ready";

setUnauthorizedHandler(showLogin);

export async function tryLogin(key: string): Promise<{ ok: boolean; message?: string }> {
  if (!key) return { ok: false, message: "请输入 key" };
  sessionStore.set({ loggingIn: true });
  try {
    // 拿用户刚输入的这把 key 试，不动当前配置——验不过就不该覆盖已存的那把
    await apiGet("/edit/state", { key, handleUnauthorized: false });
    setCfg(await window.desktop.setConfig({ taskKey: key }));
    setAppPhase("ready");
    await refresh();
    return { ok: true };
  } catch (e) {
    if (e instanceof ApiError && (e.status === 401 || e.status === 403)) return { ok: false, message: "key 无效" };
    if (e instanceof ApiError && e.kind === "http") return { ok: false, message: "连不上后端：" + e.message };
    return { ok: false, message: e instanceof Error ? e.message : "连不上后端：未知错误" };
  } finally {
    sessionStore.set({ loggingIn: false });
  }
}

export async function saveSettings({ cacheDir, cacheLimitGB }: { cacheDir: string; cacheLimitGB: number | string }) {
  if (cacheDir && cacheDir !== sessionStore.get().cfg?.cacheDir) {
    toast("正在迁移缓存目录…");
    const r = await window.desktop.migrateCacheDir(cacheDir);
    if (r.kept) toast(`${r.kept} 个文件搬不动，留在原目录`, true);
  }
  setCfg(await window.desktop.setConfig({ cacheLimitGB: Math.max(1, Number(cacheLimitGB) || 20) }));
  setSettingsOpen(false);
  toast("设置已保存");
  refresh();
}

/** 返回是否真的清了（用户可能取消），设置弹窗据此决定要不要重新读占用 */
export async function clearVideoCache() {
  if (!await confirm("删除全部本地视频缓存副本？\n字幕在云端不受影响，视频可重新定位或从云端取回。", "全部删除")) return false;
  await window.desktop.clearCache();
  await refreshCached();
  toast("缓存已清空");
  return true;
}

export async function logout() {
  if (!await confirm("退出登录？\n本地视频库与缓存保留，云端字幕不受影响。", "退出登录")) return;
  setCfg(await window.desktop.setConfig({ taskKey: "" }));
  setKeyOK(false);
  resetLibrary();   // 下次登录进来还得先铺加载态，别拿这次的空 remote 当结论
  stopPolling();
  showLogin();
}
