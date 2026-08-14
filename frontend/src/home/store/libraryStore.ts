import { useMemo } from 'react';
import { createStore } from '../lib/createStore';
import { ApiError, apiGet, apiPost, hasKey } from '../lib/apiClient';
import { confirm, promptText, toast } from '../lib/notify';
import { RUNNING, availKind, errText, localKey, mergeLibrary } from '../utils';
import { uiStore } from './uiStore';
import { isReady, setAppPhase, setKeyOK, sessionStore, showKeybar, showLogin } from './sessionStore';
import type { LibraryEntry, MergedVideoItem, RemoteVideo } from '../types';

interface LibraryState {
  /** 本地库条目：本机导入的、还没转写的。服务端记录里没有的，merged 里也会有 */
  lib: LibraryEntry[];
  /** 云端视频记录 */
  remote: RemoteVideo[];
  /** 本地库条目里哪些是有缓存的 */
  cachedSet: Set<string>;
  /** 本地库条目里哪些源文件还在 */
  srcSet: Set<string>;
  /** 是否有其他任务在跑 */
  busyOther: boolean;
  /** 是否完成首次状态请求 */
  loaded: boolean;
  // 合并条目：服务端记录 ∪ 本地尚未转写的导入条目
  merged: MergedVideoItem[];
  // 该 key 在云端（其他平台）是否已有进行中任务
  cloudBusy: boolean;
}

/** 视频库 store */
export const libraryStore = createStore<LibraryState>({
  lib: [], remote: [], cachedSet: new Set(), srcSet: new Set(),
  busyOther: false, loaded: false, merged: [], cloudBusy: false,
});

/** 轮询定时器 */
let pollTimer: ReturnType<typeof setTimeout> | undefined;
/** 当前请求的Promise，没有就是 null */
let inflight: Promise<RefreshResult> | null = null;
/** 是否需要在当前请求完成后再次刷新 */
let refreshAgain = false;

type RefreshResult = { ready: boolean; loginMessage: string | null };

/** 写 lib/remote 一律走这里，自动重算 merged/cloudBusy */
function commit(patch: { lib?: LibraryEntry[]; remote?: RemoteVideo[] }) {
  const s = libraryStore.get();
  const lib = patch.lib ?? s.lib;
  const remote = patch.remote ?? s.remote;
  const merged = mergeLibrary(lib, remote);
  libraryStore.set({ lib, remote, merged, cloudBusy: merged.some(it => RUNNING.has(it.status)) });
}

export const setLib = (lib: LibraryEntry[]) => commit({ lib });

/**
 * 提交成功后换主键
 * 本地条目 id 已变成 video_id，此时 remote 里还没有这条，卡片会退回「未开始」直到 refresh 回来。
 * 乐观塞一条 queued 顶上，下一轮 /edit/state 回来时被真数据覆盖。
 */
export function markStarted(lib: LibraryEntry[], v: RemoteVideo) {
  const remote = libraryStore.get().remote;
  commit({ lib, remote: remote.some(r => r.video_id === v.video_id) ? remote : [...remote, v] });
}

/** 搜索 + 状态筛选 + 排序后的展示列表。只有 VideoWall 用得上，放在组件侧算，store 里只存完整的 merged */
export function useVisibleItems() {
  const merged = libraryStore.use(s => s.merged);
  const cachedSet = libraryStore.use(s => s.cachedSet);
  const srcSet = libraryStore.use(s => s.srcSet);
  const filter = uiStore.use(s => s.normalizedFilter);
  const avail = uiStore.use(s => s.avail);
  const sortMode = uiStore.use(s => s.sortMode);
  return useMemo(() => {
    const out = merged.filter(it => {
      if (filter && !it.title.toLowerCase().includes(filter)) return false;
      if (!avail.length) return true;
      const key = localKey(it);
      return avail.includes(availKind(it, cachedSet.has(key), srcSet.has(key)));
    });
    if (sortMode === "name") out.sort((a, b) => a.title.localeCompare(b.title, "zh"));
    else if (sortMode === "dur") out.sort((a, b) => (b.duration || 0) - (a.duration || 0));
    // sortMode "new"：merged 已按 addedAt 降序给好，不用再排
    return out;
  }, [merged, cachedSet, srcSet, filter, avail, sortMode]);
}

