import { ASS_EVENTS_HEAD, ASS_SCRIPT_INFO } from '../constants';
import { getPlayRes } from '../ass';
import { playStore } from '../store/playStore';
import { buildAss } from './assBuild';
import { subCanvas, video } from './media';

// 字幕预览：交给 libass 本体（JASSUB/WASM）渲染。以前那套 DOM 叠层怎么调都对不齐。
// JASSUB 打包成 IIFE 挂在全局（file:// 下 <script type="module"> 加载不了）
declare const JASSUBmod: any;

let jassub: any = null;         // 就绪前所有绘制请求直接丢掉
let jassubLoad: Promise<any> | null = null;
let lastAss = "";               // 上次喂给 libass 的 ASS：内容没变就只重画，不重建 track

/** 加载遮罩期间先取资源并启动 worker/WASM，字幕数据回来后只需换入正式 track */
export function preloadSubtitles(): Promise<any> {
  if (!jassubLoad) jassubLoad = (async () => {
    const rd = window.desktop.readRendererAsset;
    const [worker, wasm, fzy, jnb] = await Promise.all([
      rd("vendor/jassub/jassub-worker.js"),
      rd("vendor/jassub/jassub-worker-modern.wasm"),
      rd("fonts/FZZhunYuan.woff2"),
      rd("fonts/JingNanBoBoHei.woff2"),
    ]);
    const blobUrl = (data: BlobPart, type: string) => URL.createObjectURL(new Blob([data], { type }));
    const wasmUrl = blobUrl(wasm, "application/wasm");
    lastAss = ASS_SCRIPT_INFO + ASS_EVENTS_HEAD;
    const inst = new JASSUBmod.default({
      canvas: subCanvas(),
      subContent: lastAss,
      workerUrl: blobUrl(worker, "text/javascript"),
      wasmUrl, modernWasmUrl: wasmUrl,
      fonts: [fzy, jnb],
      defaultFont: "方正准圆_gbk",
      queryFonts: false,
    });
    await inst.ready;
    jassub = inst;
    return inst;
  })();
  return jassubLoad;
}

/** 字幕数据到手后换入正式 track。busy 提示由调用方按返回/抛出来控制 */
export async function initSubtitles(setBusy: (text: string | null) => void) {
  const showBusy = setTimeout(() => setBusy("字幕预览加载中…"), 300);
  try {
    const inst = await preloadSubtitles();
    const ass = buildAss();
    if (ass !== lastAss) {
      lastAss = ass;
      await inst.renderer.setTrack(ass);
    }
    drawSubs();
    setBusy(null);
  } catch (e) {
    // 失败就把提示留在画面上，具体原因交给调用处 toast
    setBusy("字幕预览加载失败");
    throw e;
  } finally {
    clearTimeout(showBusy);
  }
}

/** 字幕内容或模板变了：重建 track（libass 要重新解析整份 ASS） */
export function syncSubs() {
  if (!jassub) return;
  const ass = buildAss();
  if (ass !== lastAss) {
    lastAss = ass;
    jassub.renderer.setTrack(ass).catch(() => { });
  }
  drawSubs();
}

// 文本框逐字输入也会改字幕内容，但每敲一个字符就 setTrack 等于让 libass 把整份 ASS
// 重新解析一遍（IME 组字期间 input 事件更密）：合并成一次，末次输入后 80ms 再同步。
let subsSyncTimer: ReturnType<typeof setTimeout> | undefined;
export function syncSubsSoon() {
  clearTimeout(subsSyncTimer);
  subsSyncTimer = setTimeout(syncSubs, 80);
}

/** 只是时间变了：重画当前帧。JASSUB 内部有 busy 去重，拖播放头时多余的调用会自己丢掉 */
export function drawSubs() {
  if (!jassub) return;
  const v = video();
  const pr = getPlayRes();
  jassub.manualRender({
    mediaTime: playStore.get().t,
    expectedDisplayTime: performance.now(),
    // 视频还没加载时按模板 PlayRes 当画面尺寸，字幕照样能预览
    width: v?.videoWidth || pr.x,
    height: v?.videoHeight || pr.y,
  }, true).catch(() => { });
}
