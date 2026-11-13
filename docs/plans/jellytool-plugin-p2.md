# Jellytool P2：供应商选模与推荐

当前展示分工（2026-11-10）：三类推荐只在“快速上手 → 模型选择”展开；供应商页
保留完整模型目录，不再展示推荐分类或推荐/全部/已选切页。快速上手的“从全部模型中选择”
把尚未保存的推荐选集带入供应商编辑器，在那里补选更多模型并统一保存。下文保留 P2
早期实现与验收记录；当前交互以此说明和 `gui-shell.md` 为准。


日期：2026-11-03（Asia/Shanghai）

工作树：`/Users/ali-090235/.codex/worktrees/jellytool-plugin/magpie`

分支：`codex/jellytool-plugin`，基线 HEAD：`1223856f`；P0/P1/P2 尚未提交。

## 实现与边界

P2 使用方案允许的**受控宿主扩展**，没有启用第三方插件面板、iframe、插件市场
或新的写入 RPC。原 `renderEditor`、`draft`、脏状态/放弃确认、保存按钮和
`POST /api/provider/save` 继续负责编辑与持久化；产品脚本只替换编辑器中的
选模区域。原 Agent 页面与保存路径保留。

- 推荐、全部模型、已选三个视图共用一个供应商草稿；不预勾、不自动保存。
- 推荐分类追加保留已有选择，按完整模型 ID 去重；不同分类中的同一 ID 只选一次。
- 完整目录支持 ID 搜索，每批显示 60 项；没有推荐也能手动选择。
- 每个模型可展开目录输出类型及未知能力说明。推荐另有理由、限制、依据、
  维护日期和维护者；所有内容作为文本呈现。
- 未授权推荐不进入可选项；非 TEXT 目录项展示但禁选。失效的已选 ID 保留在
  草稿中供移除，保存拒绝失效 ID、图片和视频 ID。空选择明确保存为不提供模型，
  不再隐式退回整个供应商目录。
- 替换 Key 前清空旧目录；目录读取失败或插件回退列表不能证明当前 Key 权限。
  私有宿主按顺序处理目录读取、Key 替换与退出登录，缓存读取不等待网络。
- 保存供应商选择不写 Agent 文件、不调用推理接口。实际 Agent 应用、协议与
  工具兼容性仍属于原页面及 P3 验收。

## 推荐数据

[内置数据](../../internal/plugin/jellytoken/recommendations.json) 随版本发布，
参与离线资源校验和私有插件目录代际计算。Go 的
[`JellytoolRecommendations`](../../internal/plugin/jellytool_recommendations.go)
只读编译时内嵌资源，不读用户可编辑副本、不访问在线推荐服务。

格式为 `version: 1` 和 `recommendations` 数组。每条记录必须包含完整 `id`、
`category`、`reason`、`constraints`、`evidence`、ISO 日期 `updated` 和
`maintainer`。分类仅接受 coding / reasoning / writing / research / cost /
speed / tools。同一分类内不能重复 ID。推荐内容只代表特定用途建议，不能推导
目录授权、价格、速度、发布日期、上下文、协议或工具兼容性。

当前推荐规则见 [GUI shell](../subsystems/gui-shell.md)：GPT-6 Astra/Luna 和 GPT-6.1 Sol/Terra 默认固定展示并标记推荐，
Astra 放复杂档，Sol 放日常档，Terra/Luna 都放简单档；没有目录权限时点击显示开通权限提示，
不加入选集。其他系列按当前 Key 的文本目录推荐，每档每系列一个代表。MiniMax 从推荐中移除，
已选和完整目录仍可使用。推荐更新不改变已保存选择。

## 只读接口与保存契约

`GET /api/jellytool/models` 由 Jellytool 准入层提供，仅返回当前已登录、没有
目录错误的私有插件目录的 `id` / `outputs`、内置推荐数组和 `catalogError`。
它不拉取上游、不写磁盘，也没有任何 Key、端点或工具配置字段。

