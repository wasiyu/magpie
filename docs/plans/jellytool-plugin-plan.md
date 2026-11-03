# Jellytool 插件化独立应用方案

版本：v0.1 讨论稿
日期：2026-11-03（Asia/Shanghai）
分支：`codex/jellytool-plugin`
基线：`main` / `3142a555`

本文把 Jellytool 调整为“内置 Jellytoken 插件的独立应用分支”。它是产品和
技术方案讨论稿，不代表已经完成插件接线，也不授权真实 Key、真实 Agent 配置、
付费推理、签名发布或服务器操作。

## 1. 方案结论

Jellytool 不要求用户先安装 magpie，再单独寻找和安装 Jellytoken 插件。插件在
Jellytool 打包时随应用提供，首次启动时登记到 Jellytool 的私有插件目录；用户
只看到一个独立的 `Jellytool.app`。

```text
Jellytool.app
├── magpie 原有桌面壳、供应商页和 Agent 页
├── Jellytoken 内置供应商插件
│   ├── Key 鉴权
│   ├── Jellytoken 模型目录
│   ├── Chat / Responses / Anthropic 请求
│   └── 推荐数据与模型说明
├── Jellytool 产品边界
│   ├── 只展示 Jellytoken
│   ├── 关闭无关页面和入口
│   ├── 阻断 API / CLI 绕过
│   └── 停止无关后台任务
└── 独立应用身份、配置目录、端口、插件目录和更新来源
```

这不是把 magpie 全部重写成 Jellytoken，也不是只替换应用名称。正确的产品
定义是：**Jellytool 是 magpie 的独立品牌精简分支，内置一个受信任的 Jellytoken
供应商插件，Agent 配置继续复用 magpie。**

## 2. 为什么采用这个方向

用户最需要的是降低 Key、模型和 Agent 配置的门槛，同时希望尽快得到可测试的
程序。插件化能直接复用 magpie 已有的供应商模型体系、Agent 页面、配置预览、
漂移检查和恢复机制，减少重复实现。

它也保留独立应用的产品边界：Jellytool 可以有自己的名称、包标识、数据路径、
端口和发布包，用户不需要面对 magpie 的完整功能集合。

## 3. 与 magpie 插件机制的对应关系

当前 magpie 插件机制已经提供这些扩展点：

| 能力 | 当前插件机制 | Jellytool 使用方式 |
| --- | --- | --- |
| Key 登录 | `auth.methods` 支持 API Key 方法 | 固定 Jellytoken 供应商，只显示 Key 输入 |
| 模型目录 | provider `models` hook | 请求 Jellytoken `/v1/models`，保留完整模型 ID |
| 请求转发 | provider plugin `fetch` | 通过 Jellytoken 插件处理协议和上游请求 |
| 供应商信息 | provider 名称、图标、模型、鉴权状态 | 预设 Jellytoken 名称、图标和固定地址 |
| 供应商补充面板 | `magpie.panels` 的 `provider` slot | 展示推荐、模型说明、授权和验证提示 |
| Agent 补充入口 | `agents` slot 或 agent action | 只在确有必要时提供跳转，不另造 Agent 页面 |
| 独立插件页面 | `magpie.page` | 仅用于帮助或推荐详情，不替代原供应商/Agent 页面 |

插件页面当前能读取的宿主数据是受限的，例如 `providers:list` 和
`usage:summary`。供应商和 Agent 的实际保存仍由宿主页面及宿主 API 负责。
因此推荐面板不能自行修改 Agent 配置；它只能把用户引导到宿主选模器，或由
后续增加一个经过审查的最小桥接接口完成勾选。

## 4. 自动安装的正确实现

“插件自动安装”有两种含义，产品上需要采用第一种：

### 4.1 随包内置，推荐

- 构建 Jellytool 时把 Jellytoken 插件包、依赖和版本锁定文件放入应用资源。
- 首次启动时复制或登记到 Jellytool 的私有插件目录。
- 不访问 npm，不从 GitHub 下载，不显示插件市场，不要求用户执行安装命令。
- 插件只允许来自 Jellytool 的内置资源，并校验版本和文件完整性。
- 插件升级跟随 Jellytool 应用版本，不单独运行通用插件更新器。

