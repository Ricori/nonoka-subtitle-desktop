import { useEffect, useRef } from 'react';
import { shallowEqual } from '../../home/lib/createStore';
import { bindScrub } from '../lib/laneDrag';
import { effGain, waveAmp } from '../lib/wave';
import { docStore } from '../store/docStore';
import { layoutStore } from '../store/layoutStore';
import { tOf, viewDur, viewStore } from '../store/viewStore';

/**
 * 波形：只画可视窗口。60 分钟 × 140px/s 的整条时间轴远超 canvas 尺寸上限，
 * 波形 canvas 只做视口宽，随滚动平移重画。
 */
export function WaveRow({ height, left, w }: { height: number; left: number; w: number }) {
  const cvRef = useRef<HTMLCanvasElement>(null);
  const { pps } = viewStore.use(s => ({ pps: s.pps, t0: s.t0, t1: s.t1 }), shallowEqual);
  const gain = layoutStore.use(s => s.waveGain);
  const peaks = docStore.use(s => s.peaks);

  useEffect(() => {
    const cv = cvRef.current;
    if (!cv) return;
    const g = effGain();
    const W = Math.min(w, Math.round(viewDur() * pps));
    const H = height || 56;
    const x0 = Math.max(0, Math.min(left, viewDur() * pps - W));
    cv.width = W * 2; cv.height = H * 2;
    cv.style.width = W + "px"; cv.style.height = H + "px";
    cv.style.left = x0 + "px";
    const ctx = cv.getContext("2d");
    if (!ctx) return;
    ctx.scale(2, 2);
    ctx.clearRect(0, 0, W, H);
    ctx.fillStyle = "rgba(139,147,165,0.55)";
    const mid = H / 2;
    for (let x = 0; x < W; x++) {
      // 放大后超过满格的直接削平（顶满而不是画出行外）
      const a = Math.min(1, waveAmp(tOf(x0 + x)) * g) * (H / 2 - 3);
      ctx.fillRect(x, mid - Math.max(a, 0.6), 1, Math.max(a * 2, 1.2));
    }
    ctx.fillStyle = "rgba(139,147,165,0.18)";
    ctx.fillRect(0, mid - 0.5, W, 1);
  }, [height, left, w, pps, gain, peaks]);

  return (
    <div className="waverow" id="waverow" style={{ height: height + "px" }}
      onPointerDown={e => bindScrub(e, true)}>{/* 点音频行同时取消句子选定 */}
      <canvas id="wave" ref={cvRef} />
    </div>
  );
}
