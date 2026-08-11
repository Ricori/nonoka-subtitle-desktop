import { useRef } from 'react';
import { PopoverMenu } from './PopoverMenu';
import { bootStore, installUpdate } from '../store/bootStore';
import { openGlossaryManager } from '../store/glossaryStore';
import { logout, sessionStore, showLogin } from '../store/sessionStore';
import { closePopover, setSettingsOpen, togglePopover, uiStore } from '../store/uiStore';
import { useTheme } from '../hooks/useTheme';

// 标题栏
export function TopBar() {
  const { theme, toggleTheme } = useTheme();
  const updateBanner = bootStore.use(s => s.updateBanner);
  const ver = sessionStore.use(s => s.cfg?.version);
  const keybar = sessionStore.use(s => s.keybar);
  const gearOpen = uiStore.use(s => s.popover === "gear");
  const gearRef = useRef<HTMLButtonElement>(null);

  return (
    <>
      <div className="topbar">
        <div className="brand">NONOKA<span>字幕</span><small>桌面版</small><small id="ver">{ver ? `v${ver}` : ""}</small></div>
        <div className="spacer"></div>
        <button id="updatebtn" className="tbtn update" hidden={!updateBanner}
          title="新版本已就绪，点击重启安装" onClick={installUpdate}>
          {updateBanner ? `⭮ 更新到 v${updateBanner.version}` : ""}
        </button>
        <button id="themebtn" className="tbtn" title="切换亮/暗主题" onClick={toggleTheme}>
          {theme === "light" ? "☀" : "☾"}
        </button>
        <button id="gear" ref={gearRef} className="tbtn" title="更多" aria-haspopup="menu"
          aria-expanded={gearOpen}
          onClick={e => { e.stopPropagation(); togglePopover("gear"); }}>⋮</button>
      </div>

      <PopoverMenu id="gear" anchorRef={gearRef}>
        <button role="menuitem" onClick={() => { closePopover(); openGlossaryManager(); }}>
          <span className="ico">▤</span>专业术语表
        </button>
        <button role="menuitem" onClick={() => { closePopover(); setSettingsOpen(true); }}>
          <span className="ico">⚙</span>设置
        </button>
        <button className="danger" role="menuitem" onClick={() => { closePopover(); logout(); }}>
          <span className="ico">⏻</span>退出登录
        </button>
      </PopoverMenu>

      <div id="keybar" className="keybar" hidden={!keybar}>
        <span id="keymsg">{keybar?.msg}</span>
        <button id="keyfix" hidden={!keybar?.fix} onClick={() => showLogin()}>重新登录</button>
      </div>
    </>
  );
}
