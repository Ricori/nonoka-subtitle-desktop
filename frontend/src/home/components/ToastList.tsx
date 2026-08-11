import { toastStore } from '../lib/notify';

export function ToastList() {
  const items = toastStore.use(s => s.items);
  return (
    <div id="toast">
      {items.map(t => <div key={t.id} className={t.bad ? "bad" : undefined}>{t.msg}</div>)}
    </div>
  );
}
