// 纯工具函数和常量，无 DOM/状态依赖，可在任何地方 import
import type { LibraryEntry, MergedVideoItem, PipeState, RemoteVideo } from './types';

// 进度与状态映射
export const PHASE_TEXT: Record<string, string> = {
  queued: "排队中", downloading: "读取视频", processing: "预处理",
  transcribing: "识别中", translating: "翻译中",
};
/** 云端「进行中」的状态集合，与 PHASE_TEXT 的键一一对应 */
export const RUNNING = new Set(Object.keys(PHASE_TEXT));

export function taskProgress(v: RemoteVideo | null | undefined): number {
  if (!v) return 0;
  switch (v.status) {
    case "queued": return 3;
    case "downloading": return 8;
    case "processing": return 14;
    case "transcribing": {
      const total = v.total || 0;
      if (!total) return 20;
      const dec = v.decoded ?? total;
      return 15 + (dec / total) * 30 + (v.done || 0) / total * 40;
    }
    case "translating": return 90;
    case "done": return 100;
    default: return 0;
  }
}

// 用已耗时按当前进度线性外推。前 6% 和刚起步的 30 秒噪声太大，不给数
export function etaText(v: RemoteVideo | null | undefined): string {
  // 排队时还没开工，本来就估不出来；说「估算中」会让人以为界面坏了，如实说在排队
  if (v?.status === "queued") return "排队等待中…";
  const p = taskProgress(v) / 100;
  if (!v || !v.created_at || p >= 1) return "预计时长估算中…";
  const elapsed = Math.max(0, Date.now() / 1000 - v.created_at);
  // 外推噪声太大时不给数，但要把已耗时显出来：短片（服务端只切 1 块）整个任务
  // 都在闸门内，否则从头到尾就是一句不动的「估算中」，看着像界面卡死
  if (p <= 0.06 || elapsed < 12) return `已用时 ${fmtDur(elapsed)}`;
  return `约还需 ${Math.max(1, Math.round(elapsed * (1 - p) / p / 60))} 分钟`;
}

export const fmtSize = (n: number) => !n ? "—" :
  n >= 1024 ** 3 ? (n / 1024 ** 3).toFixed(2) + " GB" : (n / 1024 ** 2).toFixed(0) + " MB";

export function fmtDur(s: number): string {
  if (!s || !isFinite(s)) return "—";
  s = Math.round(s);
  const h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), x = s % 60;
  const p = (n: number) => String(n).padStart(2, "0");
  return h ? `${h}:${p(m)}:${p(x)}` : `${m}:${p(x)}`;
}

export const fmtDate = (ms: number) => !ms ? "" : new Date(ms).toLocaleDateString("zh-CN");

// 占位封面：缩略图缺失时按 id 确定性挑一张差分和一个渐变色相，同一视频永远同一套。
// 取模只看低位，线性哈希对短 id / 连号 id 极易撞脸，所以末尾用 mix 再混淆一轮；
// 异或结果是有符号 32 位，必须 >>>0 拉回无符号，否则负数取模得负下标。
const PH_FACE_N = 6;
function phHash(id: string, mix: number): number {
  let h = 0;
  for (const c of String(id)) h = (Math.imul(h, 131) + c.charCodeAt(0)) >>> 0;
  h = Math.imul(h ^ (h >>> 13), mix) >>> 0;
  return (h ^ (h >>> 15)) >>> 0;
}

export const phFace = (id: string) => phHash(id, 0x5bd1e995) % PH_FACE_N;
/** 换个 mix 参数，让色相和表情不绑定 */
export const phHue = (id: string) => phHash(id, 0x2545f491) % 360;

// ── 术语表 CSV 解析 / 序列化 ──────────────────────────────────────────────────
// 格式与服务端一致。带引号的字段要按 CSV 规矩解
export const GM_COLS = ["类别", "原词", "略称", "中文", "中文略称", "备注"];
// 六列宽度：原词/中文是主角给最宽，类别要放得下「专有名词」「ASR纠错」，备注吃剩下的
export const GM_WIDTHS = ["14%", "20%", "11%", "20%", "11%", "auto"];

