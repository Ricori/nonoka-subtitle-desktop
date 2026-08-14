import { ROW_H0, ROW_MAX, ROW_MIN } from '../constants';
import { parseAssTemplate } from '../ass';
import { apiUrl, authHeaders, backHome, getVid } from '../session';
import { bumpDoc, docStore } from '../store/docStore';
import { LAYOUT } from '../store/layoutStore';
import { setLoadedState } from '../store/saveStore';
import { resumeKnowledgeLearning } from '../store/knowledgeLearningStore';
import { select } from '../store/selectionStore';
import { modalStore, toast } from '../store/uiStore';
import { videoStore } from '../store/videoStore';
import { ensureBlkWin, relayout, setDuration, syncZoomRange, viewStore } from '../store/viewStore';
import { clampN, errText } from '../utils';
import { initSubtitles, preloadSubtitles } from './subtitles';
import { setupVideo, showVideoFallback } from './videoSource';
import { resetAutoGain } from './wave';
import type { Clip, Peaks, Seg, Track } from '../types';

/**
 * 清洗 ASR 数据自带的毫秒级重叠（编辑器约束同轨不可重叠；重叠还会让 ASS 渲染器把
 * 后一句整句顶离贴边位）：前句出点钳到后句入点
 */
function mapSegs(arr: any[]): Seg[] {
  const out: Seg[] = (arr || []).map((s: any) => {
    const o: Seg = { t0: +s.t0 || 0, t1: +s.t1 || 0, ja: s.ja || "", zh: s.zh || "" };
    if (Array.isArray(s.words) && s.words.length) o.words = s.words;
    if (s.low_conf) o.low_conf = true;
    return o;
  }).sort((a, b) => a.t0 - b.t0);
  for (let i = 0; i < out.length - 1; i++)
    if (out[i].t1 > out[i + 1].t0) out[i].t1 = out[i + 1].t0;
  return out;
}

/** 启动：字幕、视频缓冲、JASSUB 三路并行 */
export async function runBootSequence() {
  try {
    void window.desktop.setOpenInEditor(getVid());
    setupVideo().catch(e => showVideoFallback(false, "视频预加载失败：" + errText(e)));
    preloadSubtitles().catch(() => { });
    // 必须带超时：加载遮罩盖着整个窗口且不再有「返回」退路，后端冷启动挂住的话
    // 只剩一个转不完的圈。超时会走下面的 catch，自动关窗回主页并说明原因。
    const [er, pr] = await Promise.all([
      fetch(apiUrl(`/edit/${getVid()}`), { headers: authHeaders(), signal: AbortSignal.timeout(30_000) }),
      fetch(apiUrl(`/edit/${getVid()}/peaks`), { headers: authHeaders(), signal: AbortSignal.timeout(30_000) }).catch(() => null),
    ]);
    if (er.status === 401 || er.status === 403) { backHome({ unauthorized: true }); return; }
    if (!er.ok) {
      const d = await er.json().catch(() => ({} as any));
      throw new Error(d.detail || "加载失败（HTTP " + er.status + "）");
    }
    const data = await er.json();

    const segs = mapSegs(data.subtitles);
    // 兼容早期扁平结构（hidden/style_ja/style_zh/h）→ 新的 ja/zh 分 lane 结构
    const tracks: Track[] = (data.tracks || []).map((tr: any, i: number) => {
      const ja = tr.ja || { hidden: !!tr.hidden, style: tr.style_ja || null };
      const zh = tr.zh || { hidden: !!tr.hidden, style: tr.style_zh || null };
      return {
        id: tr.id || ("t" + Date.now().toString(36) + i),
        name: tr.name || ("轨道 " + (i + 1)),
        ja: { hidden: !!ja.hidden, style: ja.style || null },
        zh: { hidden: !!zh.hidden, style: zh.style || null },
        hja: clampN(+(tr.h_ja != null ? tr.h_ja : tr.h), ROW_MIN, ROW_MAX, ROW_H0),
        hzh: clampN(+(tr.h_zh != null ? tr.h_zh : tr.h), ROW_MIN, ROW_MAX, ROW_H0),
        segs: mapSegs(tr.segs),
      };
    });
    // 默认轨元数据：老数据没有时初始化（隐藏状态迁移自旧版 localStorage 的 hideJa）
    const tm = data.track_meta || {};
    const trackMeta = {
      name: tm.name || "默认轨",
      ja: { hidden: tm.ja ? !!tm.ja.hidden : !!LAYOUT.hideJa, style: (tm.ja && tm.ja.style) || "JP" },
      zh: { hidden: tm.zh ? !!tm.zh.hidden : false, style: (tm.zh && tm.zh.style) || "CN" },
    };

    let peaks: Peaks | null = null;
    if (pr && pr.ok) { try { peaks = await pr.json(); } catch { /* 波形可有可无 */ } }

    docStore.set({
      rev: data.rev || 0,
      title: data.title || getVid(),
      videoFp: data.fp || null,
      segs, tracks, trackMeta,
      assTemplate: data.ass_template || "",
      isAdmin: !!data.is_admin,
      knowledgeBase: data.knowledge_base || "",
      canLearnKnowledge: !!data.can_learn_knowledge,
      knowledgeLearning: data.knowledge_learning || { status: "idle" },
      peaks,
    });
    parseAssTemplate(docStore.get().assTemplate);
    resetAutoGain();   // 自动档的实际倍率要等 peaks 到手才算得出

    setDuration((peaks?.duration) || (segs.length ? segs[segs.length - 1].t1 + 2 : 60));
    document.title = "NONOKA字幕 · " + docStore.get().title;

    // 切片存本地库，拿不到（比如这台机器没导入过这个视频）就当没有切片
    const clips: Clip[] = ((await window.desktop.getClips(getVid()).catch(() => [])) || [])
      .sort((a: Clip, b: Clip) => a.t0 - b.t0);
    viewStore.set({ clips });

    modalStore.set({ bootDone: true });
    bumpDoc();
    // 预览渲染器（WASM）异步起来，起不来只是没预览，不挡编辑和导出
    initSubtitles(text => videoStore.set({ subBusy: text }))
      .catch(e => toast("字幕预览渲染器加载失败：" + errText(e)));
    relayout();
    syncZoomRange();
    ensureBlkWin(true);
    if (segs.length) select(0);
    setLoadedState();
    resumeKnowledgeLearning();
  } catch (e: any) {
    // 加载失败没有可用的编辑器，留在这儿只能看着遮罩——直接回主页，
    // 原因交给主页弹提示（它有 toast，这边遮罩底下什么都没有）
    const why = e?.name === "TimeoutError"
      ? "连接后端超时（30 秒），请检查网络后重试。"
      : (e?.message || "加载失败");
    backHome({ error: "打开字幕失败：" + why });
  }
}
