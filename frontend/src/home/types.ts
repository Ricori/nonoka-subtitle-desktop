// 主页状态层用到的类型。桌面端原生数据结构（AppConfig/LibraryEntry 等）复用 Wails
// 自动生成的绑定类型，不重复定义；服务端（Python /edit/state）返回的形状不在绑定里，
// 在这里补上。
export type { AppConfig, LibraryEntry } from '../../bindings/online.nonoka.subtitle/desktop/internal/app';

// /edit/state 返回的单条云端记录
export interface RemoteVideo {
  video_id: string;
  title?: string;
  fp?: string;
  created_at?: number;
  media?: "video" | "audio";
  shared?: boolean;
  status: string;
  count?: number;
  error?: string;
  decoded?: number;
  done?: number;
  total?: number;
  has_r2?: boolean;
}

// mergeLibrary() 产出的一张卡片对应的数据：服务端记录 ∪ 本地库条目 合并后的结果
export interface MergedVideoItem {
  id: string;
  localId: string | null;
  title: string;
  size: number;
  duration: number;
  width: number;
  height: number;
  addedAt: number;
  srcPath: string | null;
  media: string;
  shared: boolean;
  remote: RemoteVideo | null;
  status: string;
  count: number;
  error: string;
}

// 本机转写流水线（抽音频 → 上传 → 起任务）
export type PipeStage = "audio" | "upload" | "start";

export interface PipeState {
  cardId: string;
  localId: string;
  vid: string | null;
  stage: PipeStage;
  done: number;
  total: number;
  msg: string;
  tmp: string | null;
  canceling?: boolean;
}

/** 主页加载阶段：boot（校验中）/ login（待输入 key）/ ready（已登录）*/
export type AppPhase = "boot" | "login" | "ready";

export interface KeybarState {
  msg: string;
  fix: boolean;
}

// 专业术语表
export interface GlossItem {
  name: string;
  csv: string;
  can_edit: boolean;
  mine: boolean;
  rows: number;
  owner?: string;
}
export type GlossSets = Record<string, string>;

export interface SpeakerResult {
  speakers: number; // 0=关；-1=开但人数交给模型估；>=2=开且已知人数
  glossary: string; // 术语表名，""=不使用
  correct: boolean;   // ⑧ A 阶段：合并被切碎的句子 + ASR 纠错
  translate: boolean; // ⑨ B 阶段：翻译。关掉则产物只有原文，译文全空
  // 已有产物（可选）。null=没有产物，走完整识别链。
  // kind="empty" 时 rows 只有时间，随任务上传当人工轴；"ja"/"bi" 不上传视频，走 /edit/video/import
  axis: AxisImport | null;
}

export interface AxisImport {
  kind: import('./lib/assAxis').AxisKind;
  rows: import('./lib/assAxis').AxisRow[];
  filename: string;
  /** 轴里读到的说话人（≥2 位时服务端不跑 diarization，直接按轴分轨） */
  speakers: string[];
}

// ask() 页内确认框
export interface AskDialogState {
  text: string;
  okText: string;
  isInput: boolean;
  value: string;
}

export interface ToastItem {
  id: number;
  msg: string;
  bad: boolean;
}
