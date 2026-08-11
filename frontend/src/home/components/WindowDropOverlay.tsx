import { useEffect, useRef, useState } from 'react';
import { toast } from '../lib/notify';
import { importPaths } from '../store/libraryStore';
import { isReady } from '../store/sessionStore';

const VIDEO_EXTS = ["mp4", "m4v", "mov", "mkv", "webm"]; // 与主进程保持一致
const isFileDrag = (e: DragEvent) => [...(e.dataTransfer?.types || [])].includes("Files");

// 整窗拖放
export function WindowDropOverlay() {
  const [dropActive, setDropActive] = useState(false);
  const dropRef = useRef<HTMLDivElement>(null);
  const depth = useRef(0);

  useEffect(() => {
    const clear = () => {
      depth.current = 0;
      document.body.classList.remove("file-drop-target-active");
      dropRef.current?.classList.remove("file-drop-target-active");
      setDropActive(false);
    };
    const onDragEnter = (e: DragEvent) => {
      if (!isFileDrag(e)) return;               // 不 preventDefault，交还给默认行为
      e.preventDefault();
      if (++depth.current === 1 && isReady()) setDropActive(true);
    };
    const onDragOver = (e: DragEvent) => { if (isFileDrag(e)) e.preventDefault(); };
    const onDragLeave = (e: DragEvent) => {
      if (!isFileDrag(e)) return;
      e.preventDefault();
      if (e.relatedTarget === null || --depth.current <= 0) clear();
    };
    const onDrop = (e: DragEvent) => {
      clear();
      if (!isFileDrag(e)) return;                // 拖文字进来：什么都不做
      e.preventDefault();                        // 登录页也要拦，否则窗口会导航到该文件
    };
    window.addEventListener("dragenter", onDragEnter);
    window.addEventListener("dragover", onDragOver);
    window.addEventListener("dragleave", onDragLeave);
    window.addEventListener("drop", onDrop);
    return () => {
      window.removeEventListener("dragenter", onDragEnter);
      window.removeEventListener("dragover", onDragOver);
      window.removeEventListener("dragleave", onDragLeave);
      window.removeEventListener("drop", onDrop);
    };
  }, []);

  useEffect(() => {
    // 实测过的载荷不总是严格贴合 onFilesDropped 声明的 string[]，兼容一下 {filenames} 包装
    const off = window.desktop.onFilesDropped((payload: any) => {
      if (!isReady()) return;
      const paths: string[] = Array.isArray(payload) ? payload : payload?.filenames;
      if (!Array.isArray(paths) || !paths.length) return;
      // 扩展名先过一道，非视频不进导入流程（主进程 importVideos 里还有权威校验）
      const ok = paths.filter(p => VIDEO_EXTS.includes(p.split(".").pop()!.toLowerCase()));
      const bad = paths.length - ok.length;
      if (bad) toast(`忽略了 ${bad} 个非视频文件（支持 ${VIDEO_EXTS.join(" / ")}）`, true);
      if (ok.length) importPaths(ok);
    });
    return () => off?.();
  }, []);

  return (
    <div id="drop" ref={dropRef} data-file-drop-target className={dropActive ? "on" : undefined}>
      <div className="drop-card">
        <div className="drop-face">⸜(｡˃ ᵕ ˂ )⸝♡</div>
        松手即可导入
      </div>
    </div>
  );
}
