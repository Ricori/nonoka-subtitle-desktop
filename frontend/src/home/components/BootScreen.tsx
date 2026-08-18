import { bootStore, gateOpen } from '../store/bootStore';

// 启动加载态
export function BootScreen() {
  const lead = bootStore.use(s => s.bootLead);
  const gated = bootStore.use(gateOpen);
  if (gated) return null;
  return (
    <div id="boot">
      <div className="brand">NONOKA<span>字幕</span> · 桌面版</div>
      <div className="spin"></div>
      <div id="boot-lead" className="lead">{lead}</div>
    </div>
  );
}
