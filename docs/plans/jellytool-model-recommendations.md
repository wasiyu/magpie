# Jellytool 模型推荐配置

实际使用的配置是 `~/.config/jellytool/model-recommendations.json`（配置了 XDG 或便携目录时，跟随 Jellytool 的数据目录）。首次启动自动创建。安装新版时，能读取的旧版 `version: 1` 配置会被新版推荐配置替换，原文件另存为 `model-recommendations.json.v1.bak`；`version: 2` 配置和写错的 JSON 都会保留。安装包旁的 `model-recommendations.example.json` 是参考副本，编辑实际配置才会生效。

默认配置里四个 GPT 条目都带 `latest`：Astra、Sol、Terra、Luna 各自显示当前 Key 目录里版本最高的型号（按数字逐段比较，`gpt-6.10-sol` 高于 `gpt-6.9-sol`）。目录里一个都没有时（没有 GPT 权限），显示 `id` 写的 `gpt-6.2-astra`、`gpt-6.2-sol`、`gpt-6.1-terra`、`gpt-6-luna`，点击时提示先开通权限。

DeepSeek 默认推荐为复杂档的 `deepseek-v4-pro-0813` 和日常档的
`deepseek-v4.1-flash-1017`。自动计算时先比较模型版本，同版本优先带 `-MMDD`
日期的型号，再按日期从晚到早选择；没有带日期的型号时使用无日期型号。
配置里明确填写的完整 `id` 会固定到该型号，不会自动换成其他日期。

例如，下面的完整配置只展示四个 GPT 型号，各自跟随最高版本：

```json
{
  "version": 2,
  "tiers": {
    "complex": [
      { "latest": "astra", "id": "gpt-6.2-astra", "recommended": true, "alwaysShow": true }
    ],
    "daily": [
      { "latest": "sol", "id": "gpt-6.2-sol", "recommended": true, "alwaysShow": true }
    ],
    "simple": [
      { "latest": "terra", "id": "gpt-6.1-terra", "recommended": true, "alwaysShow": true },
      { "latest": "luna", "id": "gpt-6-luna", "recommended": true, "alwaysShow": true }
    ]
  }
}
```

| 字段 | 含义 |
| --- | --- |
| `tiers.complex` | 解决复杂问题，数组顺序就是显示顺序 |
| `tiers.daily` | 日常工作与编码 |
| `tiers.simple` | 简单任务 |
| `id` | 完整模型 ID，必须使用接口里的拼写 |
| `latest` | 可选，`astra`、`sol`、`terra` 或 `luna`。填写后显示目录里该 GPT 型号的最高版本，`id` 必须是同一型号（如 `gpt-6.2-sol`），只在目录里一个都没有时显示；同一型号只能写一次。不填则固定为 `id` |
| `name` | 可选的显示名称，不改变请求使用的 ID |
| `recommended` | `true` 显示“★ 推荐”，省略或 `false` 则不标记 |
| `alwaysShow` | `true` 时未开通也显示；省略或 `false` 时仅在接口目录包含该型号时展示 |

在现有文件上调整即可：改 `id` 可换型号，移动条目可改档位，删除条目可从推荐里移除。一个 ID 只能出现一次。把某档设成 `[]` 会隐藏该档卡片；删除整个档位字段会恢复该档的自动计算，同时避免重复展示已在其他档位配置的 ID。比如仅保留 `"tiers": {}`，就会让三个档位都自动计算。

保存后刷新页面或重启 Jellytool。JSON 不支持注释和末尾逗号；写错时界面会显示配置错误，程序不会覆盖或清空你的文件。

配置只决定推荐展示，不赋予权限，也不自动改动供应商选集。没有权限的固定卡片点击时会提示先开通；全选只选接口允许的模型。要实际使用调整后的选集，仍需在页面点击“保存选择”。
