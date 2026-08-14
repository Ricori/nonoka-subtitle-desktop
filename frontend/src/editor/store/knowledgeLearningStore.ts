import { createStore } from '../../home/lib/createStore';
import { apiUrl, authHeaders, backHome, getVid } from '../session';
import { docStore, type KnowledgeLearningState } from './docStore';
import { flushSave, saveStore } from './saveStore';
import { toast } from './uiStore';

export const knowledgeLearningStore = createStore<{ busy: boolean }>({ busy: false });

async function post<T>(path: string, body: Record<string, unknown>): Promise<T> {
  const response = await fetch(apiUrl(path), {
    method: "POST",
    headers: { ...authHeaders(), "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(30_000),
  });
  if (response.status === 401) { backHome({ unauthorized: true }); throw new Error("key 无效"); }
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.detail || `HTTP ${response.status}`);
  return data as T;
}

const delay = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));

async function waitForResult() {
  for (let i = 0; i < 240; i++) {
    await delay(2500);
    const state = await post<KnowledgeLearningState>("/edit/knowledge/learn/status", {
      video_id: getVid(),
    });
    docStore.set({ knowledgeLearning: state });
    if (state.status === "error") throw new Error(state.error || "知识学习失败");
    if (state.status !== "done") continue;
    const r = state.report || {};
    const changed = (r.added || 0) + (r.updated || 0);
    toast(changed
      ? `知识学习完成：新增 ${r.added || 0}，更新 ${r.updated || 0}，生效 ${r.activated || 0}`
      : "知识学习完成：没有发现新的可复用知识", false, null, 5000, true);
    return;
  }
  toast("知识学习仍在后台进行，稍后重新打开编辑器可查看状态", false, null, 5000);
}

async function runWait() {
  if (knowledgeLearningStore.get().busy) return;
  knowledgeLearningStore.set({ busy: true });
  try { await waitForResult(); }
  catch (e) { toast(e instanceof Error ? e.message : "知识学习失败", true); }
  finally { knowledgeLearningStore.set({ busy: false }); }
}

export async function learnKnowledge() {
  const doc = docStore.get();
  if (!doc.knowledgeBase) { toast("这个视频没有选择知识库"); return; }
  if (!doc.canLearnKnowledge) { toast("只有视频和知识库的所有者可以执行学习"); return; }
  if (knowledgeLearningStore.get().busy) return;
  knowledgeLearningStore.set({ busy: true });
  try {
    await flushSave();
    const save = saveStore.get();
    if (save.conflicted) throw new Error("版本冲突，刷新并确认字幕后再学习");
    if (save.dirty || save.saving) throw new Error("字幕尚未保存成功，请先重试保存");
    const state = await post<KnowledgeLearningState>("/edit/knowledge/learn", {
      video_id: getVid(), rev: docStore.get().rev,
    });
    docStore.set({ knowledgeLearning: state });
    toast(`已保存校订字幕，正在学习到「${doc.knowledgeBase}」…`);
    await waitForResult();
  } catch (e) {
    toast(e instanceof Error ? e.message : "知识学习失败", true);
  } finally {
    knowledgeLearningStore.set({ busy: false });
  }
}

export function resumeKnowledgeLearning() {
  const status = docStore.get().knowledgeLearning.status;
  if (status === "queued" || status === "running") void runWait();
}
