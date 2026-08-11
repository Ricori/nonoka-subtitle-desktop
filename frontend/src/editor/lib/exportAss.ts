import { apiUrl, authHeaders, getVid } from '../session';
import { docStore } from '../store/docStore';
import { flushSave } from '../store/saveStore';
import { toast } from '../store/uiStore';
import { viewStore } from '../store/viewStore';
import { errText } from '../utils';
import { buildClipAss } from './assBuild';

/**
 * 完整片走服务端那份（与网页版逐字一致，是 buildAss 的对照基准）；
 * 切片没有对应的服务端接口，本地按区间拼——两条路的样式/堆叠规则同源。
 */
export async function exportAss() {
  try {
    await flushSave();
    const { title } = docStore.get();
    const v = viewStore.get();
    let content: string;
    let name = (title || getVid()).replace(/\.[a-z0-9]{2,4}$/i, "") || getVid();
    if (v.curClip) {
      content = buildClipAss(v.t0, v.t1);
      name += " - " + v.curClip.name;
    } else {
      const r = await fetch(apiUrl(`/edit/${getVid()}/export.ass`), { headers: authHeaders() });
      if (!r.ok) {
        const d = await r.json().catch(() => ({} as any));
        throw new Error(d.detail || "HTTP " + r.status);
      }
      content = await r.text();
    }
    const path = await window.desktop.saveSubtitle(name + ".ass", content);
    if (!path) return;
    toast("已导出 " + path + " · 点此打开所在文件夹",
      true, () => window.desktop.revealInFolder(path), 5000, true);
  } catch (e) {
    toast("导出失败：" + errText(e));
  }
}
