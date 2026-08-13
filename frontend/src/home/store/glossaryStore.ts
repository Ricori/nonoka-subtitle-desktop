import { createStore } from '../lib/createStore';
import { apiGet, apiPost } from '../lib/apiClient';
import { parseAxisFile, type AxisKind, type AxisParse } from '../lib/assAxis';
import { setGlossaryManager, setSpeakerOpen, uiStore } from './uiStore';
import type { GlossItem, GlossSets, SpeakerResult } from '../types';

// 术语表清单缓存 + 转写选项弹窗（说话人+术语表）的 Promise 式确认。
// 清单：{名称: CSV 文本} + 每套的元信息（能不能改、多少条）。登录后首次用到时拉一次并缓存，
// 管理界面增删改后带 force 重拉。拉不到就只留「不使用」。
interface GlossaryState {
  /** null = 还没拉过；{} 是合法结果（一套都没有），不能拿它当「未加载」 */
  sets: GlossSets | null;
  items: GlossItem[];
  admin: boolean;
  /** 成功拉到过一次。失败时不置位，下次用到还会再试 */
  loaded: boolean;
  // 转写选项表单。放在这里而不是弹窗组件内部：术语表管理弹窗关闭时需要把刚编辑的
  // 那套回填进来，两边得共享同一份状态
  spkOn: boolean;
  spkNum: string;
  glossValue: string;
  // LLM 两阶段开关（⑧合并+纠错 / ⑨翻译），默认都开。关掉翻译时产物只有原文，
  // 译文留空，编辑器里照样能逐句重译
  correctOn: boolean;
  translateOn: boolean;
  translationPrompt: string;
  translationPromptHasStyle: boolean;
  // ── 第一阶段：有没有已有产物 ──────────────────────────────────────────
  // 弹窗分两步走：先问「有没有已经打好的轴」，再按答案决定第二步显示什么。
  // 三种产物走的是三条完全不同的路（见 lib/assAxis.ts 顶部），所以这一步不能省。
  /** "none" = 没有产物（默认，与旧行为一致）；"have" = 有，等着选文件 */
  axisMode: "none" | "have";
  axisFile: string;
  /** 解析结果；null = 还没选文件或解析失败 */
  axisParse: AxisParse | null;
  /** 用户可手动改判类型（判错的代价很高：日文轴被当成空轴就白跑一遍识别） */
  axisKind: AxisKind;
  axisError: string;
}

export const glossaryStore = createStore<GlossaryState>({
  sets: null, items: [], admin: false, loaded: false, spkOn: false, spkNum: "", glossValue: "",
  correctOn: true, translateOn: true, translationPrompt: "", translationPromptHasStyle: false,
  axisMode: "none", axisFile: "", axisParse: null, axisKind: "empty", axisError: "",
});

export const setSpkOn = (spkOn: boolean) => glossaryStore.set({ spkOn });
export const setSpkNum = (spkNum: string) => glossaryStore.set({ spkNum });
export const setGlossValue = (glossValue: string) => glossaryStore.set({ glossValue });
export const setCorrectOn = (correctOn: boolean) => glossaryStore.set({ correctOn });
export const setTranslateOn = (translateOn: boolean) => glossaryStore.set({ translateOn });
export const setTranslationPrompt = (translationPrompt: string) => glossaryStore.set({ translationPrompt });
export const setTranslationPromptHasStyle = (translationPromptHasStyle: boolean) =>
  glossaryStore.set({ translationPromptHasStyle });
export const setAxisKind = (axisKind: AxisKind) => glossaryStore.set({ axisKind });

/** 选「我没有产物」：清掉可能选过的文件，免得退回来再进时还挂着上一份 */
export const setAxisMode = (axisMode: "none" | "have") =>
  glossaryStore.set(axisMode === "have"
    ? { axisMode }
    : { axisMode, axisFile: "", axisParse: null, axisError: "" });

/** 选中一份 ASS/SRT：当场解析并回显，判错了用户可以在下拉里改判 */
export async function loadAxisFile(file: File) {
  glossaryStore.set({ axisFile: file.name, axisParse: null, axisError: "" });
  try {
    const parsed = parseAxisFile(await file.text(), file.name);
    if (!parsed.rows.length) {
      glossaryStore.set({ axisError: "这个文件里没有解析出任何时间轴，换一份试试" });
      return;
    }
    glossaryStore.set({ axisParse: parsed, axisKind: parsed.kind });
  } catch (e) {
    glossaryStore.set({ axisError: `读取失败：${e instanceof Error ? e.message : String(e)}` });
  }
}

