# 生图尺寸档位：采用官方调度实现

最后更新：2026-11-05。

本分支采用上游 [PR #8462](https://github.com/Wei-Shaw/sub2api/pull/8462)，对应合并提交 `6f92090d7f540efd88cd4b81e651120cb36e3ff5`。引入该 PR 的完整改动，不包含同时间其他上游提交。

原 fork 的 `image_size_capabilities` 尺寸到模型名覆盖逻辑及专属配置界面已移除。生图工作台继续使用公开基础模型名加 `size`，由官方调度器选择账号，再使用账号已有的普通模型映射。

## 配置方式

对于不同尺寸使用不同型号的供应商，按档位创建账号记录，凭据和供应商地址可相同，放在同一目标分组：

| 账号 | 生图尺寸档位 | 普通模型映射 |
|---|---|---|
| 供应商 A · 1K | 仅 1K | `gpt-image-2 → gpt-image-2` |
| 供应商 A · 2K | 仅 2K | `gpt-image-2 → gpt-image-2-2K` |
| 供应商 A · 4K | 仅 4K | `gpt-image-2 → gpt-image-2-4K` |

各账号公开的模型映射 key 都是 `gpt-image-2`，客户使用一个模型名。上游型号按供应商实际名称填写，仍遵守网关原有的端点和模型名称兼容规则。

如果供应商原本就通过参数区分分辨率，可以保留一个账号，尺寸档位全选，沿用原有模型映射。普通账号模型映射负责最终名称，本功能不会额外追加或改写型号后缀。

官方配置存储为：

```json
{
  "openai_image_size_tiers": ["2K"]
}
```

这是账号 `extra` 中的字段。缺省、空数组或三档齐全均表示不限档位。配置对整个账号生效，不是某一个模型的专属限制。创建、编辑和批量编辑均使用上游界面；批量全选通过写入空数组解除旧限制。

## 请求行为

- 显式可识别的尺寸：只选择允许该档位的账号，换号重试也遵守档位限制。
- `auto`、未传尺寸或无法识别的尺寸：不排除账号，依次优先不限档位、允许 4K、允许 2K、仅 1K 的账号；已有粘性绑定按上游规则保留。
- 整组所有合格账号均明确不接该档位时返回 `400 invalid_request_error`；暂时限流、冷却等仍按原有可用性逻辑处理。
- 档位判断复用 `ClassifyImageBillingTier`：长边不超过 1024 为 1K，不超过 2048 为 2K，更大为 4K；`2880x2880` 属于 4K。
- 计费规则沿用网关原有实现；配置档位不保证上游一定返回指定像素尺寸。

## 覆盖范围

官方配置面向 OpenAI 平台账号，包括 API Key、OAuth、Setup Token，并继续遵守各账号和端点原有的能力限制。

已接入：

- `/v1/images/generations`、`/v1/images/edits`；
- 复用同一 Images handler 的异步图片任务；
- HTTP `/v1/responses` 中显式声明 `image_generation` 工具的请求，读取工具的 `size`，缺省再取顶层 `size`。

未接入：Responses WebSocket、Codex 被动注入的生图工具，以及 Gemini / Grok 原生链路。没有为这些入口新增自研覆盖逻辑。

## 从旧 fork 配置迁移

**上线前需要转换已配置 `image_size_capabilities` 的账号。** 旧字段可以继续留在数据库中作对照，但新实现不会读取它；本次代码整合不自动修改或复制线上账号。

1. 保存旧账号的公开模型、每档上游名、分组、凭据、并发和限额配置。
2. 为实际需要拆分的档位创建账号，将旧的每档上游名写进对应账号的普通 `model_mapping`，只允许对应尺寸。
3. 原基础账号如果仍在分组内，需移除被拆分的公开生图模型，或限制为它实际负责的档位。若它继续不限档位，2K / 4K 请求仍可能被分配给它。
4. 同一真实 API Key 的多个账号在网关内分别计算并发和配额。按上游总限制分配这些数值，不要把原账号的完整额度和并发机械复制到每一档。
5. 对有多个生图模型的账号，逐项检查每个档位的普通模型映射。避免在复制时把同一批文本模型也重复计入调度容量。
6. 用显式 1K / 2K / 4K 请求验证选中的账号和实际 `upstream_model`，再单独检查 `auto` 请求。确认稳定后，可按需要清理旧字段。

尺寸表不能仅重命名成 `openai_image_size_tiers`：旧表包含上游型号，官方列表只表达账号允许接收的档位。

## 验证

找出仍需迁移的旧配置（只读）：

```sql
SELECT id, name, extra->'image_size_capabilities' AS legacy_size_mapping,
       credentials->'model_mapping' AS model_mapping,
       extra->'openai_image_size_tiers' AS allowed_tiers
FROM accounts
WHERE deleted_at IS NULL AND platform = 'openai'
  AND extra ? 'image_size_capabilities';
```

检查转发记录：

```sql
SELECT created_at, requested_model, model, upstream_model,
       image_size, account_id, total_cost
FROM usage_logs
WHERE requested_model LIKE 'gpt-image%' OR model LIKE 'gpt-image%'
ORDER BY created_at DESC LIMIT 20;
```

上游回归测试覆盖高级调度、两种旧调度、粘性绑定、重试、缺档错误、未指定尺寸的偏好以及调度快照。工作台测试另外验证所有分辨率使用统一公开模型名和 `size`。
