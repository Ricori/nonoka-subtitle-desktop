import { useEffect } from 'react';
import { AUTOSAVE_MS } from '../constants';
import { promptMandatoryUpdate, requestClose } from '../lib/closeFlow';
import { mountVideo, showRetrieving, showVideoFallback } from '../lib/videoSource';
import { getVid, isLeaving, reportBootError } from '../session';
import { expJob, exportStore } from '../store/exportStore';
import { clipsDirty, saveStore, startAutosave } from '../store/saveStore';
import { videoStore } from '../store/videoStore';
import { fmt, fmtMB } from '../utils';

const pctOf = (p: { done?: number; total?: number }) =>
  p.total ? Math.round((p.done || 0) / p.total * 100) : 0;

/** 主进程事件、关闭拦截、自动保存这些一次性挂载的副作用 */
export function useDesktopEvents() {
  useEffect(() => {
    const onError = (e: ErrorEvent) => reportBootError(e.error || e.message);
    const onRejection = (e: PromiseRejectionEvent) => reportBootError(e.reason);
    addEventListener("error", onError);
    addEventListener("unhandledrejection", onRejection);
    return () => {
      removeEventListener("error", onError);
      removeEventListener("unhandledrejection", onRejection);
    };
  }, []);

  useEffect(() => {
    const offClose = window.desktop.onRequestClose(requestClose);
    // 事件可能早于编辑器脚本就绪，主动补查避免错过强制更新提示
    const offUpdate = window.desktop.onUpdateStatus?.(promptMandatoryUpdate);
    window.desktop.getUpdateStatus?.().then(promptMandatoryUpdate).catch(() => { });
    return () => { offClose?.(); offUpdate?.(); };
  }, []);

  useEffect(() => {
    const onBeforeUnload = (e: BeforeUnloadEvent) => {
      if (isLeaving()) return;
      const st = saveStore.get();
      if ((st.dirty && !st.conflicted) || clipsDirty()) { e.preventDefault(); e.returnValue = ""; }
    };
    addEventListener("beforeunload", onBeforeUnload);
    return () => removeEventListener("beforeunload", onBeforeUnload);
  }, []);

  useEffect(() => startAutosave(AUTOSAVE_MS), []);

  // 后台复制/下载完成：自动挂上缓存副本，无需用户再操作
  useEffect(() => {
    const offReady = window.desktop.onVideoReady(({ id, url }) => {
      if (id !== getVid()) return;
      videoStore.set({ retrieving: false, badge: null });
      if (!videoStore.get().src && url) mountVideo(url);
    });
    const offFailed = window.desktop.onVideoFailed(({ id, error }) => {
      if (id !== getVid()) return;
      videoStore.set({ retrieving: false, badge: null });
      if (!videoStore.get().src) showVideoFallback(false, "取回失败：" + error + " 请改为选择本地文件：");
    });
    const offProgress = window.desktop.onProgress(p => {
      // 导出用的是另一个 job id，得在按 vid 过滤之前接掉
      if (p.stage === "export" && p.id === expJob()) { exportStore.set({ pct: pctOf(p) }); return; }
      if (p.id !== getVid()) return;
      if (p.stage === "download") {
        videoStore.set({ badge: `从云端取回 ${pctOf(p)}%` });
        if (videoStore.get().retrieving) showRetrieving(`${pctOf(p)}%（${fmtMB(p.done)} / ${fmtMB(p.total)}）`);
      } else if (p.stage === "transcode") {
        // done/total 是秒数，不是字节
        videoStore.set({
          badge: `转码 ${pctOf(p)}%`,
          transcodePct: `${pctOf(p)}%（${fmt(p.done)} / ${fmt(p.total)}）`,
        });
      } else if (p.stage === "copy" && !videoStore.get().src) {
        videoStore.set({ badge: `复制到缓存 ${pctOf(p)}%` });
      }
    });
    return () => { offReady?.(); offFailed?.(); offProgress?.(); };
  }, []);
}