这能满足“用户打开就是已安装”的体验，也避免首次启动网络、Bun 版本或插件
仓库不可用导致接入失败。

### 4.2 首次启动联网安装，不推荐

首次启动时调用 Bun/npm 下载插件，会增加网络失败、供应链、版本漂移和权限问题，
也会让 Jellytool 仍然依赖插件市场。它不适合作为首轮低门槛内测方案。

## 5. 独立 App 需要改什么

只改显示名称不够。以下身份必须独立：

| 身份项 | Jellytool 处理 |
| --- | --- |
| 显示名称和图标 | 显示 `Jellytool`；图标可先沿用上游资源，正式发布再替换 |
| Bundle ID / 包标识 | 使用 Jellytool 自己的开发和正式标识 |
| 配置和缓存目录 | 使用 Jellytool 独立目录，不自动读取或迁移 magpie 可变数据 |
| 单实例锁 | Jellytool 自己的锁，不能结束或接管 magpie |
| 本地端口 | 使用 Jellytool 独立端口，默认只监听回环地址 |
| 插件目录和插件状态 | 使用 Jellytool 私有目录，不能共用 magpie 的插件列表和 Key |
| URL scheme / deeplink | 不继承 magpie scheme，除非 Jellytool 后续明确需要 |
| 更新来源 | 首轮手动更新，不连接 magpie 更新地址 |
| 许可证和第三方声明 | 保留 MIT 及第三方声明 |

Go module、上游内部包名和许可证不需要全局改名。这样能减少无意义的重命名，
也方便后续同步上游修复。

## 6. 功能关闭规则

Jellytool 可以保留 magpie 源码，但功能开关必须覆盖三层：

| 层级 | 必须做到 |
| --- | --- |
| 页面层 | 只展示 Jellytoken、Agent、设置与帮助；关闭其他供应商、插件市场、用量、会话等无关页面 |
| API / CLI 层 | 关闭其他供应商添加、账号发现、远程控制、插件安装、旧管理 API 和绕过 UI 的入口 |
| 后台层 | 不启动其他供应商刷新、跨供应商回退、路由组、预热、签到、云同步、旧更新器等活动 |

上游代码可以保留，但隐藏菜单不代表功能已经关闭。Jellytool 需要在入口和后台
任务处使用产品边界判断，避免通过旧 CLI、深链接或内部 API 绕过界面限制。

目标工具自身的模型、登录、插件和无关配置仍然保留。Jellytoken-only 表示
Jellytool 提供一个供应商，不表示删除工具原本的其他来源。

## 7. 用户流程

```text
打开 Jellytool
  -> Jellytoken 供应商页
  -> 输入 Key
  -> 获取当前 Key 有权限的模型
  -> 推荐 / 全部模型 / 已选中挑选供应商模型
  -> 保存供应商选用集合
  -> 进入 magpie 原 Agent 页
  -> 为 Codex / Claude Code / OpenCode / WorkBuddy 分别选择模型
  -> 预览配置变化并确认
  -> 按工具要求重启、新建会话或在工具内选择
  -> 用户主动测试并查看状态
```

供应商选用集合和 Agent 模型子集是两个层次：

- 保存供应商模型不会自动修改任何 Agent。
- 新增供应商模型不会自动加入已有 Agent。
- Agent 的默认模型、子代理模型和专项设置继续属于 Agent。
- 已授权但兼容性未验证的模型，用户明确确认风险后才可应用。
- 已知不兼容模型禁用并说明原因。

## 8. 推荐功能

推荐随 Jellytoken 供应商插件提供，但不绕过授权、协议和工具兼容性判断。
推荐页面或供应商面板只产生供应商选模草稿，不直接修改 Agent。

当前候选分类为：

- 日常编码
- 复杂推理
- 写作与表达
- 长文与资料整理
- 成本优先
- 响应优先
- 工具调用与自动化

每条推荐需要有准确模型 ID、理由、适用限制、依据和维护日期。无法证明价格、
速度、发布时间或工具兼容性的内容显示为未知，不用模型名称猜测。

