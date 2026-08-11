import { useRef } from 'react';
import { PopoverMenu } from './PopoverMenu';
import { closePopover, togglePopover, uiStore } from '../store/uiStore';

export interface SelectOption { value: string; label: string; }

interface CustomSelectProps {
  id: string;
  value: string;
  options: SelectOption[];
  onChange: (value: string) => void;
  variant?: "fake" | "sort";
  disabled?: boolean;
}

// 自定义下拉：外观是按钮+浮动菜单，语义等价于原生 <select>（value/onChange）
export function CustomSelect({ id, value, options, onChange, variant = "fake", disabled }: CustomSelectProps) {
  const popoverId = `select:${id}`;
  const open = uiStore.use(s => s.popover === popoverId);
  const btnRef = useRef<HTMLButtonElement>(null);

  const selected = options.find(o => o.value === value);
  const btnClass = (variant === "sort" ? "sortsel" : "fake-select") + " custom-sel-btn" + (open ? " focus-ring" : "");

  return (
    <>
      <button type="button" id={id} ref={btnRef} className={btnClass}
        style={variant === "sort" ? { textAlign: "left" } : undefined}
        disabled={disabled}
        onClick={() => togglePopover(popoverId)}>
        <span style={{ display: "block", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
          {selected ? selected.label : ""}
        </span>
      </button>
      <PopoverMenu id={popoverId} anchorRef={btnRef} className="custom-select-menu" matchWidth>
        {options.map(o => (
          <button key={o.value} type="button" title={o.label}
            style={o.value === value ? { color: "var(--accent)" } : undefined}
            onClick={() => { onChange(o.value); closePopover(); }}>
            {o.label}
          </button>
        ))}
      </PopoverMenu>
    </>
  );
}
