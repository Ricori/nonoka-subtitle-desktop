# Nonoka Subtitle Desktop (Frontend)

桌面端应用的前端工程，技术栈为 **React 19 + TypeScript 5.9 + Vite 8**。

为了配合桌面端的多窗口管理，前端采用 **多页面 (MPA)** 架构：主页与视频编辑页是两个独立的 HTML 入口，分别运行在不同的系统窗口中。

## 🚀 快速开始

脱离 Wails 后端做纯 UI 调试时，可以直接用 Vite 启动（此时 `window.desktop` 不存在，后端调用会失败，仅适合看样式与布局）：

```bash
npm install
npm run dev
```

完整开发请在上层目录跑 `task dev`，Wails 会自动拉起这里的 Vite。

| 脚本 | 作用 |
| :--- | :--- |
| `npm run dev` | 启动 Vite 开发服务器（Wails 开发模式会自动拉起，一般不用手动执行） |
| `npm run build` | 生产构建，产物落到 `dist/`（Wails 打包时自动调用） |
| `npm run build:dev` | 不压缩的 development 构建，便于排查打包产物问题 |
| `npm run typecheck` | `tsc --noEmit` 严格类型检查，**提交前必跑** |
| `npm run preview` | 本地预览 `dist/` 产物 |

## 🏗️ 架构概览：多页面应用 (MPA)

前端不是传统 SPA，而是在 `vite.config.ts` 的 `rollupOptions.input` 中声明了两个完全独立的入口：

1. **🏠 主页窗口 (`index.html`)**
   - **入口链路**：`src/home/index.tsx` → `src/home/App.tsx`
   - **职责**：视频媒体库、导入视频、转写流水线进度、术语表与软件设置。
2. **🎬 编辑器窗口 (`editor.html`)**
   - **入口链路**：`src/editor/index.tsx` → `src/editor/App.tsx`
   - **职责**：专业字幕编辑器，含时间轴、波形图、ASS 实时预览与拖拽交互。
   - 该页在模块脚本之前先同步引入了 `public/vendor/jassub/jassub.js`，字幕渲染引擎需要它先在全局就位。

两个页面**不共享**任何内存状态（分属不同系统窗口），跨窗口协作只有三条通道：后端的 Wails 事件（如 `home:refresh`）、后端持久化数据，以及同源 `localStorage` 的 `storage` 事件（主题切换就走这条）。

## 📂 核心目录结构

```text
frontend/
├── index.html         # 主页入口
├── editor.html        # 编辑器入口
├── bindings/          # Wails 自动生成的 Go 绑定与类型，请勿手改
├── public/            # 静态资源：字体、图标、JASSUB 引擎
└── src/
    ├── bridge/        # 桥接层：封装所有 Go 后端调用与事件监听（desktop.ts）
    ├── home/          # 主页模块
    │   ├── components/  # UI 组件（TopBar、VideoWall、modals/ 等）
    │   ├── css/         # 按功能拆分的样式，由 index.css 汇总
    │   ├── hooks/       # useTheme、首屏探测等
    │   ├── lib/         # createStore（两个页面共用）、apiClient、notify
    │   └── store/       # 状态切片
    ├── editor/        # 编辑器模块（目录划分同上，另有 lib/ 承载核心引擎）
    └── types/         # 全局类型声明
```

> ⚠️ `createStore.ts` 目前住在 `home/lib/` 下，但编辑器也从这里引入（`import { createStore } from '../../home/lib/createStore'`）。改它等于同时改两个页面。

### `src/home/` 里面有什么？

- `components/`：主页 UI（顶栏 `TopBar`、视频瀑布流 `VideoWall`、`modals/` 下的各类弹窗）。
- `lib/`：`apiClient.ts` 封装云端 HTTP 与 401 拦截，`notify.ts` 提供 toast / loading / 页内确认框。
- `store/`：去中心化的状态切片，每片对应一块独立业务。

<details>

<summary><b>点击展开：主页核心状态切片</b></summary>

