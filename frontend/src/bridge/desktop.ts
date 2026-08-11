import { Events, System, Window } from "@wailsio/runtime";
import { DesktopService, PrototypeService } from "../../bindings/online.nonoka.subtitle/desktop-wails/internal/app";
import type {
  AppConfig,
  ExportOptions,
  FFmpegStatus,
  HomeNotice,
  HomeProbe,
  ImportFailure,
  ImportResult,
  LibraryEntry,
  CloudLibraryEntry,
  MediaProgress,
  ThumbReady,
  VideoFailed,
  VideoReady,
} from "../../bindings/online.nonoka.subtitle/desktop-wails/internal/app";

const listen = <T>(name: string, callback: (value: T) => void) =>
  Events.On(name, (event) => callback(event.data as T));

/** 配置是整体读写的，这里补齐未传的字段再写回，调用方只需给要改的那几项 */
const setConfig = async (patch: Partial<AppConfig>) => {
  const current = await DesktopService.GetConfig();
  return DesktopService.SetConfig({ ...current, ...patch });
};

// Go 端的切片返回可能是 null，统一补成空数组，调用方不用到处判空
const getLibrary = async () => (await DesktopService.GetLibrary()) ?? [];
const syncCloudLibrary = async (entries: CloudLibraryEntry[]) =>
  (await DesktopService.SyncCloudLibrary(entries)) ?? [];

const importPaths = async (paths: string[]): Promise<
  Omit<ImportResult, "added" | "failed"> & { added: LibraryEntry[]; failed: ImportFailure[] }
> => {
  const result = await DesktopService.ImportVideos(paths);
  return { ...result, added: result.added ?? [], failed: result.failed ?? [] };
};

const readRendererAsset = async (name: string) => {
  if (!/^(vendor\/jassub|fonts)\/[\w.-]+$/.test(name)) throw new Error(`asset not allowed: ${name}`);
  const response = await fetch(`/${name}`);
  if (!response.ok) throw new Error(`读取渲染资源失败（HTTP ${response.status}）`);
  return new Uint8Array(await response.arrayBuffer());
};

const setTitleBarOverlay = async ({ color }: { color: string; symbolColor?: string }) => {
  const match = /^#([\da-f]{2})([\da-f]{2})([\da-f]{2})$/i.exec(color);
  if (!match) return false;
  await Window.SetBackgroundColour(
    Number.parseInt(match[1], 16),
    Number.parseInt(match[2], 16),
    Number.parseInt(match[3], 16),
    255,
  );
  return true;
};

const installWindowsTitlebar = async () => {
  let isWindows = System.IsWindows();
  if (!isWindows) {
    try {
      isWindows = (await System.Environment()).OS === "windows";
    } catch {
      return;
    }
  }
  if (!isWindows || document.getElementById("wails-window-controls")) return;

  const style = document.createElement("style");
  style.textContent = `
    html.wails-frameless .topbar { padding-right: 148px; }
    #wails-window-controls {
      position: fixed; top: 0; right: 0; z-index: 10000;
      display: flex; height: 48px; color: var(--text, #e8edf7);
      -webkit-app-region: no-drag; user-select: none;
    }
    #wails-window-controls button {
      width: 46px; height: 48px; padding: 0; border: 0; border-radius: 0;
      display: grid; place-items: center; background: transparent; color: inherit;
      cursor: default; -webkit-app-region: no-drag;
    }
    #wails-window-controls button:hover { background: rgba(255,255,255,.09); }
    #wails-window-controls button:active { background: rgba(255,255,255,.14); }
    #wails-window-controls .wails-close:hover { background: #c42b1c; color: #fff; }
    #wails-window-controls .wails-close:active { background: #a62014; }
    #wails-window-controls svg { width: 11px; height: 11px; fill: none; stroke: currentColor; stroke-width: 1.15; }
    #wails-window-controls .wails-min { --wails-non-client-region: minimize; }
    #wails-window-controls .wails-max { --wails-non-client-region: maximize; }
    #wails-window-controls .wails-close { --wails-non-client-region: close; }
    html.wails-fullscreen #wails-window-controls { display: none; }
    html.wails-fullscreen .topbar { padding-right: 10px; }
  `;
  document.head.appendChild(style);
  document.documentElement.classList.add("wails-frameless");

  const controls = document.createElement("div");
  controls.id = "wails-window-controls";
  controls.setAttribute("aria-label", "窗口控制");
  controls.innerHTML = `
    <button class="wails-min" type="button" title="最小化" aria-label="最小化">
      <svg viewBox="0 0 11 11"><path d="M1 5.5h9"/></svg>
    </button>
    <button class="wails-max" type="button" title="最大化" aria-label="最大化"></button>
    <button class="wails-close" type="button" title="关闭" aria-label="关闭">
      <svg viewBox="0 0 11 11"><path d="M1 1l9 9M10 1L1 10"/></svg>
    </button>`;
  document.body.appendChild(controls);

  const maximise = controls.querySelector<HTMLButtonElement>(".wails-max")!;
  const renderMaximise = async () => {
    const maximised = await Window.IsMaximised();
    maximise.title = maximised ? "还原" : "最大化";
    maximise.setAttribute("aria-label", maximise.title);
    maximise.innerHTML = maximised
      ? '<svg viewBox="0 0 11 11"><path d="M3 1.5h6.5V8H8M1.5 3H8v6.5H1.5z"/></svg>'
      : '<svg viewBox="0 0 11 11"><rect x="1.5" y="1.5" width="8" height="8"/></svg>';
  };
  controls.querySelector<HTMLButtonElement>(".wails-min")!.onclick = () => void Window.Minimise();
  maximise.onclick = () => void Window.ToggleMaximise();
  controls.querySelector<HTMLButtonElement>(".wails-close")!.onclick = () => void Window.Close();

  Events.On("common:WindowMaximise", () => void renderMaximise());
  Events.On("common:WindowUnMaximise", () => void renderMaximise());
  Events.On("common:WindowRestore", () => void renderMaximise());
  Events.On("common:WindowFullscreen", () => document.documentElement.classList.add("wails-fullscreen"));
  Events.On("common:WindowUnFullscreen", () => document.documentElement.classList.remove("wails-fullscreen"));
  void renderMaximise();
};

