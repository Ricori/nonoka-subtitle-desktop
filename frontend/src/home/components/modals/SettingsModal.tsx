import { useEffect, useState } from 'react';
import { fmtSize } from '../../utils';
import { clearVideoCache, saveSettings, sessionStore } from '../../store/sessionStore';
import { setSettingsOpen, uiStore } from '../../store/uiStore';

export function SettingsModal() {
  const open = uiStore.use(s => s.settingsOpen);
  const cfg = sessionStore.use(s => s.cfg);
  const [cacheDir, setCacheDir] = useState("");
  const [limit, setLimit] = useState<number | string>(20);
  const [cacheInfo, setCacheInfo] = useState("");

  const readCacheStats = async () => {
    const st = await window.desktop.cacheStats();
    setCacheInfo(`已占用 ${fmtSize(st.bytes)}（${st.files} 个视频副本）`);
  };

  useEffect(() => {
    if (!open) return;
    setCacheDir(cfg?.cacheDir || "");
    setLimit(cfg?.cacheLimitGB || 20);
    readCacheStats();
  }, [open, cfg]);

  const pickDir = async () => {
    const dir = await window.desktop.pickDirectory();
    if (dir) setCacheDir(dir);
  };

  const clear = async () => {
    if (await clearVideoCache()) await readCacheStats();
  };

  return (
    <div id="setmask" className={"mask" + (open ? " on" : "")}
      onClick={e => { if (e.target === e.currentTarget) setSettingsOpen(false); }}>
      <div className="modal">
        <h2>设置</h2>
        <div className="field">
          <label>视频缓存目录</label>
          <div className="row">
            <input id="s-cachedir" type="text" readOnly value={cacheDir} />
            <button id="s-pickdir" className="btn" onClick={pickDir}>更改</button>
          </div>
          <div id="s-cacheinfo" className="hint">{cacheInfo}</div>
        </div>
        <div className="field">
          <label>缓存上限（GB）</label>
          <div className="row">
            <input id="s-limit" type="number" min="1" step="1" value={limit} onChange={e => setLimit(e.target.value)} />
            <button id="s-clear" className="btn danger" onClick={clear}>清理全部缓存</button>
          </div>
        </div>
        <div className="modal-foot">
          <button id="s-cancel" className="btn" onClick={() => setSettingsOpen(false)}>取消</button>
          <button id="s-save" className="btn primary"
            onClick={() => saveSettings({ cacheDir, cacheLimitGB: limit })}>保存</button>
        </div>
      </div>
    </div>
  );
}
