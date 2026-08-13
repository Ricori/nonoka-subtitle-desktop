import { useEffect, useRef, useState } from 'react';
import { CheckLine } from '../CheckLine';
import { CustomSelect, type SelectOption } from '../CustomSelect';
import {
  closeSpeakers, glossaryStore, loadAxisFile, openGlossaryManager, setAxisKind, setAxisMode,
  setCorrectOn, setGlossValue, setSpkNum, setSpkOn, setTranslateOn, setTranslationPrompt,
  setTranslationPromptHasStyle,
} from '../../store/glossaryStore';
import { uiStore } from '../../store/uiStore';
import { AXIS_KIND_HINT, AXIS_KIND_LABEL, type AxisKind } from '../../lib/assAxis';

const SYSTEM_TRANSLATION_PROMPT_EXAMPLE = `## 翻译风格与取舍
- 语序重组（倒装修正）：日语口语常先抛结论/谓语，再补主语、原因、宾语；译文按中文逻辑把「其实」「因为」「当时」等提前，把后置的补充成分融回主句，绝不照搬日语语序。例：「それもあるのよ。素材が、実は。」→「其实那个的素材 也是有的哦」；「撮ってて。メイキングを。」→「当时还拍了花絮」。
- 强力去噪：直播口语废话极多，大胆删。无意义填充词（あの、その、えっと、作口头禅的 なんか、作发语词的 ま、句尾无意义的 ね）直接不译；重复如「もう、もうやだ」只译“真是讨厌”。但带强烈情绪（惊吓、破防）的复读要保留以增强节目效果（如恐惧下的“死了死了死了”）。主播说错立即改口只翻改口后的正确内容；若“嘴瓢”本身成了笑点或被自己吐槽则保留。
- 状态拟声词：主播常把漫画/剧本里的状态词念出来，按语境转成带括号的动作描写或网感拟声词。すやーー→呼噜噜——／Zzz——；キリッ→（认真脸）／（耍帅）；そわそわ→（急急急）／（搓手期待）。
- 一致性：人名、地名、游戏/专有术语全片统一；若提供 Background 段，遵循其中的译名与设定。作品/游戏/影视/节目/曲名统一用『』包裹（如『家庭教师HITMAN REBORN!』）。
- 标点与括号（最高优先级）：
   - 绝不使用中文逗号（，）和句号（。）。需要停顿、断句、语气舒缓处一律用空格代替，用空格长短在视觉上重构主播的换气点。
   - 游戏台词、被读出的观众评论用「」精准包裹，与主播场外吐槽区分开（弹幕统一格式「弹幕：XXX」；转述他人对话/企划名也用「」）。
   - 括号吐槽：适语境用带括号补充增强实况感，如（棒读）（认真脸）。表达敷衍、假哭、弱气吐槽、无感情捧哏时，克制地用单边括号“（”收尾（如：凶杀案 好害怕（）。
   - 除上述『』「」（）这几种功能性括号外，正文不加任何其它标点。
- 风格——去翻译腔、轻小说/Vtuber 语感：彻底打散日语语法，用中文母语短平快句式重组（“什么鬼啊这是”“你在干嘛啦”“啥情况啊”）。ちゃん译“酱”。按亲疏关系删生硬的“前辈”称呼、改熟络口吻。活用圈内高频梗与网感词：いつもの→老传统了、発売日はよ→发售日搞快点、人の心ない→官方简直没有心；绷不住了/难蚌、提刀就砍、光速下线、纯纯风评被害、妥妥的切片素材。情绪词接地气：破防、大杀特杀、草木皆兵、牙白、哈？；句末语气“啦/嘛/啊/哦”塑造熟络氛围。适度保留圈内原生感（余裕、艾玛桑）与病娇/致郁的反差（惨叫译“美味的悲鸣”）。
- 多角色精分：主播无缝切声线模仿其他角色时高度警惕，用格式标注（如 希罗小姐（梅露露 东云春ver.））；按角色调整语气（慌乱破防用“不是的QAQ”“救命”“崩完了啊”等文字辅助）。`;

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
  const translationPrompt = glossaryStore.use(s => s.translationPrompt);
  const translationPromptHasStyle = glossaryStore.use(s => s.translationPromptHasStyle);
  const axisMode = glossaryStore.use(s => s.axisMode);
  const axisFile = glossaryStore.use(s => s.axisFile);
  const axisParse = glossaryStore.use(s => s.axisParse);
  const axisKind = glossaryStore.use(s => s.axisKind);
  const axisError = glossaryStore.use(s => s.axisError);
  const bothOff = !correctOn && !translateOn;
  const [exampleOpen, setExampleOpen] = useState(false);
  const [copyState, setCopyState] = useState<"idle" | "done" | "error">("idle");
  const goRef = useRef<HTMLButtonElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const exampleCloseRef = useRef<HTMLButtonElement>(null);

  // 有产物但还没解析出可用的轴时不能开工（closeSpeakers 也拦一道，这里只是让按钮变灰）。
  // 这时下面的选项也一概不显示：还不知道这份文件是哪种轴，就不知道哪些选项用得上。
  const promptError = /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/.test(translationPrompt)
    ? "包含不允许的控制字符"
    : translationPrompt.length > 2000 ? "不能超过 2000 个字符" : "";
  const axisBlocked = axisMode === "have" && !axisParse;
  const blocked = axisBlocked || !!promptError;
  const showOptions = !axisBlocked;
  // 识别链只在「没有产物」和「空轴」两种情况下会跑；日文/双语轴根本不上传视频
  const willTranscribe = axisMode !== "have" || axisKind === "empty";
  // 轴自己标了说话人（≥2 人）就不必再问也不必跑 diarization——人标的比模型准。
  // 只标了一个人、或整份都没标（实测样本里很常见），就退回手动选。
  const axisSpeakers = axisMode === "have" && axisParse ? axisParse.speakers : [];
  const autoSpeakers = axisSpeakers.length >= 2;

  useEffect(() => {
    if (!open) setExampleOpen(false);
    else if (exampleOpen) exampleCloseRef.current?.focus({ preventScroll: true });
    else goRef.current?.focus({ preventScroll: true });
  }, [open, exampleOpen]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (gmOpen) return; // 管理弹窗叠在最上层时，回车/Esc 都归它
      if (exampleOpen) {
        if (e.key === "Escape") { e.preventDefault(); setExampleOpen(false); }
        return;
      }
      if (e.key === "Escape") { e.preventDefault(); closeSpeakers(false); }
      else if (e.key === "Enter" && !(e.target instanceof HTMLTextAreaElement)) {
        e.preventDefault(); closeSpeakers(true);
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, gmOpen, exampleOpen]);

  const glossOptions: SelectOption[] = loading
    ? [{ value: "", label: "正在加载术语表…" }]
    : [{ value: "", label: "不使用（默认）" }, ...Object.keys(sets ?? {}).map(name => ({ value: name, label: name }))];

  const kindOptions: SelectOption[] = (["empty", "ja", "zh", "bi"] as AxisKind[])
    .map(k => ({ value: k, label: AXIS_KIND_LABEL[k] }));

  // 中文轴/双语轴不发一次 LLM，只是把字幕搬进来；日文轴只补译文
  const importOnly = axisMode === "have" && (axisKind === "zh" || axisKind === "bi");
  const willTranslate = !importOnly && ((axisMode === "have" && axisKind === "ja") || translateOn);
  const goText = importOnly ? "导入" : !willTranscribe ? "开始翻译" : "开始任务";

  const openExample = () => {
    setCopyState("idle");
    setExampleOpen(true);
  };

  const copyExample = async () => {
    try {
      await navigator.clipboard.writeText(SYSTEM_TRANSLATION_PROMPT_EXAMPLE);
      setCopyState("done");
    } catch {
      const area = document.createElement("textarea");
      area.value = SYSTEM_TRANSLATION_PROMPT_EXAMPLE;
      area.style.cssText = "position:fixed;left:-9999px;opacity:0";
      document.body.appendChild(area);
      let copied = false;
      try {
        area.select();
        copied = document.execCommand("copy");
      } catch {
        copied = false;
      } finally {
        area.remove();
      }
      setCopyState(copied ? "done" : "error");
    }
  };

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

          {showOptions && willTranslate && (
            <section className="sec">
              <div className="sec-title prompt-title">
                <span>自定义翻译 Prompt</span>
                <button className="prompt-example-trigger" type="button"
                  aria-haspopup="dialog" onClick={openExample}>查看示例</button>
              </div>
              <div className="field">
                <textarea id="translation-prompt" rows={4} maxLength={2000}
                  placeholder="可选，例如：整体更克制，少用网络流行语；角色名保留英文"
                  value={translationPrompt} onChange={e => setTranslationPrompt(e.target.value)} />
                <CheckLine id="translation-prompt-style" checked={translationPromptHasStyle}
                  onChange={setTranslationPromptHasStyle}>这段 Prompt 已包含完整翻译风格</CheckLine>
                <div className={"hint" + (promptError ? " bad" : "")}>
                  {promptError || (translationPromptHasStyle
                    ? `将替换系统默认翻译风格（${translationPrompt.length}/2000）`
                    : `保留系统默认风格，并把 Prompt 作为附加偏好（${translationPrompt.length}/2000）`)}
                </div>
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

      {exampleOpen && (
        <div className="prompt-example-mask"
          onClick={e => { if (e.target === e.currentTarget) setExampleOpen(false); }}>
          <section className="prompt-example-dialog" role="dialog" aria-modal="true"
            aria-labelledby="translation-prompt-example-title">
            <header className="prompt-example-head">
              <div>
                <h2 id="translation-prompt-example-title">系统翻译 Prompt 示例</h2>
                <p>勾选“已包含完整翻译风格”时，可参照这个结构编写自己的 Prompt。</p>
              </div>
              <button ref={exampleCloseRef} className="prompt-example-close" type="button"
                aria-label="关闭示例" onClick={() => setExampleOpen(false)}>×</button>
            </header>
            <pre className="prompt-example-content" tabIndex={0}>{SYSTEM_TRANSLATION_PROMPT_EXAMPLE}</pre>
            <footer className="prompt-example-foot">
              <span className={"prompt-copy-state" + (copyState === "error" ? " bad" : "")}
                aria-live="polite">
                {copyState === "done" ? "已复制到剪贴板" : copyState === "error" ? "复制失败，请手动选择" : ""}
              </span>
              <div className="prompt-example-actions">
                <button className="btn primary" type="button" onClick={() => void copyExample()}>
                  {copyState === "done" ? "已复制" : "复制 Prompt"}
                </button>
                <button className="btn" type="button"
                onClick={() => setExampleOpen(false)}>关闭</button>
              </div>
            </footer>
          </section>
        </div>
      )}
    </div>
  );
}
