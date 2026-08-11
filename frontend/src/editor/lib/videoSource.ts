import { apiUrl, authHeaders, getVid } from '../session';
import { docStore } from '../store/docStore';
import { toast } from '../store/uiStore';
import { videoStore } from '../store/videoStore';
import { errText } from '../utils';

/**
 * 视频解析链：① 缓存副本 → ② 原文件还在原处 → ③ 从 R2 取回 → ④ 让用户重新定位。
 * 全程走主进程，渲染层不碰 fs；媒体由本地回环服务供给，自带 Range，拖进度条零网络。
 */

/**
 * R2 上还有没有原视频一律以服务端的 has_r2 为准，客户端不自己探测（presigned 有效期和
 * 对象何时被删都看不见）。media=="audio" 的对象里只有音轨，取回来没法播，不算可用。
 */
async function r2Available(): Promise<boolean> {
  try {
    const r = await fetch(apiUrl("/edit/state"), { headers: authHeaders() });
    if (!r.ok) return false;
    const d = await r.json();
    const rec = (d.videos || []).find((v: any) => v.video_id === getVid());
    if (!rec) return false;
    return !!rec.has_r2 && (rec.media || "video") !== "audio";
  } catch { return false; }
}

export const mountVideo = (url: string) =>
  videoStore.set({ src: url, fallbackOpen: false, badge: null });

export function showRetrieving(pctText = "") {
  videoStore.set({ retrieving: true, retrievePct: pctText, collapsed: false, fallbackOpen: true });
}

export function showVideoFallback(collapsed: boolean, msgText?: string) {
  videoStore.set({
    retrieving: false, collapsed, fallbackOpen: true, warn: "", usePath: null,
    ...(msgText != null ? { fbMsg: msgText } : {}),
  });
}

export async function setupVideo() {
  const r = await window.desktop.resolveVideo(getVid(), { canR2: await r2Available() });
  if (r.state === "cached" || r.state === "source") { mountVideo(r.url || ""); return; }
  if (r.state === "downloading") { showRetrieving(); return; }
  showVideoFallback(false, r.reason || "");
}

/**
 * ④ 选文件兜底：原生对话框 + 主进程校验（fp 优先，时长 ±2s 兜底）。
 * 指纹/时长的比对逻辑都在主进程，渲染层只决定校验不过时要不要「仍然使用」。
 */
export async function pickVideoFile() {
  videoStore.set({ warn: "", usePath: null });
  const { videoFp, peaks } = docStore.get();
  let r;
  try {
    r = await window.desktop.pickAndValidateVideo(getVid(), {
      fp: videoFp ?? undefined, duration: peaks?.duration,
    });
  } catch (e) { toast("读取文件失败：" + errText(e), true); return; }
  if (!r || r.canceled) return;
  if (r.ok) { void attachChosen(r.path); return; }
  videoStore.set({
    warn: r.warn + " 若确为该视频的其它版本可「仍然使用」（字幕数据不受影响）。",
    usePath: r.path,
  });
}

/**
 * 采用所选文件：主进程记进库当 srcPath（下次开走解析链②）并后台复制进缓存，
 * 播放不等复制——直接播原文件。
 */
export async function attachChosen(srcPath: string) {
  try {
    const r = await window.desktop.attachLocalVideo(getVid(), srcPath);
    mountVideo(r.url);
  } catch (e) { toast("挂载失败：" + errText(e), true); }
}
