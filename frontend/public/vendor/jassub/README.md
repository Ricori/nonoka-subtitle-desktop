# JASSUB（libass 的 WASM 版）—— 字幕预览渲染器

编辑器预览用它渲染字幕，保证「预览 = 导出的 ASS 交给播放器渲染」逐像素一致。
在此之前预览是 DOM 近似（字号/描边/行距/碰撞规避都得自己凑，永远对不齐）。

- 上游：<https://github.com/ThaUnknown/jassub> v2.5.7（npm `jassub`）
- 许可：见 LICENSE（JASSUB 本体 MIT）；wasm 内含 libass / FreeType / fribidi 等，
  完整声明为 `LGPL-2.1-or-later AND (FTL OR GPL-2.0-or-later) AND MIT AND
  MIT-Modern-Variant AND ISC AND NTP AND Zlib AND BSL-1.0`

## 产物怎么来的

上游发的是未打包的 ESM + 4 个 npm 依赖，本项目是无构建链的静态页，所以预先打成
自包含 IIFE 提交进仓库。重新生成（升级版本时）：

```sh
npm i jassub@2.5.7 esbuild
npx esbuild node_modules/jassub/dist/jassub.js --bundle --format=iife \
  --global-name=JASSUBmod --outfile=jassub.js --legal-comments=none --minify \
  --define:import.meta.url='"file:///jassub/"'
npx esbuild node_modules/jassub/dist/worker/worker.js --bundle --format=iife \
  --outfile=jassub-worker.js --legal-comments=none --minify \
  --define:import.meta.url='"file:///jassub/"'
cp node_modules/jassub/dist/wasm/jassub-worker-modern.wasm .

# 三个渲染器建 context 时都带 desynchronized: true，必须关掉，理由见下（minify 后是 !0）
python -c "s=open('jassub-worker.js',encoding='utf-8').read(); n=s.count('desynchronized:!0'); assert n==4, n; open('jassub-worker.js','w',encoding='utf-8').write(s.replace('desynchronized:!0','desynchronized:!1')); print('patched',n)"
```

两个要点，改动前先读：

- `--define:import.meta.url` 不能省。IIFE 里 `import.meta.url` 会变成 undefined，
  emscripten glue 拿它 `new URL()` 定位 wasm 会抛 `Invalid URL`，而 JASSUB 又把
  构造期异常包成了 proxy——表现是 `ready` 正常 resolve，之后每个方法调用都报
  `Cannot read properties of undefined (reading 'apply')`。值本身无所谓，合法即可：
  wasm 真正的来源是调用方传的 `wasmUrl`（JASSUB 内部 hack 了 `fetch`）。
- worker 打成 IIFE 但仍以 `{type:'module'}` 加载（JASSUB 写死的）——IIFE 代码本来
  就是合法的 ES module，这样 blob worker 里不会有解析不了的相对 import。

只带 SIMD 版 wasm（`-modern`），省 2 MB；调用处 `wasmUrl` 与 `modernWasmUrl` 都指它。
Electron 33（Chromium 130）和所有目标浏览器都支持 WASM SIMD。

## 调用方三个必须踩对的点

改 editor.html 里的初始化前先看这几条，每条都是实测踩出来的：

- **canvas 标签上不要写 width/height。** JASSUB 只在「渲染尺寸 ≠ 当前 canvas 尺寸」时
  才把分辨率 uniform 传给 shader；初始尺寸恰好等于它算出来的渲染尺寸时，那个 uniform
  一直是 0，顶点坐标除零，整张画面全空——而且 `_draw` 不报任何错，libass 那边一切正常。
- **字体走 `fonts: [...]` 全量预载，不要用 `availableFonts` 的按名懒加载。**
  后者是异步的，libass 同步问字体时先拿到「没有」，于是回退成默认字体（日志里是
  `Using default font family: (荆南波波黑, …) -> FZY3K--GBK1-0`）。
- **`queryFonts` 必须是 `false`。** 设成 `'local'` 走 Local Font Access 那条路，在
  Electron 里会把 wasm 直接搞崩：`rawRender` 抛
  `RuntimeError: null function or function signature mismatch`。
  代价是模板里写了没随包带的字体（比如注释样式的思源黑体）只能回退到 `defaultFont`。
- **`desynchronized` 必须关掉**（打包步骤里那条 patch）。上游三个渲染器建 context 时
  都开着它，走的是低延迟合成路径；一旦下面压着**正在播放**的 `<video>`（会被提升成
  硬件 overlay 层），这张 canvas 就变成不透明黑块，把视频整个盖住——JASSUB 本身毫无
  异常，字幕也在画，看起来就是「预览区黑屏」。视频暂停时不一定复现。
  字幕预览完全不需要那点延迟，关掉纯赚。

`wasmUrl` 要给绝对地址：它是在 worker 里解析的，相对路径会叠到 worker 自己的目录上。

## desktop 与 web 的差别

web 走普通 URL 即可。desktop 的 renderer 是 `file://`（`loadFile`），Chromium 在
该协议下既不让 `new Worker('./x.js')` 也不让 `fetch('./x.js')`，所以 worker 脚本、
wasm、字体一律由 preload 用 fs 读出来，在渲染进程里转成 blob URL / Uint8Array 再
喂给 JASSUB。blob worker 本身在 file:// 下是允许的（已实测）。
