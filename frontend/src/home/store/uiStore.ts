import { createStore } from '../lib/createStore';
import { closeAsk } from '../lib/notify';
import { AVAIL_ORDER, type AvailKind } from '../utils';

export type SortMode = "new" | "name" | "dur";
export type ViewMode = "grid" | "list";

interface UiState {
  // 搜索/排序/视图偏好。filter 存搜索框里的原始文本（受控输入要求原样回显，不能存
  // 修剪/小写后的版本，否则用户输入中间就会被强改），匹配时用 normalizedFilter
  filter: string;
  normalizedFilter: string;
  /** 视频状态多选筛选，空数组=不筛。顺序恒等于 AVAIL_ORDER，按钮上的摘要才不会跳来跳去 */
  avail: AvailKind[];
  sortMode: SortMode;
  view: ViewMode;
  // 弹窗开合。集中在一处，登录闸门离开 ready 时才好一次性收干净
  settingsOpen: boolean;
  speakerOpen: boolean;
  gmOpen: boolean;
  gmPreselect: string | null;
  // 顶栏⋮菜单、卡片「⋯」菜单、自定义下拉共用一套「同时只开一个」的状态，用字符串 id 标识
  popover: string | null;
}

export const uiStore = createStore<UiState>({
  filter: "", normalizedFilter: "", avail: [], sortMode: "new", view: "grid",
  settingsOpen: false, speakerOpen: false, gmOpen: false, gmPreselect: null,
  popover: null,
});

export const setFilter = (filter: string) =>
  uiStore.set({ filter, normalizedFilter: filter.trim().toLowerCase() });

export const toggleAvail = (kind: AvailKind) => uiStore.set(s => ({
  avail: s.avail.includes(kind)
    ? s.avail.filter(k => k !== kind)
    : AVAIL_ORDER.filter(k => k === kind || s.avail.includes(k)),
}));
export const clearAvail = () => uiStore.set(s => (s.avail.length ? { avail: [] } : {}));
export const setSortMode = (sortMode: SortMode) => uiStore.set({ sortMode });
export const setView = (view: ViewMode) => uiStore.set({ view });

export const setSettingsOpen = (settingsOpen: boolean) => uiStore.set({ settingsOpen });
export const setSpeakerOpen = (speakerOpen: boolean) => uiStore.set({ speakerOpen });
export const setGlossaryManager = (gmOpen: boolean, gmPreselect: string | null = null) =>
  uiStore.set({ gmOpen, gmPreselect });

// ── popover ────────────────────────────────────────────────────────────────
// 监听只在有菜单打开时挂着，用命令式管理（不经过 React），因为触发点分布在
// 卡片/顶栏/下拉各处，没有单一的宿主组件
let popoverDisposer: (() => void) | null = null;

function attachPopoverWatchers() {
  // 必须在捕获阶段拦：等冒泡到卡片按钮时，那些按钮自己的监听器已经先把动作做完了。
  // 触发菜单的按钮自身（gear / 卡片⋯ / 下拉按钮）放行，交给它们的 onClick 切换开合。
  const onDocClick = (e: MouseEvent) => {
    const target = e.target as Element | null;
    if (target?.closest("#gear") || target?.closest('[data-act="more"]') || target?.closest(".custom-sel-btn")) return;
    if (target?.closest(".topmenu")) return;
    closePopover();
    e.stopPropagation();
  };
  const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") closePopover(); };
  document.addEventListener("click", onDocClick, true);
  document.addEventListener("keydown", onKey);
  return () => {
    document.removeEventListener("click", onDocClick, true);
    document.removeEventListener("keydown", onKey);
  };
}

function applyPopover(next: string | null) {
  if (uiStore.get().popover === next) return;
  uiStore.set({ popover: next });
  document.body.classList.toggle("menu-open", next !== null);
  if (next !== null && !popoverDisposer) popoverDisposer = attachPopoverWatchers();
  else if (next === null && popoverDisposer) { popoverDisposer(); popoverDisposer = null; }
}

export const togglePopover = (id: string) => applyPopover(uiStore.get().popover === id ? null : id);
export const closePopover = () => applyPopover(null);

/** 退出 ready 时把顶栏/卡片菜单、设置弹窗和待答的确认框一并收掉，别让它们被闸门盖住还挂着 */
export function closeAllTransient() {
  closeAsk(false);
  uiStore.set({ settingsOpen: false, gmOpen: false, gmPreselect: null });
  closePopover();
}