| Store 切片 | 关键状态 | 深入解析 |
| :--- | :--- | :--- |
| `bootStore` | `bootLead`, `ffmpegStatus`, `initialCheckPending`, `mandatoryActive`, `news` | 启动序列。FFmpeg 安装闸门 + 强制更新闸门 + 登录校验，三者跑完才决定进登录页还是主界面。`runBootSequence()` 全局只跑一次。 |
| `sessionStore` | `cfg`, `appPhase`, `keyOK`, `keybar` | 会话与配置。`appPhase` 三态（`boot`/登录/就绪）会同步写到 `document.body.dataset.app`，部分不受 React 控制的元素靠它驱动 CSS 显隐。 |
| `libraryStore` | `lib`, `remote`, `merged`, `cachedSet`, `srcSet` | 视频库数据中心。本地条目（`lib`）与云端记录（`remote`）合并成 `merged` 供列表渲染；`cachedSet`/`srcSet` 标记缓存与源文件是否还在。内部有轮询与请求去重。 |
| `pipelineStore` | `pipe` | 转写流水线：抽音频 → upload init → 流式 PUT → start → 换主键 → 轮询。原视频**永不上传**，只传音轨，同时并行把原视频复制进缓存。 |
| `glossaryStore` | `sets`, `items`, `spkOn`, `spkNum`, `glossValue` | 术语表清单缓存 + 转写选项弹窗（说话人 / 术语表）的 Promise 式确认。`sets` 为 `null` 表示还没拉过，`{}` 是合法的空结果。 |
| `uiStore` | `filter`, `sortMode`, `view`, `settingsOpen`, `popover` | 搜索排序偏好 + 弹窗开合。`popover` 用字符串 id 保证「同时只开一个」浮层。 |

</details>

### `src/editor/` 里面有什么？

编辑器是重交互模块，逻辑最复杂的部分都在这里：

- `components/`：核心交互 UI（`Timeline` 时间轴、`VideoStage` 播放器、`WaveRow` 波形图、`SegList` 字幕列表、`Inspector` 属性面板）。
- `lib/`：编辑器引擎。`edits.ts` 字幕增删改、`history.ts` 撤销栈、`laneDrag.ts` 拖拽、`playback.ts` 播放同步、`assBuild.ts`/`subtitles.ts` 字幕编译与预览、`wave.ts` 波形、`exportAss.ts` 导出、`closeFlow.ts` 关闭拦截。
- `store/`：按更新频率与职责切开的状态切片。

<details>
<summary><b>点击展开：编辑器核心状态与引擎</b></summary>

**1. 状态切片 (`src/editor/store/`)**

| Store 切片 | 关键状态 | 深入解析 |
| :--- | :--- | :--- |
| `docStore` | `segs`, `tracks`, `trackMeta`, `assTemplate`, `rev`, `version` | **文档源数据**。句对象**就地可变**，改完调 `bumpDoc()` 触发重渲染（详见下方开发导航）。`rev` 是服务端乐观锁版本。 |
| `viewStore` | `duration`, `t0`, `t1`, `curClip`, `clips`, `pps` | **视图窗口与缩放**。`pps` = 像素/秒。所有时间数据都是「原片绝对秒」，只有时间↔像素换算按 `t0` 平移，所以存盘、ASS 生成、撤销栈都不需要知道切片的存在。 |
| `tlStore` | `left`, `w`, `top`, `h`, `scrollH` | **时间轴 DOM 指标**。持有滚动容器引用，`curVp()` 一次读齐、`syncTlMetrics()` 统一写回——读写交错会强制同步重排。 |
| `playStore` | `t`, `playing`, `rate` | **播放头**。单独一片是有意的：播放时 `t` 每帧都变，混在别的状态里会让整个编辑器每秒重渲染 60 次。 |
| `selStore` | `curTrack`, `sel`, `selSet`, `preview` | **选中状态**。`selSet` 存句**对象引用**而非下标（多选可跨轨，下标会串位）；`sel` 是激活轨内的主选中下标。 |
| `dragStore` | `marquee`, `dropTi` | **拖拽中的临时态**。框选矩形与跨轨拖动的目标轨高亮。 |
| `layoutStore` | `sideW`, `lblW`, `tlViewH`, `rowH`, `waveGain`, `snap` | **本机布局偏好**，持久化到 `localStorage`。轨道隐藏、自定义行高这类跟数据走的偏好存在服务端 `track_meta` 里。 |
| `videoStore` | `src`, `retrieving`, `fallbackOpen`, `badge` | **视频区占位卡**。挂载走「缓存副本 → 原文件还在 → 从 R2 取回 → 让用户重新定位」这条解析链，后三种都不阻塞字幕编辑。 |
| `saveStore` | `dirty`, `saving`, `conflicted`, `stateText` | **保存**。手动 `Ctrl+S` + 每 5 分钟自动保存，走 `rev` 乐观锁；409 后 `conflicted` 置位转为本地只读。 |
| `exportStore` | `open`, `clip`, `busy`, `pct` | **导出流程**。任务 id 由 `expJob()` 生成，与 Go 端约定一致，改一处必须改另一处。 |
| `uiStore` | toast / ask 弹窗 / 右键菜单 / 轨道浮层 | **编辑器 UI 池**，与主页的 `uiStore` 完全独立。 |