/** 单调递增，只有最后发起的那轮探测能写回结果——导入和轮询撞上时别让旧结果盖掉新的 */
let cachedProbeSeq = 0;

export async function refreshCached(list?: MergedVideoItem[]) {
  const items = list || libraryStore.get().merged;
  const seq = ++cachedProbeSeq;
  const [cached, srcs] = await Promise.all([
    Promise.all(items.map(it => window.desktop.hasCached(localKey(it)))),
    Promise.all(items.map(it => it.srcPath ? window.desktop.fileExists(it.srcPath) : Promise.resolve(false))),
  ]);
  if (seq !== cachedProbeSeq) return;
  libraryStore.set({
    cachedSet: new Set(items.filter((_, i) => cached[i]).map(localKey)),
    srcSet: new Set(items.filter((_, i) => srcs[i]).map(localKey)),
  });
}

/** 请求 key 的状态信息  */
async function fetchKeyState({ deferGate = false } = {}): Promise<string | null> {
  if (!hasKey()) {
    setKeyOK(false);
    if (!deferGate) showLogin();
    return null;   // 没 key 就不算「加载过」，下次登录进来还得先铺加载态
  }
  try {
    // 必须带超时：启动校验期间 appPhase 是 boot，整个界面都不渲染，这个请求一旦挂住就永远停在验证登录
    const d = await apiGet<{ videos?: RemoteVideo[]; busy_other?: boolean }>("/edit/state", {
      timeout: 20_000, handleUnauthorized: false,
    });
    setKeyOK(true);
    const remote = d.videos || [];
    // Go 端只认这四个字段，RemoteVideo 的其余部分不传
    const cloud = remote.map(v => ({
      video_id: v.video_id, title: v.title ?? "", fp: v.fp ?? "", created_at: v.created_at ?? 0,
    }));
    commit({ remote, lib: await window.desktop.syncCloudLibrary(cloud) });
    const busyOther = !!d.busy_other;
    libraryStore.set({ busyOther });
    showKeybar(busyOther ? "您的 key 在点播平台有任务进行中，此时无法提交新的任务。" : null, false);
    return null;
  } catch (e) {
    setKeyOK(false);
    if (e instanceof ApiError && (e.status === 401 || e.status === 403)) {
      if (!deferGate) showLogin(e.message);
      return e.message;
    }
    const msg = e instanceof ApiError && e.kind === "http"
      ? "连不上后端：" + e.message
      : (e as Error)?.message || "连不上后端：未知错误";
    if (isReady()) showKeybar(msg, true);
    else if (!deferGate) showLogin(msg);
    return msg;
  } finally {
    libraryStore.set({ loaded: true });
  }
}

/**
 * 刷新主页状态。passive 用于 focus/可见性/轮询这类被动触发，撞上在飞的请求就合流；
 * 改过数据的调用方（重命名、删除、流水线）不传它，仍旧排队重取。
 */
export function refresh(opts: { deferGate?: boolean; passive?: boolean } = {}): Promise<RefreshResult> {
  if (inflight) {
    if (!opts.passive) refreshAgain = true;
    return inflight;
  }
  inflight = doRefresh(opts.deferGate);
  return inflight;
}

async function doRefresh(deferGate = false): Promise<RefreshResult> {
  try {
    const loginMessage = await fetchKeyState({ deferGate });
    const ready = sessionStore.get().keyOK;
    if (ready && !deferGate) setAppPhase("ready");
    await refreshCached();   // store 写入是同步的，这里读到的已经是这一轮的 merged
    return { ready, loginMessage };
  } finally {
    inflight = null;
    // 轮询只在「有任务在跑」且「本窗口可见」时开着。必须在 finally 里排下一轮：
    // 中间任何一步抛了（hasCached 之类的 IPC 也会），轮询链就此断掉且再没人接得上
    clearTimeout(pollTimer);
    if (!document.hidden && libraryStore.get().cloudBusy) {
      pollTimer = setTimeout(() => refresh({ passive: true }), 20_000);
    }
    if (refreshAgain) { refreshAgain = false; refresh(); }
  }
}

