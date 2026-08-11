import React, { useEffect, useRef, useState } from 'react';
import { PopoverMenu } from './PopoverMenu';
import { VideoThumb } from './VideoThumb';
import { toast } from '../lib/notify';
import {
  deleteCloudSubtitle, libraryStore, openEditor, removeVideo, renameVideo, revealVideo,
} from '../store/libraryStore';
import { cancelPipe, pipelineStore, startTranscribe, stopTask } from '../store/pipelineStore';
import { closePopover, togglePopover } from '../store/uiStore';
import {
  PHASE_TEXT, RUNNING, taskProgress, etaText, fmtSize, fmtDur, fmtDate,
  onPipe, pipeLabel, pipePct, inR2, blockReason, localKey,
} from '../utils';
import type { MergedVideoItem, PipeState } from '../types';

function StateBadge({ it, pipe }: { it: MergedVideoItem; pipe: PipeState | null }) {
  if (onPipe(pipe, it)) return <span className="badge state st-run">{pipeLabel(pipe!)}</span>;
  if (it.status === "done") return <span className="badge state st-done">已完成 · {it.count}句</span>;
  if (it.status === "error") return <span className="badge state st-err" title={it.error}>失败</span>;
  if (RUNNING.has(it.status)) {
    const p = Math.round(taskProgress(it.remote));
    return <span className="badge state st-run">{PHASE_TEXT[it.status]} {p}%</span>;
  }
  return <span className="badge state st-none">未开始</span>;
}

function AvailChip({ it, cached, hasSrc }: { it: MergedVideoItem; cached: boolean; hasSrc: boolean }) {
  if (cached) return <span className="chip c-ok">✓ 已缓存</span>;
  if (hasSrc) return <span className="chip c-ok">✓ 本机视频</span>;
  if (inR2(it)) return <span className="chip c-cloud">☁ 云端文件</span>;
  return <span className="chip c-warn">⚠ 视频缺失</span>;
}

interface PrimaryActionProps {
  it: MergedVideoItem;
  pipe: PipeState | null;
  busyOther: boolean;
  cloudBusy: boolean;
}

function PrimaryAction({ it, pipe, busyOther, cloudBusy }: PrimaryActionProps) {
  if (onPipe(pipe, it)) return <button className="btn" data-act="cancel" onClick={() => cancelPipe()}>取消</button>;
  if (it.status === "done") return <button className="btn primary" onClick={() => openEditor(it)}>编辑字幕</button>;
  if (RUNNING.has(it.status)) {
    return it.shared
      ? <button className="btn" disabled title="这是共享给你的视频，只有所有者能停止">停止</button>
      : <button className="btn" onClick={() => stopTask(it)}>停止</button>;
  }
  const why = blockReason(it, { pipe, busyOther, cloudBusy });
  const label = it.status === "error" ? "重试" : "开始任务";
  return (
    <button className={it.status === "error" ? "btn" : "btn primary"} disabled={!!why} title={why || undefined}
      onClick={() => startTranscribe(it)}>{label}</button>
  );
}

type MenuAct = "rename" | "reveal" | "remove" | "deleteCloud";
interface MenuEntry { act: MenuAct; ico: string; label: string; danger?: boolean; }

