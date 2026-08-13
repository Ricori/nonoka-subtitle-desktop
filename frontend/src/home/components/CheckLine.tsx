import type { ReactNode } from 'react';

interface CheckLineProps {
  id: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  children: ReactNode;
}

/** 勾选行（复选框 + 说明文字同排）。 */
export function CheckLine({ id, checked, onChange, children }: CheckLineProps) {
  return (
    <label className="chkline">
      <input id={id} type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} />
      <span>{children}</span>
    </label>
  );
}