**2. 核心引擎 (`src/editor/lib/`)**

- **时间轴拖拽 (`laneDrag.ts`)**：基于原生 `PointerEvent` 的拖拽引擎，支持拖动片段、拉伸边缘，并内置吸附（相邻块边缘、播放头）。
- **音视频驱动 (`playback.ts`)**：接管 `<video>` 元素，用 `requestAnimationFrame` 高频更新 `playStore.t`，实现播放头与画面的帧级同步。
- **撤销/重做 (`history.ts`)**：改动前压入深拷贝快照，最多 `HISTORY_MAX`（60）步。文本编辑按「聚焦 → 首次输入」合成一步（`armPending`/`commitPending`），拖动按「首次移动」入栈；有新改动即清空重做栈。
- **字幕编译 (`assBuild.ts` / `subtitles.ts`)**：把文档按样式模板编译成 ASS 交给 JASSUB。`syncSubs()` 重建整份 track，`syncSubsSoon()` 是给逐字输入用的 80ms 合并版，`drawSubs()` 只重画当前帧。

</details>

## 🔄 数据状态怎么管的？

没有 Redux，只有 `home/lib/createStore.ts` 里约 60 行的响应式实现，基于 `useSyncExternalStore`。

```ts
const store = createStore({ a: 1, b: 2 });

store.get();                 // 同步读最新值，供跨 await 的编排函数使用
store.set({ a: 2 });         // 浅合并；所有字段引用都没变则不通知订阅者
store.set(s => ({ a: s.a + 1 }));
const a = store.use(s => s.a);   // 组件内按需订阅
```

三条必须记住的规则：

1. **`set` 是浅合并**，且用 `Object.is` 逐字段比较；写进去的值引用没变就不会触发渲染。
2. **`use` 必须传精确的 selector**，别把整个 state 取出来再点属性。
3. **selector 返回新对象时必须给 `isEqual`**，否则每次渲染都是新引用 → 无限重渲染：

   ```ts
   const { a, b } = store.use(s => ({ a: s.a, b: s.b }), shallowEqual);
   ```

## 🔌 和 Go 后端怎么打交道？

所有交互收口在 `src/bridge/desktop.ts`，它对 `bindings/` 下 Wails 自动生成的绑定做了一层封装，并把结果挂到全局 `window.desktop`。业务代码统一用 `window.desktop.xxx()`，不要直接 import `bindings/`。

1. **主动调用**：`window.desktop.getLibrary()`、`window.desktop.importVideos(paths)`、`window.desktop.renderExport(options)` ……方法名基本对应 Go 端的 Service 方法。
2. **监听事件**：`on*` 系列返回一个取消函数，务必在 effect 里回收。

   ```ts
   useEffect(() => {
     const off = window.desktop.onProgress(p => { /* ... */ });
     return () => off?.();
   }, []);
   ```

   现有事件：`ffmpeg:status`、`media:progress`、`thumb:ready`、`video:ready`、`video:failed`、`home:refresh`、`files:dropped`、`update:status`、`update:ready`、`prototype:request-close`。

桥接层还顺手抹平了几个后端差异，新增方法时建议延续：Go 的 nil 切片统一补成 `[]`（如 `getLibrary`）、可选参数在这里补默认值（如 `removeVideo`）、`setConfig` 先读回完整配置再合并写回。

> `bindings/` 由 Wails 在编译时生成，**不要手动修改**。Go 侧签名改了就重新跑一次 `task dev` 或 `task build` 同步。

## 🧭 开发导航

读完这节，基本就能独立提交第一个改动了。

