import { useEffect, useRef } from 'react';
import { CheckLine } from '../CheckLine';
import { CustomSelect, type SelectOption } from '../CustomSelect';
import {
  closeSpeakers, glossaryStore, openGlossaryManager, setCorrectOn, setGlossValue,
  setSpkNum, setSpkOn, setTranslateOn,
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
  const correctOn = glossaryStore.use(s => s.correctOn);
  const translateOn = glossaryStore.use(s => s.translateOn);
  const bothOff = !correctOn && !translateOn;
  const goRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (open) goRef.current?.focus({ preventScroll: true });
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
      <div className="modal speaker-modal">
        <header className="speaker-modal-head">
          <h2>转写选项</h2>
        </header>

        <div className="speaker-modal-body">
          <section className="sec">
            <div className="sec-title">多说话人</div>
            <div className="field">
              <CheckLine id="spk-on" checked={spkOn} onChange={setSpkOn}>自动分轨道</CheckLine>
              <div className="hint">识别谁在说话，字幕按人自动分成多条轨道；两人同时说话处需人工校对</div>
            </div>
            <div className={"field" + (spkOn ? "" : " off")} id="spk-count">
              <div className="row">
                <label htmlFor="spk-num">说话人数量</label>
                <input id="spk-num" type="number" min="2" max="10" step="1" placeholder="自动"
                  disabled={!spkOn} value={spkNum} onChange={e => setSpkNum(e.target.value)} />
              </div>
              <div className="hint">填准确的人数识别更准；不确定就留空，交给模型判断</div>
            </div>
          </section>

          <section className="sec">
            <div className="sec-title">AI 处理环节</div>
            <div className="field">
              <CheckLine id="ai-correct" checked={correctOn} onChange={setCorrectOn}>智能纠错</CheckLine>
              <div className="hint">把被切碎的句子合并，顺手订正听错的词、去掉口癖；关掉则原样保留识别结果</div>
            </div>
            <div className="field">
              <CheckLine id="ai-translate" checked={translateOn} onChange={setTranslateOn}>翻译成中文</CheckLine>
              <div className={"hint" + (bothOff ? " bad" : "")}>
                {bothOff
                  ? "两项都关：只做识别和断句，字幕不经任何 AI 加工"
                  : "关掉则只出日文原文、译文留空，之后仍可在编辑器里逐句重译"}
              </div>
            </div>
          </section>

          <section className="sec">
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
          </section>
        </div>

        <footer className="modal-foot speaker-modal-foot">
          <button id="spk-cancel" className="btn" onClick={() => closeSpeakers(false)}>取消</button>
          <button id="spk-go" ref={goRef} className="btn primary" onClick={() => closeSpeakers(true)}>开始任务</button>
        </footer>
      </div>
    </div>
  );
}