刷新仍走原 `POST /api/provider/models`。保存仍走原
`POST /api/provider/save`，以服务端当前私有插件目录校验完整 ID，忽略端点、
代理、回退和任意模型元数据。编辑器持有旧快照时，用户必须移除失效选择后重试，
不会自动换成其他型号。目录是最近一次成功读取的权限快照，不承诺即时获知远端
在刷新之间改变的权限；真实请求仍由上游鉴权。

## 验证记录

本轮全部使用临时 HOME/XDG、固定 Go 缓存、随包 Bun 1.4.2、本机模拟接口。
没有使用真实 Key、启动真实桌面应用、写真实 Agent 配置、发送付费请求或操作服务器。

验证脚本与日志位于 `/tmp/jellytool-plugin-p2-checks`。

- Jellytool 模拟接口、准入、选集边界、资源完整性与实际 Bun 私有宿主测试通过。
  `TestJellytoolSelectionBoundary` 用临时 Codex 配置哨兵验证供应商保存不写 Agent。
- Chromium / WebKit × en / zh / zh-TW / ja / de：**20 项通过**，包括跨分类追加、
  去重、原保存/放弃、无匹配推荐、目录读取失败、失效项移除、126 项目录分批与
  精确搜索、键盘焦点和推荐依据按文本呈现。模拟推荐带 MOCK 标识。
- 全局翻译键、占位符与三种语言页面检查：**13 项通过**。
- Jellytool plugin/gui/cli/gateway/settings 的 `TestJellytool*` race 检查通过。
- 原生 vet、Linux nogui / Windows vet，两种产品构建均通过。
- 普通 magpie Mac / Linux / Windows 构建通过；Jellytool Linux nogui / Windows
  交叉构建及 Mac 本地开发包构建通过。交叉构建不代表对应平台运行验收。
- Go overlay 负向变异产生实际行为 FAIL：放开非文本保存、恢复空选择隐式全目录、
  取消推荐依据必填、跳过推荐资源完整性检查。工作树源文件没有被负向变异改写。
  移除 fallback 清空分支未使实际宿主测试失败，因为当前插件没有默认模型；该实验
  不计入负向验证通过数。错误目录的实际宿主检查和保存准入另有独立断言。
- `go test -p 2 -timeout 45m -tags nogui ./...`：**通过，77 个测试包**。
  产品分支的专项检查另用 `nogui,jellytool` 标签运行。

已检查英文 Chromium、中文 WebKit 的推荐详情截图。开发包路径：
`/Users/ali-090235/.cache/jellytool-build/artifacts/jellytool-plugin-p2-20261103/Jellytool.app`。
未签名，开发 Bundle ID `com.jellytoken.jellytool.dev`。已核对插件与推荐资源字节、
Bun 版本与 SHA-256、独立包标识、没有 URL scheme，以及临时 HOME 下的 `version`；
没有启动用户环境中的实际 GUI。

格式、JS 语法、文档链接和 `git diff --check` 通过。未运行整套上游浏览器测试；
本轮扩展只加载于 Jellytool，已运行全部 Jellytool 浏览器用例和受影响的全局语言检查。
Mac 编译仍有上游枚举类型转换和 SDK 部署目标警告。

复现入口：

```sh
go test -v -tags nogui,jellytool ./internal/plugin ./internal/gui -run TestJellytool -count=1
# 浏览器：设置 JELLYTOOL_TEST_BROWSER=1，并配置 Playwright 的 NODE_PATH / 浏览器目录
```

上面的 Go 命令也必须使用 P1 记录中的临时 HOME 与固定缓存环境，避免读取用户配置。
浏览器入口是 `TestJellytoolBrowser`，它启动临时 Go HTTP handler，运行
[jellytool.test.cjs](../../internal/gui/tests/jellytool.test.cjs) 与
[jellytool-recommendations.test.cjs](../../internal/gui/tests/jellytool-recommendations.test.cjs)。

## 待 P3 验证

真实平台、真实 Key、四类工具的接入/生效/共存/恢复、签名与公证、正式推荐内容和
随包 Bun 的许可证清单仍未验收。开发包不是正式发布；交叉编译与模拟通过不能替代
Mac/Windows 上的真实工具验证。
