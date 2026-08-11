import { useCallback, useEffect, useState } from 'react';
import { THEME_COLORS } from '../utils';

type Theme = "dark" | "light";

// 亮/暗主题：state 驱动 body 的 .light class 与标题栏叠加色；
// 编辑器窗口同源，那边切了主题会通过 storage 事件传过来，这边跟着换
export function useTheme() {
  const [theme, setTheme] = useState<Theme>(() => (localStorage.getItem("theme") as Theme) || "dark");

  useEffect(() => {
    document.body.classList.toggle("light", theme === "light");
    window.desktop?.setTitleBarOverlay?.(THEME_COLORS[theme]);
  }, [theme]);

  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === "theme" && (e.newValue === "dark" || e.newValue === "light")) setTheme(e.newValue);
    };
    addEventListener("storage", onStorage);
    return () => removeEventListener("storage", onStorage);
  }, []);

  const toggleTheme = useCallback(() => {
    setTheme(t => {
      const next: Theme = t === "light" ? "dark" : "light";
      localStorage.setItem("theme", next);
      return next;
    });
  }, []);

  return { theme, toggleTheme };
}