// 首页视频卡片：封面 + 状态角标/进度条 + 信息行 + 主操作按钮 + 「⋯」菜单
function VideoCardImpl({ it }: { it: MergedVideoItem }) {
  // 只订阅归属自己的 pipe：抽音频/上传进度每 200ms 变一次，订阅整个 pipe 的话
  // 每一帧都会把全墙的卡片一起唤醒
  const pipe = pipelineStore.use(s => s.pipe && s.pipe.cardId === it.id ? s.pipe : null);
  const busyOther = libraryStore.use(s => s.busyOther);
  const cloudBusy = libraryStore.use(s => s.cloudBusy);
  const cached = libraryStore.use(s => s.cachedSet.has(localKey(it)));
  const hasSrc = libraryStore.use(s => s.srcSet.has(localKey(it)));

  const moreRef = useRef<HTMLButtonElement>(null);
  const key = localKey(it);
  const thumbId = localKey(it);

  const cloud = RUNNING.has(it.status);
  const running = cloud || onPipe(pipe, it);

  // etaText 吃 Date.now()，但卡片只在轮询带来字段变化时才重渲染（20s 一次），
  // 「已耗时」不会自己走。跑起来的那张卡自己每秒走一拍（同时最多一个云端任务）
  const [, tick] = useState(0);
  useEffect(() => {
    if (!cloud) return;
    const t = setInterval(() => tick(n => n + 1), 1000);
    return () => clearInterval(t);
  }, [cloud]);
  const res = it.height ? `${it.height}p` : "";
  // 云端转写中时把 ETA 顶到信息行；纯云端记录（没匹配到本地库条目）拿不到 size/duration/分辨率
  const metaBits = cloud ? etaText(it.remote)
    : !it.localId ? fmtDate(it.addedAt)
      : [fmtSize(it.size), res, fmtDate(it.addedAt)].filter(Boolean).join(" · ");
  const pct = onPipe(pipe, it) ? pipePct(pipe!) : taskProgress(it.remote);

  const menuItems: MenuEntry[] = [];
  if (it.localId || (it.remote && !it.shared)) menuItems.push({ act: "rename", ico: "✎", label: "重命名" });
  if (it.srcPath) menuItems.push({ act: "reveal", ico: "📂", label: "打开文件夹" });
  if (it.localId) menuItems.push({ act: "remove", ico: "⤴", label: "从本地库移出" });
  if (it.remote && !it.shared && !onPipe(pipe, it) && !RUNNING.has(it.status)) {
    menuItems.push({ act: "deleteCloud", ico: "🗑", label: "删除云端字幕", danger: true });
  }
  const popoverId = `card:${it.id}`;

  const handleMenuAction = (act: MenuAct) => {
    if (act === "rename") renameVideo(it);
    else if (act === "reveal") revealVideo(it);
    else if (act === "remove") removeVideo(it);
    else if (act === "deleteCloud") deleteCloudSubtitle(it);
  };

  return (
    <div className="card" data-key={key} data-id={it.id}>
      <div className="thumb" onClick={() => { if (it.status === "done") openEditor(it); }}>
        <VideoThumb id={thumbId} />
        <StateBadge it={it} pipe={pipe} />
        {it.duration ? <span className="badge dur">{fmtDur(it.duration)}</span> : null}
        {running && <div className="bar" style={{ width: `${pct}%` }} />}
      </div>
      <div className="info">
        <div className="name" title={it.title}>{it.title}</div>
        <div className="meta">
          <span className="meta-text">{metaBits}</span>
          {it.shared && (
            <span className="chip c-share" title="所有者把它共享给了你：可以编辑字幕，但不能删除或重跑转写">⇄ 共享</span>
          )}
          <AvailChip it={it} cached={cached} hasSrc={hasSrc} />
        </div>
      </div>
      <div className="actions">
        <PrimaryAction it={it} pipe={pipe} busyOther={busyOther} cloudBusy={cloudBusy} />
        <button className="btn icon" ref={moreRef} title="更多"
          onClick={e => {
            e.stopPropagation();
            if (!menuItems.length) { toast("没有可用操作"); return; }
            togglePopover(popoverId);
          }}>⋯</button>
        <PopoverMenu id={popoverId} anchorRef={moreRef}>
          {menuItems.map(m => (
            <button key={m.act} role="menuitem" className={m.danger ? "danger" : undefined}
              onClick={() => { closePopover(); handleMenuAction(m.act); }}>
              <span className="ico">{m.ico}</span>{m.label}
            </button>
          ))}
        </PopoverMenu>
      </div>
    </div>
  );
}

// mergeLibrary() 每轮都重建所有 item 对象，引用总是新的，memo 的默认浅比较救不了——
// 只比真正影响渲染的字段。其余输入（pipe / 缓存集合等）由卡片自己订阅，不参与这里。
function sameItem(a: MergedVideoItem, b: MergedVideoItem) {
  return a.id === b.id && a.localId === b.localId && a.title === b.title &&
    a.size === b.size && a.duration === b.duration && a.height === b.height &&
    a.width === b.width && a.addedAt === b.addedAt && a.srcPath === b.srcPath &&
    a.media === b.media && a.shared === b.shared && a.status === b.status &&
    a.count === b.count && a.error === b.error &&
    a.remote?.decoded === b.remote?.decoded && a.remote?.done === b.remote?.done &&
    a.remote?.total === b.remote?.total && a.remote?.created_at === b.remote?.created_at &&
    a.remote?.has_r2 === b.remote?.has_r2;
}

export const VideoCard = React.memo(VideoCardImpl, (prev, next) =>
  prev.it === next.it || sameItem(prev.it, next.it));
