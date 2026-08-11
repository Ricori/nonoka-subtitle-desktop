import { useState } from 'react';
import { VideoCard } from './VideoCard';
import { libraryStore, pickAndImport, useVisibleItems } from '../store/libraryStore';
import { uiStore } from '../store/uiStore';

function Mascot() {
  const [failed, setFailed] = useState(false);
  if (failed) return <div className="big">🎀</div>;
  return <img className="mascot" src="./assets/mascot.png" alt="" onError={() => setFailed(true)} />;
}

// 视频墙
export function VideoWall() {
  const items = useVisibleItems();
  const loaded = libraryStore.use(s => s.loaded);
  const filter = uiStore.use(s => s.normalizedFilter);
  const view = uiStore.use(s => s.view);
  const wallClass = "wall" + (view === "list" ? " list" : "");

  if (!items.length) {
    if (!loaded && !filter) {
      return (
        <div id="wall" className={wallClass}>
          <div className="empty loading">
            <div className="spin"></div>
            <div className="t">正在加载视频库…</div>
          </div>
        </div>
      );
    }
    if (filter) {
      return (
        <div id="wall" className={wallClass}>
          <div className="empty" id="emptybox">
            <div className="big">(・_・;)</div>
            <div className="t">没有匹配的视频</div>
            <div className="s">换个关键词试试</div>
          </div>
        </div>
      );
    }
    // 空库时显示下 nonoka 立绘
    return (
      <div id="wall" className={wallClass}>
        <div className="empty" id="emptybox" onClick={() => pickAndImport()}>
          <Mascot />
          <div className="t"><span className="bubble">把视频拖进来吧～ 或点击选择 ⸜(｡˃ ᵕ ˂ )⸝</span></div>
          <div className="s">支持 mp4 / mov / mkv / webm，单个不超过 2 小时</div>
        </div>
      </div>
    );
  }

  return (
    <div id="wall" className={wallClass}>
      {items.map(it => <VideoCard key={it.localId || it.id} it={it} />)}
    </div>
  );
}
