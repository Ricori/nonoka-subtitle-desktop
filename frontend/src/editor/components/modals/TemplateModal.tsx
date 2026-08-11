import { useEffect, useState } from 'react';
import { parseAssTemplate } from '../../ass';
import { refreshAll } from '../../lib/edits';
import { apiUrl, authHeaders } from '../../session';
import { docStore } from '../../store/docStore';
import { modalStore, toast } from '../../store/uiStore';
import { errText } from '../../utils';

/** ASS 样式模板：全站共享一份，仅管理员可改（后端 PUT 也会 403 兜底） */
export function TemplateModal() {
  const open = modalStore.use(s => s.tplOpen);
  const isAdmin = docStore.use(s => s.isAdmin);
  const assTemplate = docStore.use(s => s.assTemplate);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => { if (open) setText(assTemplate); }, [open, assTemplate]);

  const close = () => modalStore.set({ tplOpen: false });

  async function save() {
    setBusy(true);
    try {
      const r = await fetch(apiUrl("/edit/ass-template"), {
        method: "PUT",
        headers: { ...authHeaders(), "Content-Type": "application/json" },
        body: JSON.stringify({ template: text }),
      });
      const d = await r.json().catch(() => ({} as any));
      if (!r.ok) throw new Error(d.detail || "HTTP " + r.status);
      docStore.set({ assTemplate: text });
      parseAssTemplate(text);
      close();
      refreshAll();
      toast("模板已保存（" + (d.styles || []).length + " 个样式，全部视频生效）");
    } catch (e) {
      toast("模板保存失败：" + errText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal" id="tpl-modal" hidden={!open}
      onClick={e => { if (e.target === e.currentTarget) close(); }}>
      <div className="box">
        <button className="x-close" id="tpl-close" title="关闭" onClick={close}>✕</button>
        <h3 id="tpl-title">{isAdmin ? "ASS 样式模板（全局）" : "ASS 样式模板（全局 · 只读）"}</h3>
        <div className="hint">所有视频的预览与导出共用这一份模板。轨道通过样式名绑定其中的 Style；
          可粘贴完整 ASS 头（含 [Script Info]）或仅 [V4+ Styles] 段，[Events] 段会被忽略。</div>
        <div className="hint" id="tpl-readonly-hint" hidden={isAdmin}>这份模板全站共享，仅管理员可修改；
          你可以查看样式名，用于给轨道绑定样式。</div>
        <textarea id="tpl-text" spellCheck={false} wrap="off" readOnly={!isAdmin}
          value={text} onChange={e => setText(e.target.value)} />
        <div className="foot" id="tpl-foot" hidden={!isAdmin}>
          <button className="btn" id="tpl-cancel" onClick={close}>取消</button>
          <button className="btn primary" id="tpl-save" disabled={busy} onClick={save}>保存模板</button>
        </div>
      </div>
    </div>
  );
}