// 分隔符按内容判：整份没有半角逗号却有制表符时按 TSV 收
export function parseCSV(text: string): string[][] {
  const t = String(text ?? "").replace(/\r\n?/g, "\n");
  const delim = !t.includes(",") && t.includes("\t") ? "\t" : ",";
  const rows: string[][] = [];
  let row: string[] = [], cell = "", quoted = false, started = false;
  for (let i = 0; i < t.length; i++) {
    const c = t[i];
    if (quoted) {
      if (c !== '"') { cell += c; continue; }
      if (t[i + 1] === '"') { cell += '"'; i++; } else quoted = false;   // "" = 一个引号
      continue;
    }
    if (c === '"' && !started) { quoted = true; started = true; continue; }   // 引号只在格首开
    if (c === delim) { row.push(cell); cell = ""; started = false; continue; }
    if (c === "\n") { row.push(cell); rows.push(row); row = []; cell = ""; started = false; continue; }
    cell += c;
    started = true;
  }
  if (cell || row.length) { row.push(cell); rows.push(row); }
  return rows;
}

// CSV 文本 → 六列的行数组：吃掉表头、缺列补空、丢掉纯空行
export function csvToRows(text: string): string[][] {
  const rows = parseCSV(text);
  if (rows.length && (rows[0][0] || "").trim() === "类别") rows.shift();
  return rows
    .map(r => GM_COLS.map((_, i) => (r[i] || "").replace(/\n/g, " ").trim()))
    .filter(r => r.some(Boolean));
}

