# 更新日志

面向开发的完整变更记录，作为 GitHub Release 正文。版本号标题使用 `## x.y.z`，发布时自动摘取对应小节。
给用户看的应用内更新提示写在 `docs/RELEASE_NOTES.md`。

## 0.6.3

- 修复主页无法调整窗口大小的 bug。
- 优化编辑器渲染性能。
- 完成桌面端子工程拆分。
- 新增 GitHub Actions 自动发布：版本号变更后自动编译 Windows 与 macOS 并创建 Release。
- 项目更名为 Nonoka Subtitle，Go 模块路径调整为 `online.nonoka.subtitle/desktop`。
- 补充 `.gitattributes`，统一换行符为 LF。