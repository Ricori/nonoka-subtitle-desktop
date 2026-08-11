import { createStore } from './createStore';
import type { AskDialogState, ToastItem } from '../types';

// 本文件包含轻提示 / loading遮罩 / 页内确认框，三者都是模块级单例，任何地方都能直接调

/** 轻提示 Store */
export const toastStore = createStore<{ items: ToastItem[] }>({ items: [] });
let seq = 0;
export function toast(msg: string, bad?: boolean) {
  const id = ++seq;
  toastStore.set(s => ({ items: [...s.items, { id, msg, bad: !!bad }] }));
  setTimeout(() => toastStore.set(s => ({ items: s.items.filter(t => t.id !== id) })), 3600);
}

/** Loading Store */
export const loadingStore = createStore(
  {
    count: 0, // 多处可能同时要求显示忙碌，最后一个结束才真正隐藏
    text: "处理中…"
  }
);

loadingStore.subscribe(() => {
  if (loadingStore.get().count > 0) document.body.setAttribute("aria-busy", "true");
  else document.body.removeAttribute("aria-busy");
});

/** 挂上 loading 遮罩 */
export function beginLoading(text?: string) {
  loadingStore.set(s => ({ count: s.count + 1, text: text || "处理中…" }));
  let ended = false;
  return () => {
    if (ended) return;
    ended = true;
    loadingStore.set(s => ({ count: Math.max(0, s.count - 1) }));
  };
}

// ── 页内确认框 ──────────────────────────────────────────────────────────────
// 不能用 window.confirm。原生 confirm 会开一个独立的模态窗口，弹出期间整个应用（含主进程 IPC）全冻结
type Resolve = (value: boolean | string | null) => void;

export const askStore = createStore<{ dialog: AskDialogState | null }>({ dialog: null });

let askResolve: Resolve | null = null;

/** 打开确认框 */
function openAsk(text: string, okText: string, value: string | null) {
  askStore.set({ dialog: { text, okText, isInput: value !== null, value: value ?? "" } });
  return new Promise<boolean | string | null>(res => { askResolve = res; });
}

/** 确认模式：确定 → true，取消/关闭 → false */
export const confirm = (text: string, okText = "确定") =>
  openAsk(text, okText, null) as Promise<boolean>;

/** 输入模式：确定 → 输入内容，取消/关闭 → null */
export const promptText = (text: string, okText: string, value: string) =>
  openAsk(text, okText, value) as Promise<string | null>;

/** 设置确认框内文本框的值 */
export function setAskValue(value: string) {
  askStore.set(s => ({ dialog: s.dialog ? { ...s.dialog, value } : null }));
}

/** 关闭确认框 */
export function closeAsk(ok: boolean) {
  const dialog = askStore.get().dialog;
  const resolve = askResolve;
  askResolve = null;
  askStore.set({ dialog: null });
  if (!resolve) return;
  resolve(ok ? (dialog?.isInput ? dialog.value : true) : (dialog?.isInput ? null : false));
}
