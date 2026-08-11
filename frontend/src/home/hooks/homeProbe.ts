// 主页健康探测，通知主进程渲染进程状态
export function reportHomeProbe(error?: any) {
  window.desktop?.reportHomeProbe?.({
    ready: !error,
    state: document.body.dataset.app || "",
    cards: document.querySelectorAll(".card").length,
    error: error ? String(error?.message || error) : "",
  });
}
