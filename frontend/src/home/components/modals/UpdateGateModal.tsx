import type { ReactNode } from 'react';
import { bootStore, retryFfmpeg } from '../../store/bootStore';

const MB = (n: number) => (n / 1024 ** 2).toFixed(1);

const FFMPEG_NOTES: Record<string, string> = {
  missing: "正在准备下载…",
  checking: "正在检查 FFmpeg…",
  verifying: "正在校验 FFmpeg…",
  // downloading 需要 total/done，见下方特判
};

const FORCE_UPDATE_NOTES: Record<string, string> = {
  verify: "正在校验安装包…",
  unpack: "正在解压…",
  ready: "即将重启完成更新…",
  installing: "正在安装，应用会自动重启…",
  "waiting-editor": "请先保存并关闭编辑器窗口，更新会自动继续",
  retry: "下载失败，正在重试…",
  // download 同上，需要 total/done
};

interface GateContent {
  title: string;
  lead: ReactNode;
  pct: number;
  note: string;
  showRetry: boolean;
  onRetry?: () => void;
}

// FFmpeg 安装进度 + 强制更新共用弹窗
export function UpdateGateModal() {
  const ffmpegStatus = bootStore.use(s => s.ffmpegStatus);
  const initialCheckPending = bootStore.use(s => s.initialCheckPending);
  const mandatoryActive = bootStore.use(s => s.mandatoryActive);
  const mandatoryStatus = bootStore.use(s => s.mandatoryStatus);

  let content: GateContent | null = null;
  if (mandatoryActive && mandatoryStatus) {
    const s = mandatoryStatus;
    content = {
      title: `需要更新到 v${s.version}`,
      lead: <>这个版本已停止支持，正在为你安装新版本。<br />完成后应用会自动重启，无需操作。</>,
      pct: s.total ? Math.min(100, s.done / s.total * 100) : 0,
      note: s.stage === "download"
        ? (s.total ? `正在下载… ${MB(s.done)} / ${MB(s.total)} MB` : "正在下载…")
        : FORCE_UPDATE_NOTES[s.stage] || "正在准备…",
      showRetry: false,
    };
  } else if (!initialCheckPending && ffmpegStatus && ffmpegStatus.state !== "ready") {
    const s = ffmpegStatus;
    content = {
      title: `正在安装 FFmpeg ${s.version}`,
      lead: <>首次启动需要下载视频处理组件。<br />安装完成后会自动进入应用。</>,
      pct: s.total ? Math.min(100, s.done / s.total * 100) : 0,
      note: s.error || (s.state === "downloading"
        ? (s.total ? `正在下载… ${MB(s.done)} / ${MB(s.total)} MB` : "正在下载…")
        : FFMPEG_NOTES[s.state] || "正在准备…"),
      showRetry: s.state === "error",
      onRetry: retryFfmpeg,
    };
  }

  const open = !!content;
  return (
    <div id="updmask" className={"mask gate force" + (open ? " on" : "")}>
      <div className="modal upd">
        <h2 id="upd-title">{content?.title || "需要更新"}</h2>
        <p id="upd-lead" className="lead">
          {content?.lead || <>这个版本已停止支持，正在为你安装新版本。<br />完成后应用会自动重启，无需操作。</>}
        </p>
        <div className="upd-track">
          <div id="upd-bar" className="upd-fill" style={{ width: (content?.pct ?? 0).toFixed(1) + "%" }}></div>
        </div>
        <div id="upd-note" className="upd-note">{content?.note || "正在准备…"}</div>
        <button id="upd-retry" className="btn primary" type="button" hidden={!content?.showRetry} onClick={content?.onRetry}>
          重试下载
        </button>
      </div>
    </div>
  );
}
