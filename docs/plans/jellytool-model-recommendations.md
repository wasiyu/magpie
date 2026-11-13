# Jellytool 模型推荐配置

实际使用的配置是 `~/.config/jellytool/model-recommendations.json`（配置了 XDG 或便携目录时，跟随 Jellytool 的数据目录）。首次启动自动创建；更新程序不会覆盖。安装包旁的 `model-recommendations.example.json` 是参考副本，编辑实际配置才会生效。

默认配置预填本轮计算的推荐结果：Astra 使用 `gpt-6-astra`，Luna 使用 `gpt-6-luna`；Sol、Terra 分别使用 `gpt-6.1-sol`、`gpt-6.1-terra`。

DeepSeek 默认推荐为复杂档的 `deepseek-v4-pro-0813` 和日常档的
`deepseek-v4.1-flash-1017`。自动计算时先比较模型版本，同版本优先带 `-MMDD`
日期的型号，再按日期从晚到早选择；没有带日期的型号时使用无日期型号。
配置里明确填写的完整 `id` 会固定到该型号，不会自动换成其他日期。

例如，下面的完整配置只展示四个 GPT 型号：

```json
{
  "version": 1,
  "tiers": {
    "complex": [
      { "id": "gpt-6-astra", "recommended": true, "alwaysShow": true }
    ],
    "daily": [
      { "id": "gpt-6.1-sol", "recommended": true, "alwaysShow": true }
    ],
    "simple": [
      { "id": "gpt-6.1-terra", "recommended": true, "alwaysShow": true },
      { "id": "gpt-6-luna", "recommended": true, "alwaysShow": true }
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
| `name` | 可选的显示名称，不改变请求使用的 ID |
| `recommended` | `true` 显示“★ 推荐”，省略或 `false` 则不标记 |
| `alwaysShow` | `true` 时未开通也显示；省略或 `false` 时仅在接口目录包含该型号时展示 |

在现有文件上调整即可：改 `id` 可换型号，移动条目可改档位，删除条目可从推荐里移除。一个 ID 只能出现一次。把某档设成 `[]` 会隐藏该档卡片；删除整个档位字段会恢复该档的自动计算，同时避免重复展示已在其他档位配置的 ID。比如仅保留 `"tiers": {}`，就会让三个档位都自动计算。

保存后刷新页面或重启 Jellytool。JSON 不支持注释和末尾逗号；写错时界面会显示配置错误，程序不会覆盖或清空你的文件。

配置只决定推荐展示，不赋予权限，也不自动改动供应商选集。没有权限的固定卡片点击时会提示先开通；全选只选接口允许的模型。要实际使用调整后的选集，仍需在页面点击“保存选择”。
