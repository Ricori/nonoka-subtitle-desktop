import { AvailFilter } from './AvailFilter';
import { CustomSelect, type SelectOption } from './CustomSelect';
import { libraryStore, pickAndImport } from '../store/libraryStore';
import { setFilter, setSortMode, setView, uiStore, type SortMode, type ViewMode } from '../store/uiStore';

const SORT_OPTIONS: SelectOption[] = [
  { value: "new", label: "最近添加" },
  { value: "name", label: "文件名 A-Z" },
  { value: "dur", label: "时长" },
];

// 工具栏：添加按钮、视频计数、搜索框、排序下拉、网格/列表切换
export function ToolsRow() {
  const count = libraryStore.use(s => s.merged.length);
  const filter = uiStore.use(s => s.filter);
  const sortMode = uiStore.use(s => s.sortMode);
  const view = uiStore.use(s => s.view);
  const hasItems = count > 0;

  return (
    <div className="toolsrow" id="toolsrow" hidden={!hasItems}>
      <button id="add" className="hero" onClick={pickAndImport}>
        <span className="hero-icon">＋</span>
        <span className="hero-copy">
          <span className="hero-title">添加视频</span>
          <span className="hero-hint">拖拽到窗口任意位置，或点击选择文件 · mp4 / mov / mkv / webm，单个不超过 2 小时</span>
        </span>
        <span className="hero-deco">✧</span>
      </button>
      <div className="toolbar">
        <div className="toolbar-count" id="wallcount">{hasItems ? `共 ${count} 个视频` : ""}</div>
        <div className="toolbar-tools">
          <div className="search-wrap">
            <span className="search-icon">⌕</span>
            <input id="q" className="search" placeholder="搜索文件名…" value={filter}
              onChange={e => setFilter(e.target.value)} />
          </div>
          <AvailFilter />
          <CustomSelect id="sort" variant="sort" value={sortMode} options={SORT_OPTIONS}
            onChange={v => setSortMode(v as SortMode)} />
          <div className="viewtoggle">
            {(["grid", "list"] as ViewMode[]).map(v => (
              <button key={v} className={"vbtn" + (view === v ? " on" : "")} id={`v-${v}`}
                title={v === "grid" ? "网格视图" : "列表视图"}
                onClick={() => setView(v)}>{v === "grid" ? "▦" : "☰"}</button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
