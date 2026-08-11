import { loadingStore } from '../lib/notify';

export function LoadingOverlay() {
  const open = loadingStore.use(s => s.count > 0);
  const text = loadingStore.use(s => s.text);
  return (
    <div id="opbusy" className={open ? "on" : undefined} role="status" aria-live="polite">
      <div className="opbusy-card"><span className="spin"></span><span id="opbusy-text">{text}</span></div>
    </div>
  );
}