export const csvCell = (s: string) => /[",\n]/.test(s) ? '"' + s.replace(/"/g, '""') + '"' : s;
export const rowsToCsv = (rows: string[][]) =>
  [GM_COLS.join(","), ...rows.map(r => r.map(csvCell).join(","))].join("\n") + "\n";

// ── 流水线阶段文案 ────────────────────────────────────────────────────────────
export const PIPE_TEXT: Record<string, string> = { audio: "抽音频", upload: "上传", start: "启动任务" };

// ── 合并数据源 ─────────────────────────────────────────────────────────────────
// 首页列表 = 服务端记录（唯一真源）∪ 本地尚未转写的导入条目。
// 匹配先按 videoId，再按 fp 兜住「网页版曾传过同一文件」的情况并自动关联。
export function mergeLibrary(lib: LibraryEntry[], remote: RemoteVideo[]): MergedVideoItem[] {
  const byId = new Map(lib.map(e => [e.id, e]));
  // 同 fp 撞车时留本机条目：云端占位条目没有源文件/缓存，拿它当匹配结果卡片会变成空壳，
  // 真正的本机条目又因为没被 used 标记而另出一张，同一个视频分裂成两张卡
  const byFp = new Map<string, LibraryEntry>();
  for (const e of lib) {
    const prev = e.fp ? byFp.get(e.fp) : undefined;
    if (e.fp && (!prev || (prev.cloudOnly && !e.cloudOnly))) byFp.set(e.fp, e);
  }
  const used = new Set<string>();
  const out: MergedVideoItem[] = [];

  for (const v of remote) {
    // byId 命中的可能正是那个空壳占位条目，这时改用 fp 找回本机条目
    const hit = byId.get(v.video_id);
    const local = hit && !hit.cloudOnly ? hit : (v.fp ? byFp.get(v.fp) : undefined) || hit;
    if (local) used.add(local.id);
    out.push({
      id: v.video_id,
      localId: local && !local.cloudOnly ? local.id : null,
      title: local?.title || v.title || v.video_id,
      size: local?.size || 0,
      duration: local?.duration || 0,
      width: local?.width || 0,
      height: local?.height || 0,
      addedAt: local?.addedAt || (v.created_at ? v.created_at * 1000 : 0),
      srcPath: local?.srcPath || null,
      media: v.media || "video",
      shared: !!v.shared,
      remote: v,
      status: v.status,
      count: v.count || 0,
      error: v.error || "",
    });
  }
  for (const e of lib) {
    if (used.has(e.id)) continue;
    // 只滤掉云端同步下来的占位条目：它们没有本机文件，云端记录没了就该跟着消失。
    // 不能按 id 前缀判断——loc_ 前缀在提交转写时就被换成云端 video_id 了
    if (e.cloudOnly) continue;
    out.push({
      id: e.id, localId: e.id, title: e.title, size: e.size, duration: e.duration,
      width: e.width, height: e.height, addedAt: e.addedAt, srcPath: e.srcPath,
      media: "video", shared: false, remote: null, status: "local", count: 0, error: "",
    });
  }
  out.sort((a, b) => (b.addedAt || 0) - (a.addedAt || 0));
  return out;
}

// ── 卡片状态派生（纯函数，供 VideoCard 渲染用）──────────────────────────────────
// 缓存 / 缩略图 / 源文件一律按本地条目 id 存：换 key 重转后条目 id 会跟云端 video_id 脱钩，
// 拿 it.id 去查就会查空
export const localKey = (it: MergedVideoItem) => it.localId || it.id;

export const onPipe = (pipe: PipeState | null | undefined, it: MergedVideoItem) =>
  !!pipe && pipe.cardId === it.id;

export function pipeLabel(pipe: PipeState): string {
  if (pipe.msg) return pipe.msg;
  const pct = pipe.total ? Math.round(pipe.done / pipe.total * 100) : 0;
  return `${PIPE_TEXT[pipe.stage] || pipe.stage} ${pct}%`;
}

// 抽音频给前 25%、上传给到 95%：上传是耗时大头，别让进度条卡在中间不动
export function pipePct(pipe: PipeState): number {
  const r = pipe.total ? pipe.done / pipe.total : 0;
  if (pipe.stage === "audio") return r * 25;
  if (pipe.stage === "upload") return 25 + r * 70;
  return 97;
}

// 以 /edit/state 的 has_r2 为准；老记录没这个字段时只认「转写中」，其余当作不在——
// 宁可少提示，也别让人点了取回才发现 404
export function inR2(it: MergedVideoItem): boolean {
  if (!it.remote || it.media === "audio") return false;
  return it.remote.has_r2 ?? RUNNING.has(it.status);
}

// 提交转写的前置条件：本机没在跑流水线、云端没有进行中的任务、并发锁没被点播平台占着
export function blockReason(
  it: MergedVideoItem,
  { pipe, busyOther, cloudBusy }: { pipe: PipeState | null; busyOther: boolean; cloudBusy: boolean },
): string {
  if (it.shared) return "这是共享给你的视频，转写由所有者发起";
  if (pipe) return "已有任务进行中";
  if (busyOther) return "点播平台有转录任务进行中，暂时无法提交";
  if (cloudBusy) return "云端已有进行中任务";
  if (it.status !== "error" && !it.localId) return "这条记录只在云端，桌面端无法重新运行";
  return "";
}

// ── 主题色 ─────────────────────────────────────────────────────────────────────
export const THEME_COLORS = {
  dark: { color: "#171b2a", symbolColor: "#eef1f9" },
  light: { color: "#faf6ec", symbolColor: "#333a52" },
} as const;

// ── API 工具 ──────────────────────────────────────────────────────────────────
// 主进程抛回来的错误可能带一层调用包装前缀，剥掉只留 Go 端写的那句话
export const errText = (e: any) => String(e?.message || e || "")
  .replace(/^Error invoking remote method '[^']*':\s*/, "")
  .replace(/^Error:\s*/, "");


// ── DOM 工具 ──────────────────────────────────────────────────────────────────
/** 把浮动菜单定位在锚点按钮下方，靠右对齐，超出视口时自动翻转到上方 */
export function placeMenu(menu: HTMLElement, anchor: HTMLElement) {
  const gap = 6;
  const edge = 8;
  const r = anchor.getBoundingClientRect();
  menu.style.left = "0px";
  menu.style.right = "auto";
  menu.style.top = "0px";
  menu.classList.add("on");
  const box = menu.getBoundingClientRect();
  const left = Math.min(innerWidth - box.width - edge, Math.max(edge, r.right - box.width));
  const below = r.bottom + gap;
  const top = below + box.height <= innerHeight - edge
    ? below
    : Math.max(edge, r.top - gap - box.height);
  menu.style.left = `${Math.round(left)}px`;
  menu.style.top = `${Math.round(top)}px`;
}
