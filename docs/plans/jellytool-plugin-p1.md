# Jellytool 插件化 P1：独立宿主

日期：2026-11-03。工作树 `jellytool-plugin/magpie`，分支
`codex/jellytool-plugin`。这是本地开发实现，不是签名发布或真实工具验收。

## 入口与身份

`go build -tags jellytool` 选择 `internal/product` 的固定产品常量。
不带该标签仍构建上游 magpie；切换名称或传入环境变量不能把普通二进制变成
Jellytool。Jellytool 在 CLI 主入口最前面分流，只有 app/help/version/licenses
和原 Agent 断开预览的隔离子进程可以进入；其他命令在迁移和网络设置前拒绝。

- 配置、认证、插件和锁：`~/.config/jellytool`（遵循绝对 XDG 根）。
- 缓存：`~/.cache/jellytool`；系统缓存和 Windows WebView2 同样独立。
- 便携标记：`.jellytool-portable`，数据目录 `jellytool-data`；不会识别 magpie
  的 `data` 或 `.portable`。
- 默认网关：`127.0.0.1:3435`，固定回环监听；`MAGPIE_ADDR` 和复制来的端口设置
  不能改变它。占用就失败，不探测、接管或关闭占用者。
- 数据根内持有 `jellytool-instance.lock`，拿不到锁就不运行第二个实例。
- macOS 开发 Bundle ID：`com.jellytoken.jellytool.dev`。没有注册 URL scheme。
- 当前原生开发壳是一个 Jellytool 主窗口，没有原 magpie 托盘和接管流程。
  macOS 已设置关闭最后窗口即退出，隐藏窗口 API 也退出 App；这些行为仍须在
  原生平台验收，测试网页不能证明。

`build/jellytool/package.py --bun /absolute/bun --output /new/Jellytool.app`
构建未签名 macOS 开发包，内置 Bun 1.4.2 和 Jellytoken 资源，保留上游 LICENSE
及 THIRD_PARTY_NOTICES。构建工具不启动 App。Windows 交叉构建只证明可编译，
Windows 安装包、运行和签名属于 P3。正式分发前还要完成 Bun 的许可证清单。

## 运行链路

`cli.jellytoolMain -> gui.RunJellytool -> plugin.ActivateJellytool` 验证并启动 P0（2026-11-19：`jellytoolMain` 从未接入，已删除；应用由 `openApp` 直接进入 `RunJellytool`）
的私有宿主。供应商适配器继续调用 `plugin.Cached/Providers/APIKey/LoaderOptions/Fetch`，
这些入口在 Jellytool 构建中只转到已激活的私有宿主和内存目录。插件异常退出后
不会下载 Bun 或启动通用宿主；需要重启 Jellytool。

只有内置 `jellytoken`，固定账号 `jellytoken`，一个 Key 替换已有 Key。
Key 仍在私有 `plugin-auth.json` 中，应用和 Key 的生命周期独立于 magpie。
通用 `plugins.json`、插件目录缓存、第三方选项和通用插件 RPC 不能选择其他插件。

原供应商页收到同样的数据结构；新增的宿主边界只保存授权目录中的模型 ID、
选空及开关，不保存旧表单携带的 URL、Key、代理、模型元数据或回退设置。
目录刷新、开关、替换 Key 和清除 Key 都收窄到内置插件。旧高级选项、手填模型、
测试推理、删除供应商和账户管理控件不进入产品流程。推荐功能及兼容性确认仍是 P2。

Agent 页面复用原来的状态、模型选择、漂移、连接和断开接口，仅提供本机 Codex、
Claude Code、OpenCode、WorkBuddy。不会加载第三方 Agent 插件或扫描 WSL。
供应商选集保存不会设置 `catalog.Changed = agent.SyncCatalogSoon`，因此不会自动
把新选集写入已有 Agent。原 Agent 配置预览只在原有临时 HOME 子进程运行。
这些原适配器仍使用部分上游内部字段名和配置节名；P3 必须检查与已有 magpie
配置的共存和恢复，不能因 App 状态隔离就宣称工具配置已经共存验收。

## 边界

原生宿主不调用上游 `gui.Run/startBackend`、CLI `startUp` 或网关
`ListenAndServe`。因此不会启动账户刷新/发现、自动迁移、供应商目录预取、签到、
预热、自动重写 Agent、Library、WSL 监听、同步、遥测或应用/Bun/插件更新器。
复制来的旧设置只读取显示偏好和 Agent 模型列表；其他子系统设置不生效。
供应商列表只由内置插件产生，路由组也不参与目录构建。

GUI 请求在通用 mux 前按方法和精确路径准入；通用安装、导入、管理、远程访问、
测试推理、账号发现和设置路由不对外开放。供应商页、Agent 页、显示设置和帮助
保留。页面不装载其他内置插件或第三方页面脚本，也不轮询旧更新源和用量。
独立页面脚本收窄可见控件，服务端检查独立生效。旧 panel 查询会重定向到供应商页；
外部前端开发服务器和 Wails updater helper 模式在启动前拒绝。

删除页面模块时发现两处隐含依赖：共享开关样式仍在 Library CSS 中，所以保留
该静态样式表；Usage 的合计观察器原先仍在核心 app.js 中，已移回 Usage 插件。
这也消除了原核心脚本超额，无须提高体积预算。核心导航只观察实际存在的视图。

