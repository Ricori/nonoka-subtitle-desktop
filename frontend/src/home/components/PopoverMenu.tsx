import { useLayoutEffect, useRef, type ReactNode, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { placeMenu } from '../utils';
import { uiStore } from '../store/uiStore';

interface PopoverMenuProps {
  id: string;
  anchorRef: RefObject<HTMLElement | null>;
  className?: string;
  role?: string;
  matchWidth?: boolean;
  children: ReactNode;
}

// 顶栏⋮菜单、卡片「⋯」菜单、自定义下拉共用的浮动菜单：挂载进 document.body，
// 按锚点按钮定位（placeMenu，右对齐、超出视口翻到上方），开合状态由 uiStore 统一管理。
export function PopoverMenu({ id, anchorRef, className, role = "menu", matchWidth, children }: PopoverMenuProps) {
  const open = uiStore.use(s => s.popover === id);
  const menuRef = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    if (!open || !anchorRef.current || !menuRef.current) return;
    if (matchWidth) menuRef.current.style.minWidth = anchorRef.current.getBoundingClientRect().width + "px";
    placeMenu(menuRef.current, anchorRef.current);
  }, [open, matchWidth]);

  if (!open) return null;
  return createPortal(
    <div ref={menuRef} className={"topmenu on" + (className ? " " + className : "")} role={role}>
      {children}
    </div>,
    document.body,
  );
}