void installWindowsTitlebar();

// 渲染层唯一的主进程入口。方法一律扁平摆放，名字对应 Go 端的 Service 方法。
export const desktopBridge = {
  // FFmpeg 有三个成员且总是一起用，唯一保留分组的一处
  ffmpeg: {
    status: DesktopService.FFmpegStatus,
    retry: DesktopService.RetryFFmpeg,
    onStatus: (callback: (status: FFmpegStatus) => void) => listen("ffmpeg:status", callback),
  },

  getConfig: DesktopService.GetConfig,
  setTitleBarOverlay,
  setConfig,
  getLibrary,
  syncCloudLibrary,
  pickVideos: async () => (await DesktopService.PickVideos()) ?? [],
  importVideos: importPaths,
  renameLibraryId: DesktopService.RenameLibraryID,
  renameTitle: DesktopService.RenameLibraryTitle,
  removeVideo: (id: string, options: { cache?: boolean; thumb?: boolean; entry?: boolean } = {}) =>
    DesktopService.RemoveLibraryData(id, {
      cache: options.cache ?? true,
      thumb: options.thumb ?? true,
      entry: options.entry ?? true,
    }),
  copyIntoCache: DesktopService.CopyIntoCache,
  hasCached: DesktopService.HasCache,
  cacheStats: DesktopService.GetCacheStats,
  clearCache: (id = "") => DesktopService.ClearCache(id),
  migrateCacheDir: DesktopService.MigrateCacheDirectory,
  extractAudio: DesktopService.ExtractAudio,
  uploadFile: DesktopService.UploadFile,
  uploadThumb: DesktopService.UploadThumb,
  cacheThumb: DesktopService.CacheThumbFromCloud,
  deleteTemp: DesktopService.DeleteTemp,
  cancelPipeline: DesktopService.CancelMediaJob,
  resolveVideo: (id: string, options: { canR2?: boolean } = {}) =>
    DesktopService.ResolveVideo(id, { canR2: options.canR2 ?? false }),
  pickAndValidateVideo: (id: string, expect: { fp?: string; duration?: number } = {}) =>
    DesktopService.PickAndValidateVideo(id, { fp: expect.fp ?? "", duration: expect.duration ?? 0 }),
  attachLocalVideo: DesktopService.AttachLocalVideo,
  getClips: async (id: string) => (await DesktopService.GetClips(id)) ?? [],
  setClips: DesktopService.SetClips,
  fileExists: DesktopService.PathExists,
  thumbnailURL: DesktopService.ThumbnailURL,
  pickDirectory: DesktopService.PickCacheDirectory,
  revealInFolder: DesktopService.RevealInFolder,
  openEditor: DesktopService.OpenLibraryVideo,
  closeEditor: async (notice: Partial<HomeNotice> = {}) => {
    await DesktopService.SetOpenInEditor("");
    return PrototypeService.CloseEditor({
      error: notice.error ?? "",
      unauthorized: notice.unauthorized ?? false,
    });
  },
  onHomeRefresh: (callback: (notice: HomeNotice) => void) => listen("home:refresh", (notice: HomeNotice) => {
    void DesktopService.SetOpenInEditor("");
    callback(notice);
  }),
  onFilesDropped: (callback: (paths: string[]) => void) => listen("files:dropped", callback),
  onRequestClose: (callback: () => void) => listen("prototype:request-close", callback),
  onUpdateReady: (callback: (status: { version: string }) => void) => listen("update:ready", callback),
  installUpdate: DesktopService.InstallUpdate,
  getUpdateStatus: DesktopService.GetUpdateStatus,
  onUpdateStatus: (callback: (status: Awaited<ReturnType<typeof DesktopService.GetUpdateStatus>>) => void) =>
    listen("update:status", callback),
  consumeReleaseNotes: DesktopService.ConsumeReleaseNotes,
  readRendererAsset,
  pickExportOutput: DesktopService.PickExportOutput,
  saveSubtitle: DesktopService.SaveSubtitle,
  renderExport: (options: ExportOptions) => DesktopService.RenderExport(options),
  setOpenInEditor: DesktopService.SetOpenInEditor,
  runtimeInfo: PrototypeService.RuntimeInfo,
  reportHomeProbe: (probe: HomeProbe) => PrototypeService.ReportHomeProbe(probe),
  reportPlaybackProbe: PrototypeService.ReportPlaybackProbe,
  onProgress: (callback: (progress: MediaProgress) => void) => listen("media:progress", callback),
  onThumbReady: (callback: (id: string) => void) => listen<ThumbReady>("thumb:ready", (payload) => callback(payload.id)),
  onVideoReady: (callback: (payload: VideoReady) => void) => listen("video:ready", callback),
  onVideoFailed: (callback: (payload: VideoFailed) => void) => listen("video:failed", callback),
};

declare global {
  interface Window {
    desktop: typeof desktopBridge;
  }
}

window.desktop = desktopBridge;