首期建议把人工审定的推荐清单随 Jellytool 版本发布，不新增在线推荐服务或
管理后台。具体模型名单、推荐依据和维护责任仍需产品确认。

## 9. 实施拆分

### P0：插件契约验证

建立最小 Jellytoken 插件包，只用模拟接口验证：

- API Key 登录方法是否能进入宿主供应商页；
- 模型 hook 是否能正确解析 `TEXT / IMAGE / VIDEO` 和业务错误；
- fetch 是否能走 Chat、Responses、Anthropic 所需路径；
- 插件资源能否随包登记到独立 Jellytool 插件目录；
- 第三方插件继续被拒绝，只有内置插件被允许。

### P1：宿主产品边界

- Jellytool 名称、Bundle ID、数据路径、端口、插件目录和单实例隔离；
- 只显示 Jellytoken 和原 Agent 页面；
- 关闭无关 UI、API/CLI 和后台任务；
- 不进入 magpie 的通用插件市场和自动更新流程。

### P2：供应商页和推荐联动

- 复用原供应商模型编辑器；
- 将推荐作为供应商面板或宿主受控扩展；
- 保存仍由宿主供应商逻辑负责；
- 明确是否需要最小“推荐加入供应商选集”桥接接口。

### P3：真实工具和平台验证

- Apple Silicon Mac 和 Windows x64；
- Codex 桌面/CLI、Claude Code、OpenCode、WorkBuddy；
- 真实 Key、真实工具配置和小样本请求需要单独确认目标、影响和费用；
- WSL、SSH、容器、Intel Mac、Windows ARM64 和 TRAE 自动配置不纳入首轮承诺。

## 10. 主要风险和处理

| 风险 | 处理 |
| --- | --- |
| 插件执行需要 Bun，和独立 App 的运行环境不一致 | P0 先验证随包 Bun/插件运行；不把“包里有插件”当成已经可执行 |
| 插件面板不能直接改宿主 Agent 配置 | Agent 保存继续由宿主负责，必要时增加最小受控桥接 |
| 只关闭页面，后台仍运行旧功能 | 为 API/CLI、初始化和后台任务增加统一 Jellytool 边界 |
| magpie 插件状态和 Jellytool 状态混用 | 独立插件目录、`plugins.json`、认证文件和数据根 |
| 上游更新改变插件契约 | 锁定宿主版本，按版本运行插件契约测试 |
| 推荐内容缺乏真实依据 | 允许空态和未知值，未审定内容不得进入发布包 |

## 11. 当前不做的事

- 不把 Jellytool 变成通用多供应商管理器。
- 不让用户在首次启动时从网络安装任意插件。
- 不重做 magpie Agent 页面。
- 不创建独立公共模型池或导入流程。
- 不把推荐直接写入所有 Agent。
- 不因为插件方案而自动接管用户现有 Agent 配置。
- 不把合成测试、交叉编译或静态页面测试称为真实工具验收。

## 12. 需要确认的产品决定

实现前请确认以下三点：

1. 接受“插件随 Jellytool 安装包内置，首次启动不联网安装”的方式。
2. 接受 Agent 页面继续使用 magpie 原页面，推荐只修改供应商选用集合。
3. 接受 Jellytool 只允许内置 Jellytoken 插件运行，其他第三方插件和插件市场
   在首期关闭。

这三点确认后，下一步先完成 P0 插件契约验证，再决定是否把当前独立 Jellytool
服务中的供应商目录、凭据和请求逻辑迁移到插件，避免先做不可逆的大规模重构。

## 13. 参考材料

- [magpie 插件运行时](../subsystems/plugin-runtime.md)
- [供应商与插件所有权](../subsystems/provider-plugins.md)
- [GUI 页面与插件页面](../subsystems/gui-shell.md)
- Jellytool 需求确认稿：在原 Jellytool 工作树的 `docs/plans/jellytool-v1-spec.md`，本稿已重新整理插件路线
- Jellytool 页面与交互设计：在原 Jellytool 工作树的 `docs/plans/jellytool-provider-first-interaction.md`，本稿保留 Agent 复用原则
- Jellytool 实施前技术收敛：在原 Jellytool 工作树的 `docs/plans/jellytool-implementation-review.md`，本稿改用插件化执行顺序
