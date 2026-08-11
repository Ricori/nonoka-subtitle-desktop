<div align="center">
  <h1>Nonoka Subtitle (Desktop)</h1>
  <a href="https://wails.io/"><img src="https://img.shields.io/badge/Wails-v3-blue.svg" alt="Wails"></a>
  <a href="https://golang.org/"><img src="https://img.shields.io/badge/Go-1.25+-00ADD8.svg" alt="Go"></a>
  <a href="https://reactjs.org/"><img src="https://img.shields.io/badge/React-19-61DAFB.svg" alt="React"></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/License-GPL_3.0-blue.svg" alt="License: GPL v3"></a>
</div>

<br>

本项目是 Nonoka Subtitle 的桌面端，专为产出外语视频的译文字幕设计。项目提供全流程的视频管理、AI 字幕生成、字幕编辑、视频导出等能力，结合了 Aegisub 的专业性与剪映的便捷性，大幅降低字幕的制作门槛。

## ✨ 核心特性

- **极简轻量 (Go 原生驱动)**：不走 Electron 路线，**应用打包体积仅约 20MB**，日常待机**内存占用低至 50MB**。
- **专业级编辑与字幕引擎**：深度集成 **JASSUB 引擎**与自定义字体，支持编辑器轨道**直接绑定 ASS 样式**。字幕渲染与主流播放器对齐，**所见即所得**。
- **全栈媒体处理链路**：自带本地及云端视频管理，支持实时同步、音频提取、带样式的 ASS 导出，以及**视频内嵌字幕与压制**。
- **沉浸式桌面**：沉浸式标题栏设计，无边框拖拽。支持亮/暗色主题切换，当前提供「星夜」与「白丝带」两套精心设计的<del>契合 nonoka 配色</del>的主题。
- **自动化安全**：内置防误删的智能 LRU 缓存管理，以及由纯 Go 驱动、强摘要校验的无感自动更新体系。

## 🚀 快速开始

开发前请确保本地已安装 **Go (1.25+)** 与 **Node.js (18+)**。

### 1. 环境准备

本项目基于 Wails v3 构建，需要全局安装 Wails3 CLI：

```shell
go install github.com/wailsapp/wails/v3/cmd/wails3
```

项目的构建/打包命令都定义在 [Taskfile.yml](./Taskfile.yml) 中，建议安装 Task：

```shell
go install github.com/go-task/task/v3/cmd/task
```

### 2. 启动开发环境

在项目目录下执行，会同时拉起 Go 后端与 Vite 前端热更新服务器：

```shell
task dev
```

> 该命令等价于 `wails3 dev -config ./build/config.yml -port 9245`。直接裸跑 `wails3 dev` 会丢掉配置与端口参数，请优先用 `task dev`。

### 3. 常用命令

| 命令 | 作用 |
| :--- | :--- |
| `task dev` | 启动开发模式（后端 + 前端热更新） |
| `task build` | 构建当前平台的可执行文件到 `bin/` |
| `task build GOOS=windows` | 构建指定平台（`windows` / `macos`） |
| `task package` | 产出可分发的安装包（Windows 下为 NSIS 安装器 / MSIX） |
| `task run` | 运行已构建的产物 |
| `go test ./...` | 运行后端 Go 单元测试 |
| `npm --prefix frontend run typecheck` | 前端严格 TypeScript 类型检查 |

<details>
<summary><b>无 GUI 的服务端模式</b></summary>

项目同时支持不带 WebView 的纯 HTTP 服务模式，便于容器化部署：

```shell
task build:server   # 构建服务端模式二进制
task run:server     # 本地运行服务端模式
task build:docker   # 构建 Docker 镜像
task run:docker     # 构建并运行 Docker 镜像
```

</details>

## 📁 项目结构

```text
desktop-wails/
├── main.go                  # 可执行程序入口，仅做装配
├── Taskfile.yml             # 构建/打包/开发命令定义
├── internal/
│   ├── app/                 # Wails 服务、应用编排与全部业务逻辑
│   └── platformprocess/     # 各操作系统的子进程窗口参数差异
├── frontend/                # React 前端子工程（详见其 README）
├── build/                   # 各平台打包配置、图标、Docker 与 Taskfile
└── scripts/                 # 发布与更新包制作脚本
```

Go 测试跟随被测包放置（如 `internal/app/cache_lru_test.go`）。

## 🏗️ 核心架构与底层实现

