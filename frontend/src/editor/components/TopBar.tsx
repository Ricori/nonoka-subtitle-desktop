import { useState } from 'react';
import { shallowEqual } from '../../home/lib/createStore';
import { useTheme } from '../../home/hooks/useTheme';
import { exportAss } from '../lib/exportAss';
import { docStore } from '../store/docStore';
import { openExport } from '../store/exportStore';
import { manualSave, saveStore } from '../store/saveStore';
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
              {
                label: "导出 ASS 字幕",
                onClick: async () => { setExpBusy(true); try { await exportAss(); } finally { setExpBusy(false); } },
              },
              { label: "导出 MP4（内嵌字幕）", onClick: () => openExport(null) },
            ], e.currentTarget)}>导出 ▾</button>
          <button className="btn primary" id="btn-save" title="保存 (Ctrl+S)"
            onClick={() => void manualSave()}>保存</button>
        </div>
      </div>
    </header>
  );
}