<details>
<summary><b>0. 起步：跑起来 & 调试</b></summary>

1. 在 `desktop/` 下 `task dev`，Wails 会启动 Go 后端并拉起本目录的 Vite。
2. 应用窗口内按 `F12`（或右键检查）打开 WebView2 DevTools，Console 里可以直接敲 `await window.desktop.getLibrary()` 试后端接口。
3. 改前端代码走 Vite HMR，热更新即时生效；改 Go 代码需要 Wails 重启。
4. 提交前跑 `npm run typecheck`，`tsc` 是严格模式，类型错误不会被 Vite 拦下来。

</details>

<details>
<summary><b>1. 加一个后端能力</b></summary>

从 Go 到 UI 的完整链路，四步：

1. **Go 侧**：在 `internal/app/desktop_service.go`（或对应文件）上给 Service 加导出方法。
2. **重跑 `task dev`**：Wails 重新生成 `frontend/bindings/`，TS 类型自动跟上。
3. **桥接层**：在 `src/bridge/desktop.ts` 的 `desktopBridge` 里加一行。方法**扁平摆放**，除 `ffmpeg` 外不再分组；如果 Go 返回可能为 nil 的切片，或有可选参数，在这里补好默认值，别让每个调用方各判一次空。
4. **业务侧**：在对应 store 的动作函数里调用，把结果写进 store，组件通过 `use(selector)` 订阅。

**新增事件**同理：Go 端 `Emit` 一个名字，桥接层用 `listen("your:event", cb)` 包一个 `onXxx`，组件/hook 在 effect 里订阅并回收。

</details>

<details>
<summary><b>2. 加一块状态</b></summary>

- **往已有切片加字段**：直接改那个 `interface` 与 `createStore` 初值，再写一个语义化的 setter（项目习惯是 `export const setXxx = (xxx) => store.set({ xxx })`），组件不直接调 `set` 写裸对象。
- **新建切片的判断标准**：**按更新频率和业务边界切**。编辑器把 `playStore` 单独拆出来就是因为它每帧都在变；`tlStore` 单独拆是因为它是 DOM 指标而非业务数据。一块状态如果只被一两个组件用、且更新很频繁，就单独开一片。
- **异步编排**：跨 `await` 的流程里用 `store.get()` 读最新值，不要闭包捕获旧的 state。

</details>

<details>
<summary><b>3. 改字幕文档</b></summary>

`docStore` 里的句对象是**就地可变**的——拖动、文本编辑直接改 `seg.t0` / `seg.ja` 字段。这是有意为之：`selSet`、撤销快照、拖拽都靠对象引用认句，做一次不可变复制就全断了。

代价是渲染和副作用得手动触发。任何修改字幕的操作都遵循同一套顺序（照抄 `lib/edits.ts` 里的现成函数即可）：

```ts
pushHistory();          // ① 改之前先存快照，否则这一步无法撤销
seg.t1 = newEnd;        // ② 就地改
refreshAll();           // ③ = bumpDoc() + syncSubs()，刷新组件与字幕预览
markDirty();            // ④ 标记未保存，状态栏与关闭拦截靠它
```

漏掉任何一步的典型症状：
- 漏 `pushHistory()` → `Ctrl+Z` 跳过了这次改动。
- 漏 `bumpDoc()` → store 里数据变了但界面纹丝不动。
- 漏 `syncSubs()` → 列表更新了，但视频上的字幕预览还是旧的（libass 按整份 ASS 渲染，只重画当前帧看不见改动）。逐字输入场景请用 `syncSubsSoon()`。
- 漏 `markDirty()` → 改动不会被保存，关窗也不提示。

</details>

<details>
<summary><b>4. 改时间轴：坐标换算与重排</b></summary>

