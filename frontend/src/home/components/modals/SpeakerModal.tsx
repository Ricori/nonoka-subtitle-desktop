import { useEffect, useRef } from 'react';
import { CheckLine } from '../CheckLine';
import { CustomSelect, type SelectOption } from '../CustomSelect';
import {
  closeSpeakers, glossaryStore, loadAxisFile, openGlossaryManager, setAxisKind, setAxisMode,
  setCorrectOn, setGlossValue, setSpkNum, setSpkOn, setTranslateOn,
} from '../../store/glossaryStore';
import { uiStore } from '../../store/uiStore';
import { AXIS_KIND_HINT, AXIS_KIND_LABEL, type AxisKind } from '../../lib/assAxis';

const fmt = (s: number) => {
  const m = Math.floor(s / 60);
  return `${m}:${String(Math.floor(s % 60)).padStart(2, "0")}`;
};

// 转写选项弹窗。两阶段：先问有没有已有产物，再按答案决定第二阶段显示什么——
// 三种产物走三条完全不同的路（见 lib/assAxis.ts 顶部），能选的选项也各不相同。
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
  const axisMode = glossaryStore.use(s => s.axisMode);
  const axisFile = glossaryStore.use(s => s.axisFile);
  const axisParse = glossaryStore.use(s => s.axisParse);
  const axisKind = glossaryStore.use(s => s.axisKind);
  const axisError = glossaryStore.use(s => s.axisError);
  const bothOff = !correctOn && !translateOn;
  const goRef = useRef<HTMLButtonElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  // 有产物但还没解析出可用的轴时不能开工（closeSpeakers 也拦一道，这里只是让按钮变灰）。
  // 这时下面的选项也一概不显示：还不知道这份文件是哪种轴，就不知道哪些选项用得上。
  const blocked = axisMode === "have" && !axisParse;
  const showOptions = !blocked;
  // 识别链只在「没有产物」和「空轴」两种情况下会跑；日文/双语轴根本不上传视频
  const willTranscribe = axisMode !== "have" || axisKind === "empty";
  // 轴自己标了说话人（≥2 人）就不必再问也不必跑 diarization——人标的比模型准。
  // 只标了一个人、或整份都没标（实测样本里很常见），就退回手动选。
  const axisSpeakers = axisMode === "have" && axisParse ? axisParse.speakers : [];
  const autoSpeakers = axisSpeakers.length >= 2;

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

  const kindOptions: SelectOption[] = (["empty", "ja", "zh", "bi"] as AxisKind[])
    .map(k => ({ value: k, label: AXIS_KIND_LABEL[k] }));

  // 中文轴/双语轴不发一次 LLM，只是把字幕搬进来；日文轴只补译文
  const importOnly = axisMode === "have" && (axisKind === "zh" || axisKind === "bi");
  const goText = importOnly ? "导入" : !willTranscribe ? "开始翻译" : "开始任务";

  return (
    <div id="spkmask" className={"mask" + (open ? " on" : "")}
      onClick={e => { if (e.target === e.currentTarget) closeSpeakers(false); }}>
      <div className="modal speaker-modal">
        <header className="speaker-modal-head">
          <h2>转写选项</h2>
        </header>

        <div className="speaker-modal-body">
          <section className="sec">
            <div className="sec-title">已有产物</div>
            <div className="field axis-pick">
              <button type="button" className={"axis-card" + (axisMode === "none" ? " on" : "")}
                onClick={() => setAxisMode("none")}>
                <b>我没有产物</b>
                <span>从头识别、断句、翻译</span>
              </button>
              <button type="button" className={"axis-card" + (axisMode === "have" ? " on" : "")}
                onClick={() => setAxisMode("have")}>
                <b>我已有轴</b>
                <span>空轴 / 单语轴 / 双语轴</span>
              </button>
            </div>

            {axisMode === "have" && (
              <div className="field">
                <div className="row">
                  <input className="axis-file" readOnly placeholder="未选择文件"
                    value={axisFile} title={axisFile} />
                  <button className="btn" type="button" onClick={() => fileRef.current?.click()}>
                    选择文件
                  </button>
                </div>
                <input ref={fileRef} type="file" accept=".ass,.ssa,.srt" hidden
                  onChange={e => {
                    const f = e.target.files?.[0];
                    // 清掉 value：同一个文件改完再选一次也要触发 change
                    e.target.value = "";
                    if (f) void loadAxisFile(f);
                  }} />
                {axisError
                  ? <div className="hint bad">{axisError}</div>
                  : axisParse
                    ? <>
                      <div className="hint ok">
                        解析出 {axisParse.rows.length} 条，覆盖到 {fmt(axisParse.rows[axisParse.rows.length - 1].t1)}
                        {axisParse.skipped > 0 ? `（跳过 ${axisParse.skipped} 条注释/无效行）` : ""}
                        {axisParse.overlaps > 0 ? `，其中 ${axisParse.overlaps} 处两人同时说话` : ""}
                      </div>
                      <div className="row" style={{ marginTop: 8 }}>
                        <CustomSelect id="axis-kind" value={axisKind} options={kindOptions}
                          onChange={v => setAxisKind(v as AxisKind)} />
                      </div>
                      <div className="hint">{AXIS_KIND_HINT[axisKind]}；判断错了就在上面改</div>
                    </>
                    : <div className="hint">支持 .ass / .srt。最终字幕会严格保持这份轴的每一行时间</div>}
              </div>
            )}
          </section>

          {showOptions && willTranscribe && (
            <section className="sec">
              <div className="sec-title">多说话人</div>
              {autoSpeakers ? (
                // 轴已经逐行标好了谁在说话：不跑 diarization，直接照它分轨。
                // 人标的比模型准，还省掉整片跑一遍模型的时间和钱。
                <div className="field">
                  <div className="hint ok">已从轴里读出 {axisSpeakers.length} 位说话人：{axisSpeakers.join("、")}</div>
                  <div className="hint">字幕按他们自动分轨（取自 ASS 的样式名）</div>
                </div>
              ) : (<>
                <div className="field">
                  <CheckLine id="spk-on" checked={spkOn} onChange={setSpkOn}>自动分轨道</CheckLine>
                  <div className="hint">
                    {axisMode === "have"
                      ? "这份轴没标谁在说话，需要的话由模型识别，字幕按人自动分成多条轨道"
                      : "识别谁在说话，字幕按人自动分成多条轨道；两人同时说话处需人工校对"}
                  </div>
                </div>
                <div className={"field" + (spkOn ? "" : " off")} id="spk-count">
                  <div className="row">
                    <label htmlFor="spk-num">说话人数量</label>
                    <input id="spk-num" type="number" min="2" max="10" step="1" placeholder="自动"
                      disabled={!spkOn} value={spkNum} onChange={e => setSpkNum(e.target.value)} />
                  </div>
                  <div className="hint">填准确的人数识别更准；不确定就留空，交给模型判断</div>
                </div>
              </>)}
            </section>
          )}

          {showOptions && willTranscribe && (
            <section className="sec">
              <div className="sec-title">AI 处理环节</div>
              <div className="field">
                <CheckLine id="ai-correct" checked={correctOn} onChange={setCorrectOn}>智能纠错</CheckLine>
                <div className="hint">
                  {axisMode === "have"
                    // 轴模式下 A 阶段禁合并：行的分组是人打的，模型只订正听错的词
                    ? "订正听错的词、去掉口癖；行的分断由你的轴定死，模型不会合并或丢弃任何一行"
                    : "把被切碎的句子合并，顺手订正听错的词、去掉口癖；关掉则原样保留识别结果"}
                </div>
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
          )}

          {showOptions && !importOnly && (
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
          )}
        </div>

        <footer className="modal-foot speaker-modal-foot">
          <button id="spk-cancel" className="btn" onClick={() => closeSpeakers(false)}>取消</button>
          <button id="spk-go" ref={goRef} className="btn primary" disabled={blocked}
            onClick={() => closeSpeakers(true)}>{goText}</button>
        </footer>
      </div>
    </div>
  );
}
