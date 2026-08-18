import { createStore } from '../lib/createStore';
import { reportHomeProbe } from '../hooks/homeProbe';
import { setAppPhase, setCfg, showLogin } from './sessionStore';
import { refresh, setLib } from './libraryStore';
import type { FFmpegStatus, UpdateStatus } from '../../../bindings/online.nonoka.subtitle/desktop/internal/app';

interface UpdateBanner { version: string; }
interface NewsItem { version: string; notes: string; }

// 启动序列：FFmpeg 安装闸门 + 强制更新闸门 + 登录校验，三者跑完才决定进登录页还是主界面。

interface BootState {
  bootLead: string;
  ffmpegStatus: FFmpegStatus | null;
  initialCheckPending: boolean;
  mandatoryActive: boolean;
  mandatoryStatus: UpdateStatus | null;
  updateBanner: UpdateBanner | null;   // 右上角「已下载新版本」
  news: NewsItem | null;
}

export const bootStore = createStore<BootState>({
  bootLead: "正在验证登录…", ffmpegStatus: null, initialCheckPending: true,
  mandatoryActive: false, mandatoryStatus: null, updateBanner: null, news: null,
});

// 闸门（FFmpeg 安装 / 强制更新）占据界面时启动页要让位，否则两层全屏内容叠在一起。
// 判定条件与 UpdateGateModal 的分支保持一致
export const gateOpen = (s: BootState) =>
  (s.mandatoryActive && !!s.mandatoryStatus) ||
  (!s.initialCheckPending && !!s.ffmpegStatus && s.ffmpegStatus.state !== "ready");

export const retryFfmpeg = async () => bootStore.set({ ffmpegStatus: await window.desktop.ffmpeg.retry() });
export const installUpdate = () => window.desktop.installUpdate();
export const clearNews = () => bootStore.set({ news: null });

let started = false;

/** 整个启动流程只应跑一次：effect 若被重挂（严格模式/HMR），第二次直接返回空清理函数 */
export function runBootSequence() {
  if (started) return () => { };
  started = true;

  let ffmpegReady = false;
  let resolveFfmpegGate: () => void;
  const ffmpegGate = new Promise<void>(res => { resolveFfmpegGate = res; });

  const markFfmpeg = (s: FFmpegStatus) => {
    bootStore.set({ ffmpegStatus: s });
    if (s.state === "ready" && !ffmpegReady) { ffmpegReady = true; resolveFfmpegGate(); }
  };

  const offFfmpeg = window.desktop.ffmpeg.onStatus(s => { if (s) markFfmpeg(s); });
  const offUpdateStatus = window.desktop.onUpdateStatus?.(s => {
    if (s?.mandatory) bootStore.set({ mandatoryActive: true, mandatoryStatus: s });
  });
  const offUpdateReady = window.desktop.onUpdateReady?.(({ version }) => bootStore.set({ updateBanner: { version } }));

  async function waitForFFmpeg() {
    let current = await window.desktop.ffmpeg.status();
    markFfmpeg(current);
    if (current.state === "missing") markFfmpeg(await window.desktop.ffmpeg.retry());
    await ffmpegGate;
  }

  async function waitForInitialUpdateCheck() {
    let current: UpdateStatus | null | undefined = await window.desktop.getUpdateStatus?.();
    if (current?.stage === "checking") {
      current = await new Promise<UpdateStatus | null>(resolve => {
        let off: (() => void) | null = null;
        const finish = (status?: UpdateStatus | null) => {
          if (!status || status.stage === "checking") return;
          off?.();
          resolve(status);
        };
        off = window.desktop.onUpdateStatus?.(finish) ?? null;
        window.desktop.getUpdateStatus?.().then(finish).catch(() => resolve(null));
      });
    }
    if (current?.mandatory) bootStore.set({ mandatoryActive: true, mandatoryStatus: current });
    return !!current?.mandatory;
  }

  (async () => {
    // 登录数据和 key 校验先在 boot 背后并行跑，结果等更新与 FFmpeg 闸门结束后再展示
    const loginCheck = (async () => {
      const cfg = await window.desktop.getConfig();
      setCfg(cfg);
      setLib(await window.desktop.getLibrary());
      await window.desktop.setOpenInEditor("");   // 清空「正在编辑器中打开」标记
      if (cfg.taskKey) return await refresh({ deferGate: true });
      return { ready: false, loginMessage: null as string | null };
    })().catch((e: any) => ({ ready: false, loginMessage: "启动失败：" + (e.message || "未知错误") }));

    // 首次更新检查留在 boot 闸门内；强制更新直接接管界面，不先露出登录页
    if (await waitForInitialUpdateCheck()) return;
    bootStore.set({ initialCheckPending: false, bootLead: "正在检查视频组件…" });
    await waitForFFmpeg();
    bootStore.set({ bootLead: "正在验证登录…" });
    const login = await loginCheck;
    if (login.ready) setAppPhase("ready");
    else showLogin(login.loginMessage);

    // 更新说明：装完更新后的新版本首次启动弹一次（主进程取走即删）。失败静默
    try {
      const n = await window.desktop.consumeReleaseNotes?.();
      if (n?.notes) bootStore.set({ news: { version: n.version, notes: n.notes } });
    } catch { }
    reportHomeProbe();
  })();

  return () => {
    offFfmpeg?.();
    offUpdateStatus?.();
    offUpdateReady?.();
  };
}
