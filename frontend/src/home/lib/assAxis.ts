// 导入已有产物：解析 ASS/SRT，判定它是空轴、日文轴、中文轴还是双语轴，顺带把说话人读出来。
//
// 四种产物走三条路（见 pipelineStore.startTranscribe）：
//   空轴   —— 只有时间没有文字，要识别。轴随任务上传，服务端整条链切到「填轴」模式。
//   日文轴 —— 原文有了缺译文，不上传视频、不识别，服务端只补 B 阶段翻译。
//   中文轴 —— 译文有了缺原文，什么都不做，直接落库（原文列留空）。
//   双语轴 —— 齐了，同样直接落库。
// 判定错一次的代价很高（把日文轴当空轴 = 白跑一遍识别），所以结果一律回显给用户，
// 且允许他在下拉里手动改判。
//
// 有文字**不等于**有内容：真实的空轴里常有打轴人留下的中文批注（"前压可以再紧紧"、
// "轴过头到下一句声音出来了"）。它与真正的中文轴靠**重复度**分开——批注翻来覆去就那几句
// （实测唯一率 0.45），字幕几乎句句不同（实测 0.85~1.00）。判成空轴时批注会被丢掉。
//
// 语种归列只在**双语轴**上做。单语轴一律整行进它自己那一列：中文字幕里常引用日文原词
// （"放了一大堆グッピーw"），日文字幕里也有整行没假名的（全汉字短句），逐行猜语种必然把
// 它们塞错列——而单语轴根本不需要猜。

export type AxisKind = "empty" | "ja" | "zh" | "bi";

export interface AxisRow {
  t0: number;
  t1: number;
  ja: string;
  zh: string;
  /** 说话人（取自 ASS 的 Style 名，一人一个样式）；"" = 这份轴没标 */
  spk: string;
}

export interface AxisParse {
  rows: AxisRow[];
  kind: AxisKind;
  /** 轴里出现过的说话人，按累计时长降序；长度 < 2 视为「没标说话人」 */
  speakers: string[];
  /** 跨说话人重叠的条数：两个人同时说话，轴上各占一条。不是错误，但会影响识别效果 */
  overlaps: number;
  /** 丢弃的事件数（注释轨、时间码解析不了、零长度） */
  skipped: number;
}

// 假名出现即判日文——汉字两边都有，靠它区分不了；平/片假名是日文独有的。
const KANA = /[぀-ヿ]/;
// 中文特征：有汉字但一个假名都没有
const HAN = /[一-鿿]/;

const isJa = (s: string) => KANA.test(s);
const isZh = (s: string) => !KANA.test(s) && HAN.test(s);

// 这些 Style 是排版用途而不是人：注释轨整条丢掉，其余的只是不当说话人。
const NOTE_STYLES = new Set(["注释", "註釋", "注釋", "comment", "note", "屏注", "字幕注释"]);
const NON_SPEAKER_STYLES = new Set([
  "default", "jp", "cn", "zh", "ja", "sign", "signs", "staff", "title", "op", "ed", "screen",
]);

/** ASS 的 H:MM:SS.cc / SRT 的 HH:MM:SS,mmm → 秒；解析不了返回 NaN */
function parseTime(raw: string): number {
  const m = /^\s*(\d+):([0-5]?\d):([0-5]?\d)[.,](\d{1,3})\s*$/.exec(raw);
  if (!m) return NaN;
  const frac = m[4].padEnd(3, "0");
  return +m[1] * 3600 + +m[2] * 60 + +m[3] + +frac / 1000;
}

/** 去掉 ASS 覆写标签（{\pos(...)}、{\an8} 等），\N/\n 还原成真换行（双语常写在一条事件的两行里）*/
function cleanAssText(raw: string): string {
  return raw
    .replace(/\{[^}]*\}/g, "")
    .replace(/\\[Nn]/g, "\n")
    .replace(/\\h/g, " ")
    .split("\n").map(s => s.replace(/\s+/g, " ").trim()).filter(Boolean)
    .join("\n");
}

interface RawEvent { t0: number; t1: number; text: string; style: string }