export function stopPolling() {
  clearTimeout(pollTimer);
}

export function resetLibrary() {
  commit({ remote: [] });
  libraryStore.set({ loaded: false, busyOther: false });
}

/** 导入视频 */
export async function importPaths(paths: string[]) {
  if (!paths?.length) return;
  toast(`正在读取 ${paths.length} 个文件…`);
  const { added, failed } = await window.desktop.importVideos(paths);
  setLib(await window.desktop.getLibrary());
  // 必须先重算 srcSet/cachedSet
  await refreshCached();
  if (added.length) toast(`已导入 ${added.length} 个视频`);
  for (const f of failed) toast(`${f.name}：${f.error}`, true);
}

export const pickAndImport = async () => importPaths(await window.desktop.pickVideos());

/** 打开编辑器 */
export async function openEditor(it: MergedVideoItem) {
  try {
    await window.desktop.openEditor(it.id); // 独立窗口打开；本页不卸载，返回时原样还在
  } catch (e) {
    toast("打开编辑器失败：" + errText(e), true);
  }
}

// 查一下文件是否还在
export async function revealVideo(it: MergedVideoItem) {
  if (!it.srcPath || !await window.desktop.fileExists(it.srcPath)) { toast("源文件已被删除", true); return; }
  await window.desktop.revealInFolder(it.srcPath);
}

export async function renameVideo(it: MergedVideoItem) {
  const name = await promptText("重命名视频", "重命名", it.title);
  if (name == null) return;
  const trimmed = name.trim();
  if (!trimmed || trimmed === it.title) return;
  if (it.localId) await window.desktop.renameTitle(it.localId, trimmed);
  // 共享来的视频标题归所有者管，跳过同步，本地改名只影响自己这边怎么显示
  if (it.remote && !it.shared) {
    try { await apiPost("/edit/video/rename", { video_id: it.id, title: trimmed }); }
    catch (e) {
      toast(it.localId ? `本地已改名，同步云端失败：${errText(e)}` : `重命名失败：${errText(e)}`, true);
      if (!it.localId) return;
    }
  }
  setLib(await window.desktop.getLibrary());
  toast("已重命名");
  refresh();
}

export async function removeVideo(it: MergedVideoItem) {
  if (!await confirm(`从本地库移出「${it.title}」？\n云端字幕不受影响。`, "移出")) return;
  await window.desktop.removeVideo(it.localId!, { cache: true, thumb: true, entry: true });
  setLib(await window.desktop.getLibrary());
  await refreshCached();
  toast("已移出本地库");
}

export async function deleteCloudSubtitle(it: MergedVideoItem) {
  if (!await confirm(`删除云端字幕「${it.title}」？\n记录和字幕将被永久删除，无法恢复。`, "删除")) return;
  try {
    await apiPost("/edit/video/delete", { video_id: it.id });
    toast("已删除云端字幕");
    await refresh();
  } catch (e) { toast(`删除失败：${errText(e)}`, true); }
}

/** 窗口状态监听 */
export function installHomeWatchers() {
  const onVisibility = () => {
    if (document.hidden) stopPolling();
    else if (isReady()) refresh({ passive: true });
  };
  const onFocus = () => { if (isReady()) refresh({ passive: true }); };
  document.addEventListener("visibilitychange", onVisibility);
  window.addEventListener("focus", onFocus);

  // 编辑器窗口关掉时重新刷一下库，云端状态会走 onFocus刷
  const offHomeRefresh = window.desktop.onHomeRefresh(async notice => {
    // 编辑器撞上 401/403 不一定等于 key 失效：先退回 boot 态收起主界面，
    // 等这一轮刷新验完再决定进主页还是登录页
    if (notice?.unauthorized) setAppPhase("boot");
    if (notice?.error) toast(notice.error, true);
    setLib(await window.desktop.getLibrary());
  });

  return () => {
    document.removeEventListener("visibilitychange", onVisibility);
    window.removeEventListener("focus", onFocus);
    offHomeRefresh?.();
  };
}
