import { createStore } from '../lib/createStore';
import { apiPost } from '../lib/apiClient';
import { confirm, toast } from '../lib/notify';
import { blockReason, errText } from '../utils';
import { askSpeakers } from './glossaryStore';
import { libraryStore, markStarted, refresh, refreshCached } from './libraryStore';
import type { MergedVideoItem, PipeState } from '../types';

// 转写流水线：抽音频 → /edit/upload/init → 流式 PUT → /edit/video/start → 换主键 → 轮询。
// 原视频永不上传，只传音轨；同时并行把原视频复制进缓存，保证转写完打开编辑器走本地缓存直接命中。
export const pipelineStore = createStore<{ pipe: PipeState | null }>({ pipe: null });

const setPipe = (pipe: PipeState | null) => pipelineStore.set({ pipe });

function patchPipe(patch: Partial<PipeState>) {
  const cur = pipelineStore.get().pipe;
  if (cur) setPipe({ ...cur, ...patch });
}

/** 撤销未完成的上传记录：不留 uploading 幽灵占位，顺带清 R2 残留。尽力而为，不阻断 */
function abortUpload(vid?: string | null) {
  if (!vid) return;
  apiPost("/edit/upload/abort", { video_id: vid }, {
    keepalive: true, loading: false, handleUnauthorized: false,
  }).catch(() => { });
}

export async function startTranscribe(it: MergedVideoItem) {
  const lib = libraryStore.get();
  const why = blockReason(it, { pipe: pipelineStore.get().pipe, busyOther: lib.busyOther, cloudBusy: lib.cloudBusy });
  if (why) { toast(why, true); return; }

  // error 重试：服务端记录还在，直接重起即可，不用再抽音频上传一遍
  if (it.status === "error") {
    try {
      await apiPost("/edit/video/start", { video_id: it.id });
      toast("已重新开始任务");
      await refresh();
    } catch (e) { toast(`重试失败：${errText(e)}`, true); }
    return;
  }

  const entry = lib.lib.find(e => e.id === it.localId);
  if (!entry) { toast("找不到本地条目", true); return; }

  // 先问说话人 + 术语表设置再开工
  const opts = await askSpeakers();
  if (opts === null) return;   // 用户取消
  const { speakers, glossary } = opts;

  // 换主键后 cachedSet 会按新 id 重建，这个判断得用换之前的答案
  const wasCached = libraryStore.get().cachedSet.has(entry.id);

  setPipe({ cardId: it.id, localId: entry.id, vid: null, stage: "audio", done: 0, total: 0, msg: "准备中…", tmp: null });

  try {
    // 1) 抽音轨。2 小时约 60–180MB，远低于 2GB 上限，所以桌面端不限视频体积
    const audio = await window.desktop.extractAudio(entry.id);
    patchPipe({ tmp: audio.path });

    // 2) 要 presigned PUT。fp 仍是对原视频算的那份，服务端据此对同 key 去重
    patchPipe({ stage: "upload", done: 0, total: audio.size, msg: "初始化上传…" });
    const init = await apiPost("/edit/upload/init", { filename: entry.title, fp: entry.fp });
    patchPipe({ vid: init.video_id });

    // 3) 流式 PUT。两个头都签进了 presigned URL，必须与网页版逐字一致，否则 403
    patchPipe({ msg: "" });
    await window.desktop.uploadFile(entry.id, audio.path, init.put_url, {
      "Content-Type": "video/mp4",
      "Cache-Control": "public, max-age=31536000, immutable",
    });

    // 3.5) 封面上传，失败静默
    if (init.thumb_put_url) {
      await window.desktop.uploadThumb(entry.id, init.thumb_put_url).catch(() => { });
    }

    // 4) 起转写。media:"audio" 告诉服务端 R2 上那个 .mp4 里只有音轨。桌面端一律不传 keep_r2
    patchPipe({ stage: "start", msg: "启动任务…" });
    await apiPost("/edit/video/start", {
      video_id: init.video_id, filename: entry.title, fp: entry.fp, media: "audio",
      speakers, glossary,
    });

    // 5) 本地条目换主键，此后一切按服务端 video_id 对齐（缩略图/缓存一并改名）
    await window.desktop.renameLibraryId(entry.id, init.video_id);
    markStarted(await window.desktop.getLibrary(), {
      video_id: init.video_id, title: entry.title, fp: entry.fp,
      created_at: Math.floor(Date.now() / 1000), media: "audio", status: "queued",
    });
    // 缓存/源文件集合还是按旧 id 建的，不趁早重算，角标会闪一下「视频缺失」——
    // 全是本地 IPC，不用等下面那次慢刷新
    await refreshCached();

    window.desktop.deleteTemp(audio.path);   // 上传成功才删：失败时留着可省一次重抽
    setPipe(null);

    // 6) 原视频复制进缓存（后台跑）
    if (entry.srcPath && !wasCached) {
      window.desktop.copyIntoCache(init.video_id, entry.srcPath)
        .then(() => refreshCached())
        .catch(() => { });
    }

    toast(`「${entry.title}」已提交，开始任务`);
    await refresh();
  } catch (e) {
    const { vid, canceling, tmp } = pipelineStore.get().pipe || {} as Partial<PipeState>;
    abortUpload(vid);            // 没走到 start 就断了，撤销占位记录
    if (tmp && canceling) window.desktop.deleteTemp(tmp);
    setPipe(null);
    toast(canceling ? "已取消任务" : `任务失败：${errText(e)}`, !canceling);
  }
}

export async function cancelPipe() {
  const cur = pipelineStore.get().pipe;
  if (!cur) return;
  patchPipe({ canceling: true, msg: "取消中…" });
  // 杀 ffmpeg / 断开 PUT，正在 await 的那一步会抛错，由 startTranscribe 的 catch 收尾
  await window.desktop.cancelPipeline(cur.localId);
}

export async function stopTask(it: MergedVideoItem) {
  if (!await confirm("停止后本次进度将全部清除且不退还余量，确定停止？", "停止")) return;
  try {
    await apiPost("/edit/video/stop", { video_id: it.id });
    toast("已停止");
    await refresh();
  } catch (e) { toast(`停止失败：${errText(e)}`, true); }
}

export function installPipelineWatchers() {
  // 抽音频/上传的进度都由主进程推上来
  const offProgress = window.desktop.onProgress(p => {
    const cur = pipelineStore.get().pipe;
    if (!cur || p.id !== cur.localId) return;
    if (p.stage !== "audio" && p.stage !== "upload") return;
    patchPipe({ stage: p.stage as PipeState["stage"], done: p.done, total: p.total, msg: cur.canceling ? cur.msg : "" });
  });

  // 流水线跑到一半关窗：把没起成任务的占位记录撤掉，别在服务端留 uploading 幽灵
  const onUnload = () => abortUpload(pipelineStore.get().pipe?.vid);
  window.addEventListener("beforeunload", onUnload);

  return () => {
    offProgress?.();
    window.removeEventListener("beforeunload", onUnload);
  };
}