本项目采用前后端分离的桌面架构，借助 Wails v3 实现逻辑控制与 UI 渲染解耦。

### 1. UI 层 (React 19)

前端作为纯粹的 Web 视图层工程独立维护，详见 [🌐 前端子项目文档](./frontend/README.md)。

- **隔离机制**：前端不直接操作文件系统或发起系统调用，所有状态落盘、媒体读取及命令行执行均通过 `@wailsio/runtime` 的 IPC 机制请求 Go 后端。
- **高阶媒体渲染**：复杂字幕的解析渲染由 JASSUB WebAssembly 引擎在 Worker 中并发执行，保证主 UI 线程流畅。

### 2. 核心模块 (`internal/app`)

这里统一接管所有重负荷与系统级操作：

- **网关中枢与状态管理 (`desktop_service.go`)**
  - 面向前端的统一 Binding API 入口，负责配置持久化与多线程安全的数据调度（通过 RWMutex 控制并发）。
  - 内置 LRU 缓存策略控制器（`cache_lru.go`），动态计算视频缓存目录的容量上限；并通过维护字典映射与 `context.CancelFunc` 实现全生命周期的任务打断与清理。
- **媒体处理引擎 (`media_engine.go` & `ffmpeg_manager.go`)**
  - **动态 FFmpeg 管理**：应用不自带沉重的二进制核心，而是内置了一套安全的原子化下载机制。首次启动时在后台下载并比对 SHA-256，校验通过后才落盘供调用。
  - **异步化处理**：提供非阻塞的媒体探针 (Probe)、抽帧缩略图生成与音轨抽取，并对 Stdout/Stderr 做管道化封装，向前端抛出精确进度。
- **专用流媒体网关 (`media_server.go`)**
  - **规避性能瓶颈**：Wails v3 Alpha 的原生 AssetServer 在处理超大视频文件或开放式 Range 请求时，会把响应完整写入内存缓冲区，导致严重的加载卡顿与内存激增。为绕过这一框架底层的内存复制瓶颈，系统会在 `127.0.0.1` 的随机端口上单独拉起一个轻量 HTTP 服务。
  - **原生 Range 传输**：完整支持 HTTP Range/206 分块传输，前端 WebView2 可精准流式读取视频切片。配合 24 位随机 Token 建立的安全沙盒路由，加载与拖动延迟降至毫秒级。
- **独立更新流 (`auto_update.go`)**
  - 基于 Wails 的 `updater` + endpoint 清单实现（摘要算法 SHA-512）。异步下载完毕后通过跨平台的无头机制触发外置替换进程。
  - 默认策略是「本次会话只下载、下次启动再安装」，不打断当前使用；清单里带 `minVersion` 且高于当前版本时才升级为强制更新。

### 3. 系统适配 (`internal/platformprocess`)

处理各平台的底层 API 差异，提供原生级体验。

- **静默后台守护**：利用 `syscall.SysProcAttr`，针对 Windows 封装了 `CREATE_NO_WINDOW` 标志位，确保启动第三方控制台工具时桌面不会闪出黑色命令行窗口。

## 🧭 开发导航

