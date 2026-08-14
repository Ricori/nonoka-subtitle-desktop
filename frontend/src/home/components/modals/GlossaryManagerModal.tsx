import { useLayoutEffect, useMemo, useRef, useState } from 'react';
import { errText } from '../../utils';
import { beginLoading, confirm, toast } from '../../lib/notify';
import {
  closeGlossaryManager, deleteGlossSet, deleteKnowledgeEntry, glossaryStore,
  loadGlossSets, loadKnowledgeBase, reviewKnowledgeEntries, saveKnowledgeBase,
  saveKnowledgeEntry,
} from '../../store/glossaryStore';
import { uiStore } from '../../store/uiStore';
import type { GlossItem, KnowledgeEntry } from '../../types';

type Filter = "active" | "candidate" | "rejected" | "all";

const emptyBase = (): GlossItem => ({
  name: "", can_edit: true, mine: true, entry_count: 0, status_counts: {}, schema_version: 2,
  settings: { auto_activate_confidence: 8 },
});

const emptyEntry = (): Partial<KnowledgeEntry> => ({
  kind: "term", category: "专有名词", source: "", target: "", aliases: [],
  target_aliases: [], note: "",
});

const active = (status: string) => ["active", "approved", "trusted"].includes(status);
const statusLabel = (status: string) => status === "candidate" ? "待审核"
  : status === "rejected" ? "已拒绝" : "已生效";
const kindLabel = (kind: string) => kind === "asr_correction" ? "ASR 纠错"
  : kind === "context" ? "背景设定" : "术语";

