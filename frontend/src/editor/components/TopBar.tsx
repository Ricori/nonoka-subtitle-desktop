import { useState } from 'react';
import { shallowEqual } from '../../home/lib/createStore';
import { useTheme } from '../../home/hooks/useTheme';
import { exportAss } from '../lib/exportAss';
import { exportSrt } from '../lib/exportSrt';
import type { SrtLang } from '../lib/srtBuild';
import { docStore } from '../store/docStore';
import { openExport } from '../store/exportStore';
import { manualSave, saveStore } from '../store/saveStore';
import { knowledgeLearningStore, learnKnowledge } from '../store/knowledgeLearningStore';
import { showCtx } from '../store/uiStore';

/**
 * 沉浸式标题栏：这一条同时是窗口标题栏（可拖动/双击最大化），右上角那三个
 * 按钮由系统画在 titleBarOverlay 里，故右侧留出 env(titlebar-area-*) 的空档。
 */
export function TopBar() {
  const { theme, toggleTheme } = useTheme();
  const title = docStore.use(s => s.title);
  const { stateText, stateCls } = saveStore.use(
    s => ({ stateText: s.stateText, stateCls: s.stateCls }), shallowEqual);
  const [expBusy, setExpBusy] = useState(false);
  const learningBusy = knowledgeLearningStore.use(s => s.busy);
  const canLearn = docStore.use(s => s.canLearnKnowledge);
  const knowledgeBase = docStore.use(s => s.knowledgeBase);

  // 导出期间按钮置灰，别让人连点出两次保存对话框
  const runExport = async (fn: () => Promise<void>) => {
    setExpBusy(true);
    try { await fn(); } finally { setExpBusy(false); }
  };
  const srtItem = (label: string, lang: SrtLang) =>
    ({ label, onClick: () => void runExport(() => exportSrt(lang)) });

  return (
    <header className="topbar">
      <div className="topbar-side l">
        <div className="brand"><b>NONOKA字幕</b></div>
      </div>
      <div className="filechip"><span className="dot"></span><span id="filetitle">{title}</span></div>
      <div className="topbar-side r">
        <span className={"savestate" + (stateCls ? " " + stateCls : "")} id="savestate">{stateText}</span>
        <div className="btn-group">
          <button className="btn" id="themebtn" title="切换亮/暗主题" onClick={toggleTheme}>
            {theme === "light" ? "☀" : "☾"}
          </button>
          <button className="btn" id="btn-export" title="导出字幕或内嵌字幕的视频" disabled={expBusy}
            onClick={e => showCtx(e, [
              { label: "导出 ASS 字幕", onClick: () => void runExport(exportAss) },
              "-",
              // SRT 把所有未隐藏轨道摊平成一条流（格式本身没有多轨概念），三种语言各存一份
              srtItem("导出 SRT（双语）", "both"),
              srtItem("导出 SRT（仅译文）", "zh"),
              srtItem("导出 SRT（仅原文）", "ja"),
              "-",
              { label: "导出 MP4（内嵌字幕）", onClick: () => openExport(null) },
            ], e.currentTarget)}>导出 ▾</button>
          <button className="btn primary" id="btn-save" title="保存 (Ctrl+S)"
            onClick={() => void manualSave()}>保存</button>
          <button className="btn" id="btn-learn-knowledge"
            title={knowledgeBase ? `让AI从校订字幕中学习，并更新「${knowledgeBase}」知识库` : "本视频没有选择知识库"}
            disabled={!canLearn || learningBusy} onClick={() => void learnKnowledge()}>
            {learningBusy ? "学习中…" : "学习知识"}</button>
        </div>
      </div>
    </header>
  );
}
