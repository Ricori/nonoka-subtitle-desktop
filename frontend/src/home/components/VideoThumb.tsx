import { useEffect, useState, type CSSProperties } from 'react';
import { phFace, phHue } from '../utils';
import { sessionStore } from '../store/sessionStore';

// 封面三级回落：本地缩略图 → 云端封面（R2 thumbs/，桌面端上传时顺手传的单帧截图，只试一次）
// → 渐变占位。云端命中后落一份到本地缓存，下次刷新直接命中第一级。
// 抽帧完成后主进程会推 thumb:ready，这里按 id 过滤，只有自己这张才刷新。
export function VideoThumb({ id }: { id: string }) {
  const streamBase = sessionStore.use(s => s.cfg?.stream);
  const [phase, setPhase] = useState<"local" | "cloud" | "placeholder">("local");
  const [src, setSrc] = useState<string | null>(null);
  const [tick, setTick] = useState(0);

  // 云端封面的地址；id 不合法或没配 stream 就是 null，表示这一级直接跳过
  const cloudURL = streamBase && /^[0-9A-Za-z_-]{1,64}$/.test(id) ? `${streamBase}/thumbs/${id}.jpg` : null;

  useEffect(() => {
    let cancelled = false;
    // src 必须一并清掉：换 id 时留着上一张会先闪一下别的视频的封面
    setPhase("local");
    setSrc(null);
    // 主进程每次都返回带新序号的地址，天然绕过缓存，抽帧完成后不用再自己加时间戳
    window.desktop.thumbnailURL(id).then(url => {
      if (!cancelled) setSrc(url);
    }).catch(() => {
      if (cancelled) return;
      if (cloudURL) { setPhase("cloud"); setSrc(cloudURL); }
      else setPhase("placeholder");
    });
    return () => { cancelled = true; };
  }, [id, tick, cloudURL]);

  useEffect(() => {
    const off = window.desktop.onThumbReady(readyId => {
      if (readyId === id) setTick(t => t + 1);
    });
    return () => off?.();
  }, [id]);

  const onError = () => {
    if (phase === "local" && cloudURL) { setPhase("cloud"); setSrc(cloudURL); }
    else setPhase("placeholder");
  };
  const onLoad = () => {
    if (phase === "cloud" && src) window.desktop.cacheThumb(id, src).catch(() => { });
  };

  if (phase === "placeholder") {
    return (
      <div className="ph" style={{ "--ph-h": phHue(id) } as CSSProperties}>
        <img className="ph-face" alt="" src={`./assets/faces/f${phFace(id)}.png`} />
      </div>
    );
  }
  return <img src={src ?? undefined} onError={onError} onLoad={onLoad} alt="" />;
}
