# Jellytool P3：WorkBuddy 开发包与人工验收

日期：2026-11-04（Asia/Shanghai）

工作树：`/Users/ali-090235/.codex/worktrees/jellytool-plugin/magpie`

分支：`codex/jellytool-plugin`，基点 `1223856f`，改动未提交。

## 范围与实现

用户授权本轮完成 WorkBuddy 的连接、选模、断开、恢复和已有 magpie 配置共存。
其他 Agent 由用户人工验收。本轮所有文件写入与接口检查都在临时 HOME/XDG 中进行，
没有使用真实 Key、付费推理、真实 WorkBuddy 配置或服务器；也未启动实际 App 窗口。

- Jellytool 仍只组合四个本机适配器，不构造 WSL 或第三方插件 Agent。
- WorkBuddy 复用原 `ConnectHow`、`provider` 字段、`ListsModels`、Agent 页面及 API。
  写入的模型 ID 保持完整，地址为 `127.0.0.1:3435`，请求 Key 为 `jellytool-workbuddy`。
- WorkBuddy 模型条目使用独立 `vendor: jellytool`。已有 `vendor: magpie` 条目与
  URL/Key 保留，不能作为 Jellytool 已连接的依据。状态读取不创建备份、不接管旧配置。若所选完整 ID 与其它供应商现有条目冲突，
  返回错误并保留原文件，不制造重复模型或把失败的连接标为成功。
- 供应商保存只保存选集，不直接写任何 Agent 文件。用户在 Agent 页面修改 WorkBuddy
  模型列表时，只显式同步这个已连接的适配器。也可以断开、重连以应用新的供应商选集。
  实际任务的模型仍在 WorkBuddy 自己的选择器里选择。
- 第一次写入前，原始文件字节存入 Jellytool 私有 stash，备份键按配置路径区分。
  持久化失败就停止写入。空文件与不存在的文件有独立状态。
- 断开只移除 Jellytool 条目；若剩余 JSON 与原始数据相同，恢复原始字节格式。
  若用户、WorkBuddy 或 magpie 在连接期间改过其它条目，保留这些变更。原文件不存在且
  未出现其它数据时，删除生成的文件。恢复失败保留备份，支持重试。
- 选择空集后仍保留断开入口；预览在子进程副本中执行真实断开，不改变源文件或 stash。
- 普通 magpie 的 WorkBuddy / CodeBuddy 行为不启用上述产品分支。

## 官网图标

使用 Jellytoken 官网链接的 256×256 方形 PNG，透明度保留，未生成或重画标识。
应用 ICNS/Dock、原生窗口、页面品牌、favicon 和 Jellytoken 供应商入口统一使用该资源。
源码与来源见 [图标来源](../../build/jellytool/ICON-SOURCE.md)。打包时离线转换 ICNS，
不下载图标、不回退到 magpie 图标；大尺寸槽位由官网原图放大。

## 验证

日志目录：`/tmp/jellytool-plugin-p3-checks`。

```sh
go test -tags nogui,jellytool ./internal/agent ./internal/gui -run '^TestJellytool' -count=1
go test -race -tags nogui,jellytool ./internal/agent ./internal/gui -run '^TestJellytool' -count=1
go test -tags nogui ./internal/agent -run 'TestWorkBuddy|TestCodeBuddy|TestConnectMagpieEmpty' -count=1
go vet -tags jellytool ./...
GOOS=linux CGO_ENABLED=0 go vet -tags nogui,jellytool ./...
GOOS=windows CGO_ENABLED=0 go vet -tags jellytool ./...
go test -p 2 -tags nogui ./... -timeout 30m
JELLYTOOL_TEST_BROWSER=1 go test -tags nogui,jellytool ./internal/gui -run '^TestJellytoolBrowser$' -count=1
```

定向验证包含旧 magpie 条目并存、Agent API 连接与选模、重复连接/断开、空集、空文件、
缺失文件、对象格式、连接期间新增用户模型、备份失败、恢复失败重试、子进程预览。
图标与原 P2 供应商草稿在 Chromium / WebKit × 五种语言下共 20 项浏览器检查通过，
并检查了截图。

定向 race、普通 WorkBuddy / CodeBuddy 回归、原生及 Linux/Windows 全仓 vet 均通过。
普通构建的 `go test -p 2 -tags nogui ./... -timeout 30m` 全部通过，含 77 个有测试的包。
Apple Silicon macOS 包和 Linux nogui / Windows 交叉构建通过。最终 macOS 包已核对
Bundle ID、ICNS、插件资源字节和 Bun SHA-256；临时 HOME 下的版本查询、禁用 CLI
与外部前端拒绝检查通过，未启动 App 窗口或创建应用配置。

两项 overlay 故障注入得到实际行为 FAIL（未改工作树源文件）：

1. 把 Jellytool 写入 vendor 改成 magpie：共存测试发现旧模型被替换。
2. 恢复前删除快照：恢复失败测试发现备份被提前消耗。

原生编译仍有上游 macOS enum/SDK deployment-target 警告，不据此宣称旧版 macOS 已验收。

## 本地开发包

`/Users/ali-090235/.cache/jellytool-build/artifacts/jellytool-plugin-p3-20261104-verified/Jellytool.app`

Apple Silicon macOS 开发包，尚未签名或公证；Bundle ID 为
`com.jellytoken.jellytool.dev`，Bun 固定为 1.4.2。包旁提供 `build-manifest.json`
记录二进制、图标和源码摘要，及 `人工验收.md`。不覆盖先前 P2 包。
另提供 `Jellytool-P3-macOS-arm64.zip`，内容为同一份 `Jellytool.app`。

## 留给人工的验收

1. 先退出 WorkBuddy；首轮退出 magpie。备份现有 WorkBuddy `models.json` 和 magpie
   配置目录。可先设置独立绝对路径的 `WORKBUDDY_CONFIG_DIR` 做隔离检查。
2. 启动这个 P3 包，保存 Jellytoken Key/文本模型，进入 Agent 页连接 WorkBuddy。
3. 重新打开 WorkBuddy，检查所选完整模型 ID；在 WorkBuddy 中选模型发一条简短消息。
4. 在 Jellytool Agent 页修改 WorkBuddy 模型列表，重开 WorkBuddy 检查列表更新。
5. 从 Jellytool 预览断开，再断开。无其它变更时原文件应字节一致；连接期间新增的用户
   条目应保留。原来不存在的文件仅在没有其它条目时删除。
6. 重复连接/断开，并恢复启动 magpie，确认原接入可用。最后再观察两个应用同时运行时
   的文件变更；本轮模拟检查不等于已验证运行中的两套进程无竞争。

真实模型协议、工具调用兼容、真实平台窗口/生命周期、Windows/Linux 实机、Bun 许可证
清单、签名、公证、安装/卸载与更新回滚仍需相应人工验收。服务器操作仍遵守 AGENTS.md
的 Server Change Approval，不把本地打包或测试授权扩大为部署授权。