- **时间 ↔ 像素**一律走 `viewStore` 的辅助函数，不要自己乘 `pps`：`xOf(t)` 秒→像素、`tOf(x)` 像素→秒、`tAtClientX(e.clientX)` 鼠标视口坐标→绝对秒（已减掉容器左边界）。
- **所有时间都是原片绝对秒**。进入切片只是收窄 `t0`/`t1` 视图窗口，显示用时间码由 `fmtView()` 平移，存盘数据不受影响。
- **读 DOM 指标用 `curVp()` 一次读齐，改完用 `syncTlMetrics()` 写回**。读一下 `scrollLeft` 又改一下样式再读，会触发强制同步重排——侧栏几千行列表在场时一次就是二三十毫秒。
- **手感常量集中在 `constants.ts`**（缩放上下限、行高、虚拟化缓冲区、吸附阈值等），调参改这里，别在组件里写魔数。
- **面板拖拽用 `lib/split.ts` 的 `splitHandler(getStart, apply)`**，它已经处理了指针捕获、`.drag` 高亮和 `pointercancel` 收尾。

</details>

<details>
<summary><b>5. 加弹窗与提示</b></summary>

**绝对不要用 `window.confirm` / `window.prompt` / `alert`**：`confirm` 会开一个独立模态窗口，弹出期间整个应用（含主进程 IPC）全冻结；`prompt` 在这类壳里直接抛「不受支持」。

主页用 `home/lib/notify.ts`，编辑器用 `editor/store/uiStore.ts`，两套 API 略有差异但用法一致：

```ts
toast("已保存");                          // 轻提示
toast("导出失败", true);                  // 主页：第二个参数标记为错误
if (await confirm("确定删除？", "删除")) { /* ... */ }
const name = await promptText("重命名", "确定", oldName);   // 取消返回 null
const done = beginLoading("处理中…");     // 主页：loading 遮罩，返回结束函数
```

多个来源同时要求 loading 会计数，最后一个结束才真正隐藏，所以务必调用返回的结束函数（`try/finally`）。

</details>

<details>
<summary><b>6. 样式、主题与无边框窗口</b></summary>

- **纯 CSS，无 UI 库**。样式按功能拆到 `css/*.css`，新增文件后记得在 `css/index.css` 里 `@import`，否则不会生效。
- **颜色一律用 CSS 变量**（`--bg`、`--panel`、`--text`、`--accent`……），定义在各自的 `base.css` 里。项目有「星夜」（暗）与「白丝带」（亮）两套主题，亮色靠 `body.light` 覆盖变量——写死十六进制色值会导致亮色主题下瞎掉。
- **主题切换**在 `home/hooks/useTheme.ts`：写 `localStorage`、切 `body.light`、同步 Windows 标题栏叠加色；另一个窗口通过 `storage` 事件跟随。
- **无边框窗口拖拽**：Windows 下自定义标题栏由 `bridge/desktop.ts` 的 `installWindowsTitlebar()` 注入（右上角三个按钮用 `--wails-non-client-region` 标记）。新增顶部 UI 时**务必**给可拖拽区域加上 `--wails-draggable: drag`，给其中的按钮加 `no-drag`，否则窗口拖不动或按钮点不了。

</details>

<details>
<summary><b>7. 加快捷键</b></summary>

编辑器的全局快捷键集中在 `editor/hooks/useShortcuts.ts` 一个 `keydown` 监听里，注意其中的**拦截顺序**：

1. `Ctrl/⌘+S` 放在最前面——它在输入框内也要生效。
2. 随后遇到 `INPUT`/`TEXTAREA` 直接 `return`，把按键交还给输入框。
3. 之后才是 `Ctrl+Z`/`Ctrl+Y` 与单键操作（空格播放、方向键步进、`I`/`O` 吸附、`N` 新建、`D` 切分、`V` 延长、`Delete` 删除）。

加新键请按这个分层插入，并想清楚它该不该在文本编辑时生效。

</details>

<details>
<summary><b>8. 常见坑速查</b></summary>

- **两个页面是物理隔离的**。强业务组件严格放在 `home/` 或 `editor/` 下，不要跨页引用；需要联动就走后端事件或持久化数据。
- **`store.use` 一定传 selector**，返回对象时配 `shallowEqual`。Timeline 动辄成百上千个字幕块，这一条直接决定性能。
- **事件订阅必须在 effect 清理函数里退订**，`on*` 的返回值就是取消函数。
- **不要 import `bindings/` 里的方法**（类型可以 import），后端调用统一走 `window.desktop`。
- **纯 Vite 模式下 `window.desktop` 是 undefined**，写模块顶层代码时注意可选链，别在导入期就崩掉整页。
- **改了 Go 签名要重启 Wails**，只重启 Vite 拿到的还是旧 bindings。

</details>