function parseAssEvents(text: string): { events: RawEvent[]; skipped: number } {
  const events: RawEvent[] = [];
  let skipped = 0;
  // Format 行决定各字段的下标：不同工具导出的列顺序并不一致，写死下标迟早出错。
  let fields: string[] = ["layer", "start", "end", "style", "name",
    "marginl", "marginr", "marginv", "effect", "text"];
  let inEvents = false;
  for (const line of text.split(/\r?\n/)) {
    const head = line.trim();
    if (head.startsWith("[")) {
      inEvents = head.toLowerCase().startsWith("[events]");
      continue;
    }
    if (!inEvents) continue;
    if (/^format\s*:/i.test(head)) {
      fields = head.slice(head.indexOf(":") + 1).split(",").map(s => s.trim().toLowerCase());
      continue;
    }
    if (!/^dialogue\s*:/i.test(head)) continue;   // Comment: 行是被注释掉的，不算产物
    // 正文里可能有逗号，所以只按字段数 - 1 切，最后一段全归 Text
    const body = head.slice(head.indexOf(":") + 1);
    const parts = body.split(",");
    const cells = parts.length > fields.length
      ? [...parts.slice(0, fields.length - 1), parts.slice(fields.length - 1).join(",")]
      : parts;
    const at = (name: string) => {
      const i = fields.indexOf(name);
      return i >= 0 && i < cells.length ? cells[i].trim() : "";
    };
    const style = at("style");
    // 注释轨是给打轴人自己看的排版说明，不是台词——整条丢掉
    if (NOTE_STYLES.has(style.toLowerCase()) || NOTE_STYLES.has(style)) { skipped++; continue; }
    const t0 = parseTime(at("start"));
    const t1 = parseTime(at("end"));
    if (!Number.isFinite(t0) || !Number.isFinite(t1) || t1 <= t0) { skipped++; continue; }
    events.push({ t0, t1, text: cleanAssText(at("text")), style });
  }
  return { events, skipped };
}

function parseSrtEvents(text: string): { events: RawEvent[]; skipped: number } {
  const events: RawEvent[] = [];
  let skipped = 0;
  // 空轴的块只有序号和时间行，正文为空——按空行切块时这类块同样要留下
  for (const block of text.replace(/^﻿/, "").split(/\r?\n\s*\r?\n/)) {
    const lines = block.split(/\r?\n/).filter(l => l.trim() !== "");
    const at = lines.findIndex(l => l.includes("-->"));
    if (at < 0) { if (lines.length) skipped++; continue; }
    const [a, b] = lines[at].split("-->");
    const t0 = parseTime((a || "").replace(/\./g, ","));
    const t1 = parseTime((b || "").split(/\s+/).filter(Boolean)[0]?.replace(/\./g, ",") ?? "");
    if (!Number.isFinite(t0) || !Number.isFinite(t1) || t1 <= t0) { skipped++; continue; }
    // 正文按行保留：SRT 的双语就是一行译文一行原文（见 vod/core/subtitles.srt_block）
    events.push({
      t0, t1, style: "",
      text: lines.slice(at + 1).map(s => s.replace(/\s+/g, " ").trim()).filter(Boolean).join("\n"),
    });
  }
  return { events, skipped };
}

// 说话人一律读 **Style** 列，不读 Name。这是这套轴的实际约定：人名写在样式上
// （优花 / haru / nana / saya / 芽衣），一人一个样式，颜色也跟着走。Name 列则装什么的都有
// ——实测有把整句日文原文写进去的轴（89 行 89 个不同值），拿它当说话人会在编辑器里炸出
// 几十条空轨。
const MAX_SPEAKERS = 10;

/** Style 集合像不像一组说话人：取值要**少而重复**。一行一个样式的文件装的是排版而不是人，
 *  这时返回 false，交给用户在 UI 上手动选说话人分离。 */
function styleIsSpeaker(events: RawEvent[]): boolean {
  const distinct = new Set(events.map(e => e.style)
    .filter(s => s && !NON_SPEAKER_STYLES.has(s.toLowerCase())));
  return distinct.size >= 2 && distinct.size <= MAX_SPEAKERS && distinct.size * 2 <= events.length;
}

/**
 * 解析一份字幕文件，合成 [{t0,t1,ja,zh,spk}] 并判定类型。
 *
 * 双语的两种写法都要认：一是同一时间区间上并排两条事件（一条日文一条中文，ASS 双轨导出
 * 的常见形态），二是一条事件里两行文本。归组按**时间区间完全相同且同一个说话人**——差一
 * 毫秒或换了个人就不是同一句，宁可当成两条独立的行，也别把两个人同时说的话糊成一条。
 */
