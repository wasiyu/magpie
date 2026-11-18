# Jellytool：状态栏与供应商引导

日期：2026-11-09（Asia/Shanghai）。工作树 `jellytool-plugin/magpie`，
分支 `codex/jellytool-plugin`。用户反馈：没有状态栏图标，缺少说明，特别是供应商页。

## 行为

- Jellytoken 官网 PNG 同时用于状态栏，菜单提供打开、版本和退出，随显示语言更新。
- 关闭窗口后继续转发，图标菜单和 Dock 可重新打开；退出才停止网关和私有插件。
- 供应商页根据已保存状态提示添加 Key、选模型并保存、连接 WorkBuddy。
  操作按钮复用原流程；说明展开状态在刷新后保留。
- 编辑器说明登录用于添加/更换 Key，保存与取消只影响模型草稿。
  无已选模型时明确提示选择模型；推荐为空时提供直达全部模型按钮。
- `internal/gui/assets/jellytool-guide.md` 讲解 Key、文本模型、保存、WorkBuddy 接入、
  断开恢复、magpie 共存、常见问题和状态栏。打包到 Resources 和应用旁的《使用说明.md》。

## 验证与交付

日志位于 `/private/tmp/jellytool-guidance-stage/logs`。测试全部使用临时 HOME/XDG，
不操作真实 Key、WorkBuddy 或 magpie 文件。原生状态栏测试创建隔离的 AppKit 进程。
浏览器通过临时 Go handler 和模拟模型验证，覆盖两种引擎和五种语言。

验证结果：

- 原生 `TestJellytoolTrayKeepsWindowAvailable` 通过：真实状态栏按钮、关闭隐藏、重新打开。
- overlay 移除托盘创建后，原生测试以 `no native status bar button` 实际失败；源文件未改动。
- Chromium / WebKit × 英文、简体、繁体、日文、德文，共 20 项产品浏览器测试通过。
  修正了展开说明时底栏遮挡开关，以及说明插入编辑器时的网格列错位；最终截图已检查。
- Jellytool GUI 定向测试通过；全仓普通 nogui 测试含 77 个有测试的包，全部通过。
- 原生、Linux、Windows 产品模式全仓 vet 通过；Linux/Windows 交叉构建通过。
- 四项翻译键检查、Go 格式、JS/Python 语法和文档链接检查通过。
- macOS 新包已核对身份、Bun 和插件资源摘要、说明文件及隔离 HOME 下的版本输出。
  上游 macOS SDK / deprecated API 编译警告仍存在；不宣称旧系统已验收。

交付位置：`/Users/ali-090235/Downloads/Jellytool-P3-20261109-guided/Jellytool.app`，
应用旁有《使用说明.md》和构建清单；完整目录另提供同名 ZIP。仍为未签名、未公证的
Apple Silicon 开发包。新包独立存放，不覆盖正在验收的旧包，也不自动退出当前窗口。
真实 WorkBuddy 行为继续由用户人工验收。代码未提交。
