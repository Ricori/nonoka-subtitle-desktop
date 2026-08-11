import { useEffect, useRef, useState, type ClipboardEvent } from 'react';
import { GM_COLS, GM_WIDTHS, csvToRows, errText, rowsToCsv } from '../../utils';
import { beginLoading, confirm, toast } from '../../lib/notify';
import {
  closeGlossaryManager, deleteGlossSet, glossaryStore, loadGlossSets, saveGlossSet,
} from '../../store/glossaryStore';
import { uiStore } from '../../store/uiStore';
import type { GlossItem } from '../../types';

const blankRow = () => ["", "", "", "", "", ""];

// 专业术语表管理：左侧选表，右侧「表格 / CSV」两个视图编辑同一份内容。
// 增删改一律走服务端，权限也以服务端为准；这里的禁用/隐藏只是别让人白点一趟，不是安全边界。
export function GlossaryManagerModal() {
  const open = uiStore.use(s => s.gmOpen);
  const preselect = uiStore.use(s => s.gmPreselect);
  const items = glossaryStore.use(s => s.items);

  const [cur, setCur] = useState<GlossItem | null>(null);
  const [orig, setOrig] = useState("");        // 打开这条时的名字，保存时当 old_name（""=新建）
  const [name, setName] = useState("");         // 名字输入框（受控，左栏「新建中」预览要跟着实时刷新）
  const [view, setView] = useState<"table" | "csv">("table");
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [rows, setRows] = useState<string[][]>([]);   // 表格视图的行数据
  const [csvText, setCsvText] = useState("");         // CSV 视图的原始文本
  const [pendingFocusRow, setPendingFocusRow] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  const tableWrapRef = useRef<HTMLDivElement>(null);

  const applyEntry = (entry: GlossItem | undefined | null, entryName: string) => {
    const parsedRows = entry ? csvToRows(entry.csv) : [];
    const seeded = entry?.can_edit && !parsedRows.length ? [blankRow()] : parsedRows;
    setCur(entry ? { ...entry } : null);
    setOrig(entryName || "");
    setName(entry?.name || "");
    setRows(seeded);
    setCsvText(rowsToCsv(parsedRows));
    setDirty(false);
  };

  // 每次打开：清到空态，等最新清单拉回来后按 preselect 定位（没有就落在第一套上）
  useEffect(() => {
    if (!open) return;
    setCur(null); setOrig(""); setDirty(false);
    let cancelled = false;
    loadGlossSets(true).then(({ items: freshItems }) => {
      if (cancelled) return;
      const want = freshItems.some(x => x.name === preselect) ? preselect : (freshItems[0]?.name ?? null);
      const it = freshItems.find(x => x.name === want);
      applyEntry(it, it?.name || "");
    });
    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const selectEntry = async (targetName: string) => {
    if (dirty && !await confirm("这套术语表有未保存的修改，确定切换吗？", "放弃修改")) return;
    const it = items.find(x => x.name === targetName);
    applyEntry(it, it?.name || "");
  };

  const newEntry = async () => {
    if (dirty && !await confirm("这套术语表有未保存的修改，确定新建吗？", "放弃修改")) return;
    setCur({ name: "", csv: "", can_edit: true, mine: true, rows: 0 });
    setOrig("");
    setName("");
    setRows([blankRow()]);
    setCsvText("");
    setDirty(false);
    setView("table");
    requestAnimationFrame(() => nameRef.current?.focus());
  };

  const readRows = () => rows.filter(r => r.some(Boolean));

  const switchView = (v: "table" | "csv") => {
    if (v === view) return;
    if (v === "csv") setCsvText(rowsToCsv(readRows()));
    else setRows(csvToRows(csvText));
    setView(v);
  };

  const count = view === "table" ? readRows().length : csvToRows(csvText).length;
  const readOnly = !cur || !cur.can_edit;

  const setCell = (rowIdx: number, colIdx: number, value: string) => {
    setRows(rs => rs.map((r, i) => i === rowIdx ? r.map((c, j) => j === colIdx ? value : c) : r));
    setDirty(true);
  };

  const removeRow = (rowIdx: number) => {
    setRows(rs => rs.filter((_, i) => i !== rowIdx));
    setDirty(true);
  };

  const addRow = () => {
    setRows(rs => [...rs, blankRow()]);
    setPendingFocusRow(true);
  };

  useEffect(() => {
    if (!pendingFocusRow) return;
    setPendingFocusRow(false);
    const wrap = tableWrapRef.current;
    const lastInput = wrap?.querySelectorAll("tbody tr")?.[rows.length - 1]?.querySelector("input");
    lastInput?.focus();
    lastInput?.scrollIntoView({ block: "nearest" });
  }, [pendingFocusRow, rows.length]);

  // 表格里整份粘贴：剪贴板是多行内容时按 CSV/TSV 解析后整批追加，单行内容不拦，正常粘进格子里
  const onTablePaste = (e: ClipboardEvent) => {
    if (!cur || !cur.can_edit) return;
    const text = e.clipboardData?.getData("text") || "";
    if (text.split("\n").filter(l => l.trim()).length < 2) return;
    const added = csvToRows(text);
    if (!added.length) return;
    e.preventDefault();
    setRows(rs => [...rs, ...added]);
    setDirty(true);
    toast(`已粘贴 ${added.length} 条`);
  };

  const save = async () => {
    if (!cur || !cur.can_edit || busy) return;
    const trimmedName = name.trim();
    if (!trimmedName) { toast("请先给这套术语表起个名字", true); nameRef.current?.focus(); return; }
    const csv = view === "csv" ? csvText : rowsToCsv(readRows());
    setBusy(true);
    const endLoading = beginLoading("正在保存术语表…");
    try {
      const d = await saveGlossSet(trimmedName, csv, orig || null);
      const { items: freshItems } = await loadGlossSets(true);
      // 用服务端规范化后的那份重铺一遍：用户能直接看到被补齐/丢掉了什么
      const saved = freshItems.find(x => x.name === d.name) || { ...cur, name: d.name, csv: d.csv };
      applyEntry({ ...saved, csv: d.csv }, d.name);
      toast(`已保存 · 共 ${d.rows} 条`);
    } catch (e) {
      toast(errText(e) || "保存失败", true);
    } finally {
      endLoading();
      setBusy(false);
    }
  };

  const del = async () => {
    if (!cur || !orig || !cur.can_edit || busy) return;
    if (!await confirm(`删除术语表「${orig}」？\n已转写完成的字幕不受影响，正在排队的任务会当作没选术语表。`, "删除")) return;
    setBusy(true);
    const endLoading = beginLoading("正在删除术语表…");
    try {
      await deleteGlossSet(orig);
      const { items: freshItems } = await loadGlossSets(true);
      const next = freshItems[0];
      applyEntry(next, next?.name || "");
      toast("已删除");
    } catch (e) {
      toast(errText(e) || "删除失败", true);
    } finally {
      endLoading();
      setBusy(false);
    }
  };

  const requestClose = async () => {
    if (dirty && !await confirm("这套术语表有未保存的修改，确定关闭吗？", "放弃修改")) return;
    setDirty(false);
    closeGlossaryManager(orig);
  };

  // Esc 关闭 / Ctrl+S 保存。两个处理函数每次渲染都是新的，用 ref 转一道，
  // 免得每敲一个字符就把 document 上的监听拆装一遍
  const keyActions = useRef({ requestClose, save });
  keyActions.current = { requestClose, save };

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") { e.preventDefault(); keyActions.current.requestClose(); }
      else if ((e.key === "s" || e.key === "S") && (e.ctrlKey || e.metaKey)) {
        e.preventDefault(); keyActions.current.save();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  return (
    <div id="gmmask" className={"mask gm-mask" + (open ? " on" : "")}
      onClick={e => { if (e.target === e.currentTarget) requestClose(); }}>
      <div className="modal gm">
        <h2>专业术语表</h2>
        <div className="gm-wrap">
          <div className="gm-side">
            <div id="gm-list" className="gm-list">
              {cur && !orig && (
                <button className="gm-item on">
                  <span className="n">{name.trim() || "未命名"}</span>
                  <span className="m">新建中 · 未保存</span>
                </button>
              )}
              {items.map(it => (
                <button key={it.name} className={"gm-item" + (orig === it.name ? " on" : "")}
                  onClick={() => selectEntry(it.name)}>
                  <span className="n">{it.name}</span>
                  <span className="m">{it.rows || 0} 条 · {it.can_edit ? (it.mine ? "我创建" : "可编辑") : "他人创建"}</span>
                </button>
              ))}
            </div>
            <button id="gm-new" className="btn" onClick={newEntry}>＋ 新建术语表</button>
          </div>

          {cur ? (
            <div id="gm-main" className="gm-main">
              <div className="gm-head">
                <input id="gm-name" ref={nameRef} className="gm-name" maxLength={40} spellCheck={false}
                  placeholder="术语表名称（转写时按这个名字选）" value={name} disabled={readOnly}
                  onChange={e => { setName(e.target.value); setDirty(true); }} />
                <span className={"gm-tag" + (readOnly ? "" : " mine")}>
                  {!orig ? "新建" : readOnly ? "他人创建 · 只读" : cur.mine ? "我创建的" : "管理员可改"}
                </span>
                <div className="viewtoggle">
                  <button className={"vbtn" + (view === "table" ? " on" : "")} id="gm-v-table" title="表格编辑"
                    onClick={() => switchView("table")}>▦</button>
                  <button className={"vbtn" + (view === "csv" ? " on" : "")} id="gm-v-csv" title="CSV 文本（可整份粘贴）"
                    onClick={() => switchView("csv")}>✎</button>
                </div>
              </div>

              <div id="gm-table" className="gm-table-wrap" ref={tableWrapRef} hidden={view !== "table"} onPaste={onTablePaste}>
                <table className="gm-tb">
                  <colgroup>
                    {GM_WIDTHS.map((w, i) => <col key={i} style={{ width: w }} />)}
                    {!readOnly && <col style={{ width: "30px" }} />}
                  </colgroup>
                  <thead>
                    <tr>
                      {GM_COLS.map(c => <th key={c}>{c}</th>)}
                      {!readOnly && <th></th>}
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((r, ri) => (
                      <tr key={ri}>
                        {GM_COLS.map((_, ci) => (
                          <td key={ci}>
                            <input value={r[ci] || ""} disabled={readOnly} spellCheck={false}
                              list={ci === 0 ? "gm-cats" : undefined}
                              onChange={e => setCell(ri, ci, e.target.value)} />
                          </td>
                        ))}
                        {!readOnly && (
                          <td>
                            <button className="x" title="删除这一行" onClick={() => removeRow(ri)}>✕</button>
                          </td>
                        )}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <textarea id="gm-csv" className="gm-csv" spellCheck={false} hidden={view === "table"}
                readOnly={readOnly} value={csvText} onChange={e => { setCsvText(e.target.value); setDirty(true); }}
                placeholder={"类别,原词,略称,中文,中文略称,备注\n人物,柊優花,ゆか,优花酱,,\n\n直接把整份 CSV 粘进来即可，表头可有可无，缺的列留空。"}>
              </textarea>

              <div className="gm-foot">
                <button id="gm-addrow" className="btn" hidden={readOnly || view === "csv"} onClick={addRow}>＋ 添加一行</button>
                <span id="gm-count" className="gm-note">共 {count} 条</span>
                <span className="sp"></span>
                <button id="gm-del" className="btn danger" hidden={readOnly || !orig} onClick={del}>删除</button>
                <button id="gm-save" className="btn primary" hidden={readOnly} disabled={busy} onClick={save}>保存</button>
              </div>
            </div>
          ) : (
            <div id="gm-blank" className="gm-blank">
              <div className="big">▤</div>
              <div>{items.length
                ? "从左边选一套术语表查看或编辑"
                : <>还没有任何术语表<br />新建一套，转写时就能选它约束译名</>}</div>
            </div>
          )}
        </div>
        <div className="modal-foot">
          <button id="gm-close" className="btn primary" onClick={requestClose}>关闭</button>
        </div>
      </div>

      <datalist id="gm-cats">
        <option value="专有名词"></option>
        <option value="人物"></option>
        <option value="俗语"></option>
        <option value="其他"></option>
        <option value="ASR纠错"></option>
      </datalist>
    </div>
  );
}