export function parseAxisFile(text: string, filename = ""): AxisParse {
  const isAss = /\.(ass|ssa)$/i.test(filename) || /^\s*\[script info\]/im.test(text)
    || /^\s*dialogue\s*:/im.test(text);
  const { events, skipped } = isAss ? parseAssEvents(text) : parseSrtEvents(text);
  const byStyle = styleIsSpeaker(events);
  // Default/JP/CN 这些排版样式不是人：留空当「没标」，免得编辑器里多出一条叫 Default 的轨
  const spkOf = (e: RawEvent) =>
    byStyle && !NON_SPEAKER_STYLES.has(e.style.toLowerCase()) ? e.style : "";
  events.sort((x, y) => x.t0 - y.t0 || x.t1 - y.t1);

  // 先只收「这一行有哪几段文字」，语种归列等判完类型再说（见文件头最后一段）
  interface Draft { t0: number; t1: number; spk: string; parts: string[] }
  const drafts: Draft[] = [];
  for (const ev of events) {
    const spk = spkOf(ev);
    const prev: Draft | null = drafts.length ? drafts[drafts.length - 1] : null;
    const same = !!prev && prev.spk === spk
      && Math.abs(prev.t0 - ev.t0) < 1e-6 && Math.abs(prev.t1 - ev.t1) < 1e-6;
    const draft = same && prev ? prev : { t0: ev.t0, t1: ev.t1, spk, parts: [] };
    if (draft !== prev) drafts.push(draft);
    for (const chunk of ev.text.split("\n")) {
      if (chunk) draft.parts.push(chunk);
    }
  }

  // 说话人按累计时长降序：主说话人排第一，落编辑器的默认轨
  const dur = new Map<string, number>();
  for (const d of drafts) {
    if (d.spk) dur.set(d.spk, (dur.get(d.spk) ?? 0) + (d.t1 - d.t0));
  }
  const speakers = [...dur.entries()].sort((a, b) => b[1] - a[1]).map(([s]) => s);

  let overlaps = 0;
  for (let i = 1; i < drafts.length; i++) {
    if (drafts[i].t0 < drafts[i - 1].t1 - 1e-6 && drafts[i].spk !== drafts[i - 1].spk) overlaps++;
  }

  // 判类型用占比而不是「有没有」：空轴里混进一两句日文备注、日文轴里漏了几行，
  // 都不该改变整份文件的结论。
  const n = drafts.length || 1;
  const jaRatio = drafts.filter(d => d.parts.some(isJa)).length / n;
  const zhRatio = drafts.filter(d => d.parts.some(isZh)).length / n;
  // 中文行的唯一率：区分「中文轴」与「打轴批注」的判据，理由见文件头
  const zhLines = drafts.flatMap(d => d.parts.filter(isZh));
  const zhUnique = zhLines.length ? new Set(zhLines).size / zhLines.length : 0;

  let kind: AxisKind = "empty";
  if (jaRatio >= 0.3 && zhRatio >= 0.3) kind = "bi";
  else if (jaRatio >= 0.3) kind = "ja";
  else if (zhRatio >= 0.6 && zhUnique >= 0.7) kind = "zh";

  const rows: AxisRow[] = drafts.map(d => {
    const row: AxisRow = { t0: d.t0, t1: d.t1, ja: "", zh: "", spk: d.spk };
    if (kind === "empty") return row;              // 空轴：批注清掉，只留时间
    for (const chunk of d.parts) {
      // 单语轴整行进自己那一列，不猜语种；双语轴才按语种分，认不出的（纯英文/数字/符号）
      // 塞进还空着的那一列、日文优先——两边都可能出现它们，塞空位最不会丢内容。
      const key: "ja" | "zh" = kind === "ja" ? "ja" : kind === "zh" ? "zh"
        : isZh(chunk) ? "zh" : (isJa(chunk) || !row.ja) ? "ja" : "zh";
      row[key] = row[key] ? `${row[key]} ${chunk}` : chunk;
    }
    return row;
  });

  return { rows, kind, speakers, overlaps, skipped };
}

export const AXIS_KIND_LABEL: Record<AxisKind, string> = {
  empty: "空轴（只有时间）",
  ja: "日文轴（有原文、缺译文）",
  zh: "中文轴（只有译文）",
  bi: "双语轴（原文译文都有）",
};

export const AXIS_KIND_HINT: Record<AxisKind, string> = {
  empty: "按这条轴识别并翻译，最终字幕的每一行都严格落在你打好的时间上",
  ja: "不上传视频、不做识别，只补中文译文",
  zh: "不做任何处理，直接导入编辑器，原文列留空",
  bi: "不做任何处理，直接导入编辑器",
};
