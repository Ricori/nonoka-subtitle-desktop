import { bootStore, clearNews } from '../../store/bootStore';

// 更新说明：主进程只在更新后首次启动给出内容，取走即删
export function NewsModal() {
  const news = bootStore.use(s => s.news);
  const open = !!news;
  return (
    <div id="newsmask" className={"mask" + (open ? " on" : "")}
      onClick={e => { if (e.target === e.currentTarget) clearNews(); }}>
      <div className="modal news">
        <h2 id="news-title">{news ? `v${news.version} 更新内容` : ""}</h2>
        <div id="news-text">{news?.notes}</div>
        <div className="modal-foot">
          <button id="news-ok" className="btn primary" onClick={clearNews}>知道了</button>
        </div>
      </div>
    </div>
  );
}
