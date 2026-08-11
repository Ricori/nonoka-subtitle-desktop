import { useEffect, useRef } from 'react';
import { askStore, closeAsk, setAskValue } from '../../lib/notify';

// 页内确认框（取代 window.confirm），state 来自 lib/notify
export function AskModal() {
  const dialog = askStore.use(s => s.dialog);
  const open = !!dialog;
  const inputRef = useRef<HTMLInputElement>(null);
  const yesRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    if (dialog.isInput) { inputRef.current?.focus(); inputRef.current?.select(); }
    else yesRef.current?.focus();
  }, [open, dialog?.isInput]);

  // 只在有待答的框时才挂监听，别抢走别处的 Esc/Enter
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") { e.preventDefault(); closeAsk(false); }
      else if (e.key === "Enter") { e.preventDefault(); closeAsk(true); }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open]);

  return (
    <div id="askmask" className={"mask ask-mask" + (open ? " on" : "")}
      onClick={e => { if (e.target === e.currentTarget) closeAsk(false); }}>
      <div className="modal ask">
        <div id="ask-text">{dialog?.text}</div>
        <div className="field" id="ask-field" hidden={!dialog?.isInput}>
          <input id="ask-input" ref={inputRef} spellCheck={false}
            value={dialog?.value ?? ""} onChange={e => setAskValue(e.target.value)} />
        </div>
        <div className="modal-foot">
          <button id="ask-no" className="btn" onClick={() => closeAsk(false)}>取消</button>
          <button id="ask-yes" ref={yesRef} className="btn primary" onClick={() => closeAsk(true)}>
            {dialog?.okText || "确定"}
          </button>
        </div>
      </div>
    </div>
  );
}
