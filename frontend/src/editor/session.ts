// 会话级常量：后端地址、taskKey、视频 id，以及「返回首页」。
// 桌面端 BACKEND 与 key 都来自主进程的 config.json（网页版是 config.js + localStorage）。

let backend = "";
let taskKey = "";
let vid = "";
let smokeMode = false;

export const getVid = () => vid;
export const isSmokeMode = () => smokeMode;
export const authHeaders = (): Record<string, string> => ({ Authorization: `Bearer ${taskKey}` });
export const apiUrl = (path: string) => backend + path;

// 编辑器是独立窗口，「返回首页」= 关掉本窗口，主进程负责把一直活着的主页显示出来。
// leaving 用来让 beforeunload 放行：用户已经在确认框里选过怎么收场了，再拦一次就是
// 点了「不保存，直接返回」没反应。
let leaving = false;
export const isLeaving = () => leaving;

/** notice 转交给主页：{error} 让它弹提示，{unauthorized} 让它先切回登录页再刷新 */
export function backHome(notice: { error?: string; unauthorized?: boolean } = {}) {
  leaving = true;
  void window.desktop.closeEditor(notice);
}

/** 读配置与运行时信息。返回 false 表示缺 vid 或 key，调用方应直接回首页 */
export async function initSession(): Promise<boolean> {
  const cfg = await window.desktop.getConfig();
  backend = (cfg.backend || "").replace(/\/+$/, "");
  taskKey = cfg.taskKey || "";
  vid = new URLSearchParams(location.search).get("v") || "";
  const runtime = await window.desktop.runtimeInfo();
  smokeMode = !!runtime.smokeMode;
  if (!vid || !taskKey) {
    backHome({ unauthorized: !taskKey });
    return false;
  }
  return true;
}

/** 启动期的未捕获错误上报给主进程（冒烟测试靠它判断渲染进程是否活着） */
export function reportBootError(error: unknown) {
  window.desktop?.reportPlaybackProbe?.({
    ready: false, seeked: false, duration: 0, currentTime: 0, width: 0, height: 0,
    error: "formal editor: " + ((error as Error)?.message || String(error) || "unknown error"),
  });
}