`gateway.ServeJellytool`（2026-11-19：从未接入，已删除；Jellytool 用普通网关的 `Front`）只接受已取得的监听器，复用原网关请求处理，但不启动
`WhileServing` 等后台活动。外层只开放 `/v1/models` 的 GET 和三个推理路径的
POST：Chat、Responses、Messages。旧管理、性能分析、MCP、Codex 订阅后端和
非 `/v1` 的别名不开放。原 Host/Origin/请求鉴权守卫仍运行。

## 验证入口

所有测试以临时 HOME、XDG 和固定 Go 缓存运行，使用合成 Key、本机 HTTP 模拟接口，
不启动安装包，不读取或修改真实 Agent 配置，不调用付费推理。

```sh
go test -race -tags nogui,jellytool ./internal/plugin ./internal/gui ./internal/cli ./internal/gateway ./internal/settings -run '^TestJellytool' -count=1
JELLYTOOL_TEST_BROWSER=1 go test -v -tags nogui,jellytool ./internal/gui -run '^TestJellytoolBrowser$' -count=1
```

浏览器测试通过 Playwright 连接真实 Go GUI handler 的临时 HTTP 服务，使用模拟
供应商目录；`NODE_PATH` 需要包含 Playwright，`PLAYWRIGHT_BROWSERS_PATH` 指向
安装了匹配 Chromium/WebKit 的目录。覆盖英文、简体/繁体中文、日语和德语。
浏览器测试无该显式变量时跳过；不能把跳过计作通过。设置 `ARTIFACT_DIR` 可保留
供应商编辑器与窄窗口设置页截图；本轮已检查 WebKit 中文截图，并修复旧 script
标签过滤留下正文 `>` 字符以及被省略样式误显示的 Omarchy 提示。

## 本轮验证记录

本地检查使用 Go 1.26.8、Bun 1.4.2、Playwright Chromium 1234 和 WebKit 2336。
日志在 `/tmp/jellytool-plugin-p1-checks`，本轮改动尚未提交。

- `TestJellytool*` 在 plugin/gui/cli/gateway/settings 五包的 race 检查通过。
- 产品浏览器测试：Chromium/WebKit × 英文、简体中文、繁体中文、日文、德文，
  10 项通过；覆盖供应商选择并保存完整模型 ID、开关保持选集、Key 输入、
  Agent/设置导航、禁用页面跳转、窄窗口、无账号首次打开只显示 Jellytoken，
  以及禁止后台请求与页面异常。
  Key 问题接口在 UI 测试中模拟，不提交 Key；真实 Bun 私有宿主的合成 Key、
  模型目录、请求流和退出登录由 `TestJellytoolActiveAdapter` 验证。
- `TestJellytoolGatewayThroughPrivateHost` 及 race 检查通过：真实动态回环
  HTTP 网关 -> 供应商适配器 -> 随包 Bun 私有宿主 -> 模拟 Jellytoken 接口。
  验证完整 `vendor/literal.v1:exact` ID、网关 token 被替换成模拟插件 Key、
  上游流式响应返回到客户端。测试专用激活入口只存在于 `_test.go`，不进入 App。
  新增集成测试的 Linux/Windows vet 同样通过。
- 原 Usage 页 `usage-sum-whole` 和 `usage-unpriced-sum` 的双引擎检查共
  18 项通过，验证初始化代码移回模块后，金额和未计价数量仍能完整显示。
- 三个负向 overlay 检查均得到行为失败：恢复不完整的 script 标签过滤后，
  页面测试报告 `page includes`（多余的 `>`）；去掉目录验证后，GUI 测试报告
  `untrusted provider save accepted`；去掉复制设置过滤后，设置测试报告
  `copied settings enabled a legacy subsystem`。工作树不受这些临时 overlay 改动。
- macOS 未签名包已构建，Bundle ID、无 URL scheme、Bun SHA256 和插件资源
  与源码一致性检查通过。只运行了 version、禁用命令和拒绝外部前端的启动门禁，
  没有打开实际窗口或启动固定端口的 App。

开发包：
`/Users/ali-090235/.cache/jellytool-build/artifacts/jellytool-plugin-p1-20261103-verified/Jellytool.app`。
它是未签名开发产物，版本输出为 `Jellytool dev`，不是发行包。编译器仍有上游
macOS 枚举转换与 SDK deployment target 警告，不能据此承诺旧 macOS 运行兼容性。

本轮首次全仓检查只剩 `TestCoreScriptBudget` 和 `TestLaneLimitRoutes` 失败：
前者已通过移动 Usage 初始化代码修复；后者原先会读取机器上正在运行的网关，
已在测试中以临时 HTTP 404 服务模拟不存在的 owner，不操作真实服务。
macOS Jellytool vet、Linux/Windows 两种构建模式的 vet、Jellytool
Linux/Windows 交叉构建以及普通 magpie 的 macOS/Linux/Windows 构建均通过。最终 `go test -p 2 -tags nogui ./...` 通过，77 个有测试包成功。
页面最终修正后再次通过产品浏览器 10 项、五包专项 race、两个旧失败用例
（CoreScriptBudget/LaneLimitRoutes）和三个负向检查；随后补充的私有宿主完整
网关集成测试也通过普通/race 运行及 Linux/Windows vet。
`gofmt -l`、`git diff --check`、JS/Python 语法和本文涉及的本地文档链接检查通过。

P0 当时的历史失败记录保留在 [P0 验证记录](jellytool-plugin-p0.md)。没有使用
真实 Jellytoken Key、没有实际写入 Agent，没有测试 Windows/Linux 原生运行、
签名、公证或更新；真实工具共存与恢复仍属于 P3。
