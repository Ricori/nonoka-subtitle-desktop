import { useRef } from 'react';
import { PopoverMenu } from './PopoverMenu';
import { clearAvail, toggleAvail, togglePopover, uiStore } from '../store/uiStore';
import { AVAIL_META, AVAIL_ORDER } from '../utils';

const POPOVER_ID = "select:avail";

// 视频状态多选筛选：点条目只切勾不收菜单（uiStore 的 document 监听放行 .topmenu 内部的点击）
export function AvailFilter() {
  const open = uiStore.use(s => s.popover === POPOVER_ID);
  const avail = uiStore.use(s => s.avail);
  const btnRef = useRef<HTMLButtonElement>(null);

  const names = avail.map(k => AVAIL_META[k].label);
  const label = !names.length ? "全部状态" : names.length === 1 ? names[0] : `${names.length} 种状态`;

  return (
    <>
      <button type="button" id="availfilter" ref={btnRef}
        className={"sortsel availsel custom-sel-btn" + (avail.length ? " on" : "") + (open ? " focus-ring" : "")}
        title={names.length ? `已筛选：${names.join("、")}` : "按视频状态筛选"}
        onClick={() => togglePopover(POPOVER_ID)}>
        {label}
      </button>
      <PopoverMenu id={POPOVER_ID} anchorRef={btnRef} className="custom-select-menu">
        <button type="button" className={avail.length ? undefined : "picked"} onClick={clearAvail}>
          <span className="ico">{avail.length ? "" : "✓"}</span>全部状态
        </button>
        {AVAIL_ORDER.map(k => {
          const on = avail.includes(k);
          return (
            <button key={k} type="button" className={on ? "picked" : undefined} onClick={() => toggleAvail(k)}>
              <span className="ico">{on ? "✓" : ""}</span>{AVAIL_META[k].label}
            </button>
          );
        })}
      </PopoverMenu>
    </>
  );
}
