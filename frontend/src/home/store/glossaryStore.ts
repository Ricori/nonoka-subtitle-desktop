import { createStore } from '../lib/createStore';
import { apiGet, apiPost } from '../lib/apiClient';
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
}

export const glossaryStore = createStore<GlossaryState>({
  sets: null, items: [], admin: false, loaded: false, spkOn: false, spkNum: "", glossValue: "",
});

export const setSpkOn = (spkOn: boolean) => glossaryStore.set({ spkOn });
export const setSpkNum = (spkNum: string) => glossaryStore.set({ spkNum });
export const setGlossValue = (glossValue: string) => glossaryStore.set({ glossValue });

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
  setSpeakerOpen(true);
  loadGlossSets();
  return new Promise<SpeakerResult | null>(res => { speakerResolve = res; });
}

export function closeSpeakers(ok: boolean) {
  setSpeakerOpen(false);
  const resolve = speakerResolve;
  speakerResolve = null;
  if (!resolve) return;
  if (!ok) { resolve(null); return; }
  const { spkOn, spkNum, glossValue } = glossaryStore.get();
  const n = parseInt(spkNum, 10);
  const speakers = !spkOn ? 0 : (Number.isFinite(n) && n >= 2 ? Math.min(n, 10) : -1);
  resolve({ speakers, glossary: glossValue || "" });
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
