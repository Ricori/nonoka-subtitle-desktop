import { useEffect, useRef } from 'react';
import { CustomSelect, type SelectOption } from '../CustomSelect';
import {
  closeSpeakers, glossaryStore, openGlossaryManager, setGlossValue, setSpkNum, setSpkOn,
} from '../../store/glossaryStore';
import { uiStore } from '../../store/uiStore';

// 转写选项弹窗：多说话人 + 术语表
export function SpeakerModal() {
  const open = uiStore.use(s => s.speakerOpen);
  const gmOpen = uiStore.use(s => s.gmOpen);
  const sets = glossaryStore.use(s => s.sets);
  // 「一套都没有」也是加载完成，不能靠 sets 是不是空来判断，否则下拉会一直停在加载中
  const loading = glossaryStore.use(s => s.sets === null);
  const spkOn = glossaryStore.use(s => s.spkOn);
  const spkNum = glossaryStore.use(s => s.spkNum);
  const glossValue = glossaryStore.use(s => s.glossValue);
  const goRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (open) goRef.current?.focus();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (gmOpen) return; // 管理弹窗叠在最上层时，回车/Esc 都归它
      if (e.key === "Escape") { e.preventDefault(); closeSpeakers(false); }
      else if (e.key === "Enter") { e.preventDefault(); closeSpeakers(true); }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, gmOpen]);

  const glossOptions: SelectOption[] = loading
    ? [{ value: "", label: "正在加载术语表…" }]
    : [{ value: "", label: "不使用（默认）" }, ...Object.keys(sets ?? {}).map(name => ({ value: name, label: name }))];

  return (
    <div id="spkmask" className={"mask" + (open ? " on" : "")}
      onClick={e => { if (e.target === e.currentTarget) closeSpeakers(false); }}>
      <div className="modal">
        <h2>转写选项</h2>
        <div className="sec">
          <div className="sec-title">多说话人</div>
          <div className="field">
            <label className="chkline">
              <input id="spk-on" type="checkbox" checked={spkOn} onChange={e => setSpkOn(e.target.checked)} />
              <span>自动分轨道</span>
            </label>
            <div className="hint">识别谁在说话，字幕按人自动分成多条轨道；两人同时说话处需人工校对</div>
          </div>
          <div className="field" id="spk-count" hidden={!spkOn}>
            <label htmlFor="spk-num">说话人数量</label>
            <input id="spk-num" type="number" min="2" max="10" step="1" placeholder="自动"
              value={spkNum} onChange={e => setSpkNum(e.target.value)} />
            <div className="hint">填准确的人数识别更准；不确定就留空，交给模型判断</div>
          </div>
        </div>
        <div className="sec">
          <div className="sec-title">专业术语表</div>
          <div className="field">
            <div className="row">
              <CustomSelect id="gloss-sel" value={glossValue} options={glossOptions}
                onChange={setGlossValue} disabled={loading} />
              <button id="gloss-edit" className="btn" type="button"
                onClick={() => openGlossaryManager(glossValue)}>编辑</button>
            </div>
            <div className="hint">选一套注入转写，约束专有名词/人名/黑话的译法；「编辑」可查看、新建、修改</div>
          </div>
        </div>
        <div className="modal-foot">
          <button id="spk-cancel" className="btn" onClick={() => closeSpeakers(false)}>取消</button>
          <button id="spk-go" ref={goRef} className="btn primary" onClick={() => closeSpeakers(true)}>开始任务</button>
        </div>
      </div>
    </div>
  );
}