/** 返回这一轮实际生效的清单：调用方（比如管理弹窗刚打开时要按最新清单定位）要的是现在这份 */
export async function loadGlossSets(force?: boolean) {
  const cur = glossaryStore.get();
  if (!force && cur.loaded) return { sets: cur.sets ?? {}, items: cur.items };

  let nextSets: GlossSets = {}, nextItems: GlossItem[] | null = null, nextAdmin = false, ok = false;
  try {
    const d = await apiGet<{ sets?: GlossSets; items?: GlossItem[]; is_admin?: boolean }>("/edit/glossary-sets", {
      timeout: 10_000, handleUnauthorized: false,
    });
    if (d.sets) { nextSets = d.sets; nextItems = d.items ?? null; nextAdmin = !!d.is_admin; ok = true; }
  } catch { /* 拉不到就只留「不使用」 */ }
  if (!ok && cur.loaded) return { sets: cur.sets ?? {}, items: cur.items };   // 留着上次的清单

  // 老后端没有 items 字段：当成一份谁都改不了的只读清单，管理界面照样能看
  const items = nextItems || Object.keys(nextSets).map(name => ({
    name, csv: nextSets[name], can_edit: false, mine: false, rows: 0,
  }));
  glossaryStore.set({
    sets: nextSets, items, admin: nextAdmin, loaded: ok,
    // 当前选中的那套被删了就回落到「不使用」
    glossValue: cur.glossValue && !nextSets[cur.glossValue] ? "" : cur.glossValue,
  });
  return { sets: nextSets, items };
}

export async function saveGlossSet(name: string, csv: string, oldName: string | null) {
  return apiPost<{ name: string; csv: string; rows: number }>("/edit/glossary/save", {
    name, csv, old_name: oldName || null,
  }, { handleUnauthorized: false });
}

export const deleteGlossSet = (name: string) =>
  apiPost("/edit/glossary/delete", { name }, { handleUnauthorized: false });

// ── 转写选项弹窗：与页内确认框同样的 Promise 模式 ────────────────────────────
let speakerResolve: ((value: SpeakerResult | null) => void) | null = null;

export function askSpeakers() {
  // 每次重新问：产物是一条一份的，上一条选过的轴绝不能带到下一条上（那会把别的视频的轴
  // 硬套过来，服务端体检虽然拦得住，但用户根本不知道自己交了什么）
  glossaryStore.set({ axisMode: "none", axisFile: "", axisParse: null, axisError: "" });
  setSpeakerOpen(true);
  loadGlossSets();
  return new Promise<SpeakerResult | null>(res => { speakerResolve = res; });
}

export function closeSpeakers(ok: boolean) {
  const { spkOn, spkNum, glossValue, correctOn, translateOn, translationPrompt,
    translationPromptHasStyle,
    axisMode, axisParse, axisKind, axisFile } = glossaryStore.get();
  // 选了「我已有产物」却还没选出可用的文件时，回车/点确定都不该放行——
  // 放行的后果是静默按「没有产物」跑一遍完整识别，用户拿到的东西和他要的完全不同。
  if (ok && axisMode === "have" && !axisParse) return;
  setSpeakerOpen(false);
  const resolve = speakerResolve;
  speakerResolve = null;
  if (!resolve) return;
  if (!ok) { resolve(null); return; }
  const axis = axisMode === "have" && axisParse
    ? { kind: axisKind, rows: axisParse.rows, filename: axisFile, speakers: axisParse.speakers }
    : null;
  const n = parseInt(spkNum, 10);
  // 轴自己标了说话人就不跑模型分离——人标的比模型准，UI 那边也已经不给选了
  const speakers = (axis && axis.speakers.length >= 2) ? 0
    : !spkOn ? 0 : (Number.isFinite(n) && n >= 2 ? Math.min(n, 10) : -1);
  resolve({
    speakers, glossary: glossValue || "",
    // 导入日文/双语轴时这两个开关不适用：日文轴恒定只跑翻译，双语轴什么都不跑。
    // 空轴仍然两阶段都可选（纠错在轴模式下只剩纠错，不再合并行）。
    correct: correctOn, translate: translateOn,
    translationPrompt: translationPrompt.trim(),
    translationPromptHasStyle: !!translationPrompt.trim() && translationPromptHasStyle,
    axis,
  });
}

/** 打开术语表管理弹窗 */
export function openGlossaryManager(preselect?: string | null) {
  setGlossaryManager(true, preselect ?? null);
  loadGlossSets(true);
}

/** 从管理弹窗自己的「关闭」按钮关：如果是从转写选项的「编辑」跳进来的，
 *  把刚才编辑的那套顺手选上——十有八九就是要用它转写这一条 */
export function closeGlossaryManager(editedName?: string) {
  setGlossaryManager(false);
  const { sets } = glossaryStore.get();
  if (uiStore.get().speakerOpen && editedName && sets?.[editedName]) setGlossValue(editedName);
}