export function GlossaryManagerModal() {
  const open = uiStore.use(s => s.gmOpen);
  const preselect = uiStore.use(s => s.gmPreselect);
  const items = glossaryStore.use(s => s.items);
  const [cur, setCur] = useState<GlossItem | null>(null);
  const [entries, setEntries] = useState<KnowledgeEntry[]>([]);
  const [orig, setOrig] = useState("");
  const [name, setName] = useState("");
  const [filter, setFilter] = useState<Filter>("active");
  const [draft, setDraft] = useState<Partial<KnowledgeEntry>>(emptyEntry());
  const [adding, setAdding] = useState(false);
  const [baseDirty, setBaseDirty] = useState(false);
  const [entryDirty, setEntryDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  const sourceRef = useRef<HTMLInputElement>(null);

  const applyDetail = (base: GlossItem, nextEntries: KnowledgeEntry[]) => {
    setCur(base); setOrig(base.name); setName(base.name);
    setEntries(nextEntries);
    setDraft(emptyEntry()); setAdding(false); setBaseDirty(false); setEntryDirty(false);
  };

  const loadDetail = async (baseName: string) => {
    const d = await loadKnowledgeBase(baseName);
    applyDetail(d.base, d.entries);
  };

  useLayoutEffect(() => {
    if (!open) return;
    setCur(null); setEntries([]); setOrig(""); setBaseDirty(false); setEntryDirty(false);
    setAdding(false); setLoading(true);
    let cancelled = false;
    void (async () => {
      try {
        const { items: fresh } = await loadGlossSets(true);
        const wanted = fresh.some(x => x.name === preselect) ? preselect : fresh[0]?.name;
        if (!wanted || cancelled) return;
        const d = await loadKnowledgeBase(wanted);
        if (!cancelled) applyDetail(d.base, d.entries);
      } catch (e) {
        if (!cancelled) toast(errText(e) || "知识库加载失败", true);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const hasUnsaved = baseDirty || entryDirty;
  const selectBase = async (baseName: string) => {
    if (hasUnsaved && !await confirm("有未保存的修改，确定切换知识库吗？", "放弃修改")) return;
    setBusy(true); setLoading(true);
    try { await loadDetail(baseName); }
    catch (e) { toast(errText(e) || "知识库加载失败", true); }
    finally { setBusy(false); setLoading(false); }
  };

  const newBase = async () => {
    if (hasUnsaved && !await confirm("有未保存的修改，确定新建吗？", "放弃修改")) return;
    const base = emptyBase();
    setCur(base); setOrig(""); setName(""); setEntries([]);
    setDraft(emptyEntry()); setAdding(false); setBaseDirty(true); setEntryDirty(false);
    requestAnimationFrame(() => nameRef.current?.focus());
  };

  const saveBase = async () => {
    if (!cur?.can_edit || busy) return;
    const trimmed = name.trim();
    if (!trimmed) { toast("请先给知识库起个名字", true); nameRef.current?.focus(); return; }
    setBusy(true);
    const end = beginLoading("正在保存知识库…");
    try {
      const d = await saveKnowledgeBase(trimmed, orig || null);
      await loadGlossSets(true);
      await loadDetail(d.name);
      toast("知识库已保存");
    } catch (e) { toast(errText(e) || "保存失败", true); }
    finally { end(); setBusy(false); }
  };

  const removeBase = async () => {
    if (!cur?.can_edit || !orig || busy) return;
    if (!await confirm(`删除知识库「${orig}」？\n其中的学习记录与证据都会删除。`, "删除")) return;
    setBusy(true);
    const end = beginLoading("正在删除知识库…");
    try {
      await deleteGlossSet(orig);
      const { items: fresh } = await loadGlossSets(true);
      if (fresh[0]) await loadDetail(fresh[0].name);
      else { setCur(null); setOrig(""); setEntries([]); }
      toast("已删除");
    } catch (e) { toast(errText(e) || "删除失败", true); }
    finally { end(); setBusy(false); }
  };

  const editEntry = async (entry?: KnowledgeEntry) => {
    if (entryDirty && !await confirm("当前条目尚未保存，确定放弃吗？", "放弃修改")) return;
    setDraft(entry ? { ...entry, aliases: [...entry.aliases], target_aliases: [...entry.target_aliases] }
      : emptyEntry());
    setAdding(!entry); setEntryDirty(false);
    if (!entry) requestAnimationFrame(() => sourceRef.current?.focus());
  };

  const patchDraft = (patch: Partial<KnowledgeEntry>) => {
    setDraft(value => ({ ...value, ...patch }));
    setEntryDirty(true);
  };

  const saveEntry = async () => {
    if (!cur?.can_edit || !orig || busy) return;
    if (!String(draft.source || "").trim() || !String(draft.target || "").trim()) {
      toast("请填写名称/原文和对应译名/内容", true); return;
    }
    setBusy(true);
    try {
      await saveKnowledgeEntry(orig, draft);
      await loadDetail(orig);
      await loadGlossSets(true);
      toast("知识条目已生效");
    } catch (e) { toast(errText(e) || "条目保存失败", true); }
    finally { setBusy(false); }
  };

  const review = async (entry: KnowledgeEntry, action: "approve" | "reject") => {
    if (!cur?.can_edit || busy) return;
    setBusy(true);
    try {
      await reviewKnowledgeEntries(orig, [entry.id], action);
      await loadDetail(orig); await loadGlossSets(true);
      toast(action === "approve" ? "候选已批准并生效" : "候选已拒绝");
    } catch (e) { toast(errText(e) || "审核失败", true); }
    finally { setBusy(false); }
  };

  const removeEntry = async (entry: KnowledgeEntry) => {
    if (!cur?.can_edit || busy) return;
    if (!await confirm(`删除知识「${entry.source}」？`, "删除")) return;
    setBusy(true);
    try {
      await deleteKnowledgeEntry(orig, entry.id);
      await loadDetail(orig); await loadGlossSets(true);
      toast("知识条目已删除");
    } catch (e) { toast(errText(e) || "删除失败", true); }
    finally { setBusy(false); }
  };

  const visible = useMemo(() => entries.filter(entry => filter === "all"
    || filter === "active" && active(entry.status) || entry.status === filter), [entries, filter]);
  const counts = useMemo(() => ({
    active: entries.filter(e => active(e.status)).length,
    candidate: entries.filter(e => e.status === "candidate").length,
    rejected: entries.filter(e => e.status === "rejected").length,
    all: entries.length,
  }), [entries]);
  const readOnly = !cur?.can_edit;
  const isContext = draft.kind === "context";
  const isAsr = draft.kind === "asr_correction";

  const requestClose = async () => {
    if (hasUnsaved && !await confirm("有未保存的修改，确定关闭吗？", "放弃修改")) return;
    closeGlossaryManager(orig);
  };

  return (
    <div id="gmmask" className={"mask gm-mask" + (open ? " on" : "")}
      onClick={e => { if (e.target === e.currentTarget) requestClose(); }}>
      <div className="modal gm">
        <h2>自学习知识库</h2>
        <div className="gm-wrap">
          {loading && <div className="gm-loading" role="status" aria-live="polite">
            <span className="gm-spinner" aria-hidden="true"></span>
            <span>正在加载知识库…</span>
          </div>}
          <aside className="gm-side">
            <div className="gm-list">
              {cur && !orig && <button className="gm-item on"><span className="n">{name || "未命名"}</span>
                <span className="m">新建中</span></button>}
              {items.map(item => <button key={item.name}
                className={"gm-item" + (orig === item.name ? " on" : "")}
                disabled={loading || busy}
                onClick={() => selectBase(item.name)}>
                <span className="n">{item.name}</span>
                <span className="m">{item.entry_count} 条 · 待审 {item.status_counts?.candidate || 0}</span>
              </button>)}
            </div>
            <button className="btn" disabled={loading || busy} onClick={newBase}>＋ 新建知识库</button>
          </aside>

          {cur ? <main className="gm-main">
            <div className="gm-head">
              <input ref={nameRef} className="gm-name" maxLength={40} value={name} disabled={readOnly}
                placeholder="知识库名称" onChange={e => { setName(e.target.value); setBaseDirty(true); }} />
              <span className={"gm-tag" + (readOnly ? "" : " mine")}>{readOnly ? "只读" : "可编辑"}</span>
              {!readOnly && <button className="btn primary" disabled={busy || !baseDirty} onClick={saveBase}>
                {orig ? "保存设置" : "创建"}</button>}
            </div>

            {orig ? <>
              <div className="gm-tabs">
                {(["active", "candidate", "rejected", "all"] as Filter[]).map(key =>
                  <button key={key} className={filter === key ? "on" : ""} onClick={() => setFilter(key)}>
                    {key === "active" ? "已生效" : key === "candidate" ? "待审核" : key === "rejected" ? "已拒绝" : "全部"}
                    <b>{counts[key]}</b>
                  </button>)}
                {!readOnly && <button className="gm-add" onClick={() => editEntry()}>＋ 人工添加</button>}
              </div>

              <div className="gm-content">
                <section className="gm-entry-list">
                  {visible.map(entry => <article key={entry.id}
                    className={"gm-entry " + entry.status + (draft.id === entry.id ? " on" : "")}
                    onClick={() => editEntry(entry)}>
                    <div className="gm-entry-top">
                      <span className="gm-kind">{kindLabel(entry.kind)}</span>
                      <span className="gm-status">{statusLabel(entry.status)}</span>
                      <span className="gm-confidence">置信 {entry.confidence}/9</span>
                    </div>
                    <strong>{entry.source}</strong>
                    <div className="gm-target">{entry.target}</div>
                    {!!entry.aliases.length && <div className="gm-alias">别名：{entry.aliases.join("、")}</div>}
                    <div className="gm-entry-meta">{entry.origin === "learned" ? "字幕学习" : "人工"}
                      · 出现 {entry.occurrences} 次 · 证据 {entry.evidence.length} 条</div>
                    {entry.status === "candidate" && !readOnly && <div className="gm-review">
                      <button className="btn primary" onClick={e => { e.stopPropagation(); review(entry, "approve"); }}>批准</button>
                      <button className="btn" onClick={e => { e.stopPropagation(); review(entry, "reject"); }}>拒绝</button>
                    </div>}
                  </article>)}
                  {!visible.length && <div className="gm-empty">这个分类里还没有知识条目</div>}
                </section>

                <section className={"gm-editor" + (!draft.id && !adding ? " is-placeholder" : "")}>
                  {!draft.id && !adding ? <div className="gm-editor-placeholder">
                    <span>选择左侧知识条目进行编辑</span>
                    {!readOnly && <button className="btn primary" onClick={() => editEntry()}>＋ 人工添加</button>}
                  </div> : <>
                  <h3>{draft.id ? "编辑知识" : "添加知识"}</h3>
                  <label>类型<select value={draft.kind} disabled={readOnly}
                    onChange={e => patchDraft({ kind: e.target.value as KnowledgeEntry["kind"] })}>
                    <option value="term">固定术语</option><option value="asr_correction">ASR 纠错</option>
                    <option value="context">背景设定</option>
                  </select></label>
                  {draft.kind === "term" && <label>分类<select value={draft.category} disabled={readOnly}
                    onChange={e => patchDraft({ category: e.target.value })}>
                    <option>专有名词</option><option>人物</option><option>俗语</option><option>其他</option>
                  </select></label>}
                  <label>{isContext ? "标题" : isAsr ? "常见误听" : "日文原词"}
                    <input ref={sourceRef} value={draft.source || ""} disabled={readOnly}
                      onChange={e => patchDraft({ source: e.target.value })} /></label>
                  <label>{isContext ? "长期事实/设定" : isAsr ? "正确日文" : "固定中文译名"}
                    <textarea value={draft.target || ""} disabled={readOnly}
                      onChange={e => patchDraft({ target: e.target.value })} /></label>
                  {!isContext && <label>日文别名（逗号分隔）
                    <input value={(draft.aliases || []).join(", ")} disabled={readOnly}
                      onChange={e => patchDraft({ aliases: e.target.value.split(/[,，]/).map(x => x.trim()).filter(Boolean) })} /></label>}
                  <label>说明/使用约束<input value={draft.note || ""} disabled={readOnly}
                    onChange={e => patchDraft({ note: e.target.value })} /></label>
                  {!!draft.evidence?.length && <div className="gm-evidence">
                    <b>最近证据</b>{draft.evidence.slice(-3).reverse().map((ev, i) =>
                      <div key={`${ev.video_id}-${ev.seq}-${i}`}><span>{ev.title || ev.video_id} · #{ev.seq}</span>
                        <p>{ev.ja}{ev.zh ? ` → ${ev.zh}` : ""}</p></div>)}
                  </div>}
                  {!readOnly && <div className="gm-editor-actions">
                    {draft.id && <button className="btn danger" onClick={() => removeEntry(draft as KnowledgeEntry)}>删除</button>}
                    <span></span><button className="btn" onClick={() => editEntry()}>清空</button>
                    <button className="btn primary" disabled={busy || !entryDirty} onClick={saveEntry}>保存并生效</button>
                  </div>}
                  </>}
                </section>
              </div>
            </> : <div className="gm-create-hint">创建后即可添加知识；校订字幕并保存后，在编辑器点击“学习知识”提取候选与证据。</div>}

            <div className="gm-foot">
              {!readOnly && orig && <button className="btn danger" onClick={removeBase}>删除整库</button>}
              <span className="sp"></span>
              <span className="gm-note">高置信知识自动生效，低置信知识进入待审核</span>
            </div>
          </main> : <div className="gm-blank"><div className="big">◫</div>
            <div>还没有知识库<br />新建后，可从人工确认的字幕中提取可复用知识</div></div>}
        </div>
        <div className="modal-foot"><button className="btn primary" onClick={requestClose}>关闭</button></div>
      </div>
    </div>
  );
}