看这个基本就能提交第一个后端改动了。前端部分见 [frontend/README.md](./frontend/README.md#-开发导航)。

<details>
<summary><b>0. 起步：代码从哪开始看</b></summary>

调用链很短，按这个顺序读一遍就有全局观：

```
main.go              仅 //go:embed frontend/dist + 调 app.Run(assets)
└── internal/app/run.go
    ├── init()               注册所有前后端事件名（RegisterEvent）
    ├── newPrototypeService  窗口/播放探针等原型能力
    ├── newLoopbackMediaServer  本地视频流服务
    ├── newFFmpegManager     FFmpeg 下载与校验
    ├── newDesktopService    主服务：配置、媒体库、缓存、导出、更新
    └── application.New(...) 注册 Services、创建窗口
```

业务逻辑基本都在 `DesktopService` 上，按主题分散在 `desktop_service.go`（配置/媒体库/缓存/缩略图）、`library_management.go`、`export.go`、`cache_lru.go`、`upload.go`、`auto_update.go` 里。

</details>

<details>
<summary><b>1. 加一个前端能调用的接口</b></summary>

Wails v3 会把注册进 `Services` 的类型上的**导出方法**自动生成为前端绑定，所以只需要：

1. 在 `internal/app/` 下给 `DesktopService`（或 `PrototypeService`）加一个导出方法：

   ```go
   func (s *DesktopService) MyFeature(id string) (MyResult, error) { ... }
   ```

2. 参数与返回值必须是可 JSON 序列化的类型，结构体字段带 `json:"..."` 标签——前端拿到的字段名以标签为准。
3. 重跑 `task dev`（或 `task build`），Wails 重新生成 `frontend/bindings/`。
4. 前端在 `src/bridge/desktop.ts` 里加一行封装，业务代码通过 `window.desktop.myFeature()` 调用。

几条项目内的约定：

- **入参一律当作不可信输入**。库 ID 必须过 `validLibraryID` 正则再拼路径，避免路径穿越；现有方法（`TouchCache`、`ClearCache` 等）都是这么做的。
- **错误信息直接面向用户**，用中文写清楚原因（`fmt.Errorf("读取配置失败：%w", err)`），前端一般直接 toast 出去。
- 返回切片时注意 Go 的 nil 切片在前端是 `null`，桥接层会补 `[]`，新增接口请同步在 `desktop.ts` 里补一下。

</details>

<details>
<summary><b>2. 派发一个新事件</b></summary>

事件是后端主动推给前端的唯一通道（进度、状态变更、窗口指令都走它），加一个要动三处：

1. `run.go` 的 `init()` 里注册类型与名字：`application.RegisterEvent[MyPayload]("my:event")`。**漏这一步事件不会生效。**
2. 后端派发：`app.Event.Emit("my:event", payload)`。注意 `s.app` 是 `attach()` 之后才有的，早期阶段要判空（参考 `reportProgress`）。
3. 前端在 `desktop.ts` 里 `listen("my:event", cb)` 包一个 `onXxx`。

命名统一用 `域:动作`（`media:progress`、`thumb:ready`、`update:status`），别用连字符。

</details>

<details>
<summary><b>3. 并发与锁的规矩</b></summary>

`DesktopService` 上有四把锁，各管各的，**不要用一把锁包打天下**：

| 锁 | 保护范围 |
| :--- | :--- |
| `mu` (RWMutex) | `config`、`library`、`app`/`home`/`media` 等字段 |
| `cacheMu` | 缓存目录的文件操作（清理、收敛） |
| `videoMu` | `videoJobs` 任务表 |
| `updateMu` | 更新状态机 |

两条铁律：

- **持锁期间不做 I/O、不发事件、不等待子进程**。标准写法是加锁取快照 → 解锁 → 干活：

  ```go
  s.mu.RLock()
  media, app := s.media, s.app
  s.mu.RUnlock()
  ```

- **`...Locked` 后缀表示调用方必须已持锁**（如 `saveLibraryLocked`、`convergeCacheLocked`），新增内部函数请延续这个命名，否则很容易嵌套自锁死。

</details>

<details>
<summary><b>4. 调用 FFmpeg 或其他外部进程</b></summary>

三件事一个都不能少：

```go
ffmpeg, err := m.ffmpeg.Ensure(ctx)              // ① 拿路径（必要时会触发下载/校验）
command := exec.CommandContext(ctx, ffmpeg, ...) // ② 必须用 CommandContext，任务才能被取消
platformprocess.SuppressConsoleWindow(command)   // ③ 必须调，否则 Windows 上会闪黑窗
```

- **进度**：解析 FFmpeg 的 stderr / `-progress` 输出（现成正则见 `media_engine.go` 顶部），通过 `emit(MediaProgress{...})` 抛给前端，别在循环里直接发事件淹没 IPC。
- **可取消的长任务**：把 `context.CancelFunc` 存进 `videoJobs`（key 用库 ID），前端调 `CancelMediaJob(id)` 即可打断，任务结束记得从表里删掉。
- **中间产物**落 `paths.TempDir`，并留意 `.part` 后缀——启动时的 `sweepPartFiles()` 只清理这类半成品。

</details>

<details>
<summary><b>5. 读写用户数据</b></summary>

- **路径一律从 `AppPaths` 取**（`app_data.go`），别自己拼 `%APPDATA%`。`NONOKA_DATA_DIR` 可以整体重定向，测试就靠它。
- **读用 `readJSON`**：文件不存在时返回 nil 而非报错，首次启动直接走默认值。
- **写用 `writeJSONAtomic`**：先写临时文件再 rename，避免写一半断电留下坏文件。
- **媒体库条目务必走 `libraryDiskEntry`**：它的 `UnmarshalJSON`/`MarshalJSON` 会把当前 Go 结构体不认识的字段原样保留（`Extra`）。如果图省事直接用 `LibraryEntry` 读出来再写回，**旧版或新版写入的额外字段会被静默抹掉**。

</details>

<details>
<summary><b>6. 给前端播放视频</b></summary>

**不要把视频交给 Wails 的 AssetServer**——它会把响应整体读进内存，大文件直接卡死。

正确做法是 `media_server.go` 的本地环回服务：调 `RegisterURL(sequence, path)` 注册文件并拿到一个 `http://127.0.0.1:<随机端口>/media/<token>?v=<seq>` 地址，交给前端 `<video>` 即可。它支持 Range/206 分块，拖动是毫秒级的。

路由前缀里的 24 字节随机 token 是安全边界（防止本机其他程序扫端口读文件），别把它打进日志或前端可见的地方。

</details>

<details>
<summary><b>7. 调试用环境变量</b></summary>

| 变量 | 作用 |
| :--- | :--- |
| `NONOKA_DATA_DIR` | 重定向整个数据根目录，调试时避免污染真实媒体库 |
| `NONOKA_SKIP_FFMPEG=1` | 跳过 FFmpeg 下载与就绪检查 |
| `NONOKA_DISABLE_UPDATE=1` | 关闭自动更新检查 |
| `NONOKA_UPDATE_URL` / `NONOKA_WAILS_UPDATE_MANIFEST` | 指向自建的更新清单，用于联调升级流程 |
| `NONOKA_PROTOTYPE_SMOKE=1` | 自检模式，配合 `NONOKA_PROTOTYPE_MEDIA` / `*_RESULT` 写出探针结果 |
| `NONOKA_FORMAL_MOCK=1` | 配合 `smoke-key` 启动内置的 mock 后端，脱离云端跑主流程 |

</details>

<details>
<summary><b>8. 测试约定</b></summary>

- 测试文件跟随被测包放置（`internal/app/xxx_test.go`），`go test ./...` 全量跑。
- **不要碰真实用户目录**：统一用 `newDesktopService(appPathsAt(t.TempDir()), ...)` 造隔离实例，现成写法见 `cache_lru_test.go` 的 `newCacheTestService`。
- 需要 FFmpeg 的路径用 `newFFmpegManagerWithOptions(ffmpegOptions{})` 注入假配置，测试不应真的联网下载。
- 涉及缓存、更新、导出这类有副作用的逻辑，请补一个用例——这几块已有的测试覆盖率是项目里最高的，别打破。

</details>

<details>
<summary><b>9. 常见坑</b></summary>

- **改了 Go 方法签名必须重启 Wails**，只重启 Vite 拿到的还是旧的 `frontend/bindings/`。
- **`s.app` 在 `attach()` 之前是 nil**，启动早期发事件要判空。
- **导出方法即公开 API**：不想暴露给前端的辅助函数请用小写开头，否则会被生成进 bindings。
- **窗口尺寸/最大化状态**存在 `config.json` 的 `bounds` 里，恢复逻辑在 `window_state.go`，改窗口创建流程时注意别绕过它（有防止窗口跑到屏幕外的校验）。
- **导出任务 id 由前端 `expJob()` 与后端约定生成**，两边必须同时改。

</details>

## 📂 数据与存储路径

应用会自动在用户系统目录中保存核心配置与媒体库缓存
*(⚠ 提示：不建议手动修改数据库或缓存目录内容)*

- **数据根目录**（可用 `NONOKA_DATA_DIR` 整体重定向，见开发导航）
  - Windows: `%APPDATA%\Nonoka`
  - macOS: `~/Library/Application Support/Nonoka`
- **核心结构**（定义见 `internal/app/app_data.go`）
  - `videos/`：大容量视频缓存区，受 LRU 容量上限约束。
  - `thumbs/`：媒体快照缩略图缓存。
  - `ffmpeg/`：自动下载并校验后的 FFmpeg 二进制。
  - `temp/`：抽取音轨等中间产物，`.part` 为未完成的下载。
  - `webview/`：WebView2 运行时数据。
  - `config.json`：全局偏好设置与窗口位置。
  - `library.json`：本地媒体库与工程的索引库。

## 📚 更多文档

- [🌐 前端子项目](./frontend/README.md)：MPA 架构、状态管理与开发导航。


## 📄 开源协议

本项目采用 **GPL-3.0** 开源许可证，详情参阅 [LICENSE](./LICENSE) 文件。
