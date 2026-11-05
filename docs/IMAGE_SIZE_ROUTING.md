# 按生图尺寸覆盖上游模型名

最后更新：2026-11-05。本文说明当前 Images API 的实际行为与已知限制。

## 用途与配置

客户使用统一模型名，例如 `gpt-image-2`，通过 `size` 选择尺寸。对于把尺寸拆成不同产品名的供应商，可以在账号的 `extra.image_size_capabilities` 中添加需要覆盖的档位：

```json
{
  "image_size_capabilities": {
    "gpt-image-2": {
      "2k": "gpt-image-2-2K",
      "4k": "gpt-image-2-4K"
    }
  }
}
```

这是**可选的模型名覆盖配置**，不承担供应商尺寸能力白名单的作用。当前 APIKey Images 转发行为如下：

| 情况 | 行为 |
|---|---|
| 当前模型和尺寸档位均命中有效配置 | 用配置值替换最终上游模型名 |
| 没有配置当前档位 | 沿用原有渠道、账号模型映射结果 |
| 没有配置当前模型，或整个表为空 | 沿用原有模型映射结果 |
| `size` 为 `auto`、未指定或无法判档 | 沿用原有模型映射结果 |

只需配置要覆盖的档位。删除一行表示取消这项覆盖；清空所有行表示恢复原有映射。配置不会新增公开模型，也不会自动为账号模型白名单增加允许项。

配置与 `credentials.model_mapping` 分开保存，避免把尺寸标记作为公开模型名发布。后台位置是 OpenAI 平台、API Key 类型账号的新增和编辑弹窗中的「按生图尺寸映射模型（可选）」。两个入口共用同一套编辑、格式兼容及校验逻辑；下文的尺寸覆盖只在 APIKey Images 转发分支生效。

## 支持的请求入口

当前接入点是 OpenAI 兼容 Images API：

- `POST /v1/images/generations`
- `POST /v1/images/edits`，包括 JSON 和 multipart 请求

handler 将解析后的 `size` 放入请求 context，APIKey 转发分支读取它。OAuth / SetupToken Images 转发分支未接入这项覆盖；Responses、Chat Completions、Gemini 原生 API、批量生图等其他入口也不能据此认定已支持。工作台通过上述 Images API 请求时使用相同逻辑。

## 模型名的实际处理顺序

```text
客户的 model
  → composite 解析后的模型名（如果使用复合分组）
  → 入口模型名称类别校验
  → 渠道映射后的 requestModel
  → requestModel 名称类别校验
  → 账号 model_mapping(requestModel)，得到基础 upstreamModel
  → 用 requestModel + size 查尺寸表，命中时覆盖 upstreamModel
  → 最终 upstreamModel 名称类别校验
  → 改写请求的 model 并发往上游
```

**当前查表 key 是渠道映射后的 `requestModel`，不是账号映射后的基础上游名。** 没有 composite 或渠道改名时，它通常就是界面中填写的「对外模型」。如果渠道将 `gpt-image-2` 改成另一个名字，以 `gpt-image-2` 为 key 的尺寸配置将不会命中。这项后端限制尚未修改，不能把原始对外名在所有映射组合中均可命中当作保证。

尺寸表的 **value 是最终上游模型名**。命中后直接覆盖账号映射结果，不会再经过一次 `model_mapping`。例如，尺寸值写成 `gpt-image-2-4K`，就会直接发送这个名字；不要期望账号映射再把该值转换成别名的目标。

账号白名单与别名映射还承担独立职责：调度按模型映射的 key 判断账号是否接收请求，转发按对应 value 改写名称。非空账号模型映射通常也构成可接收模型的白名单；普通 APIKey 账号没有映射时一般允许所有模型。尺寸覆盖不替代这些判断。

## 档位与请求参数

后端复用 `ClassifyImageBillingTier` 判断输入尺寸：

| 输入 | 档位 |
|---|---|
| `1k` / `2k` / `4k` | 对应档位，忽略大小写与首尾空白 |
| 有效像素尺寸，长边不超过 1024 | 1K |
| 有效像素尺寸，长边大于 1024 且不超过 2048 | 2K |
| 更大的有效像素尺寸 | 4K |
| `auto`、空值或无法解析的尺寸 | 不进行尺寸覆盖 |

例如 `2048x2048` 属于 2K，`2880x2880` 和 `3840x2160` 属于 4K。工作台正方形预设为 1K=`1024x1024`、2K=`2048x2048`、4K=`2880x2880`；其他比例使用对应像素尺寸。

这项功能只覆盖模型名，**不转换 `size` 参数**。例如 `size=2k` 虽可用于判档，仍会原样发送给供应商，供应商是否接受取决于其接口。计费可能优先使用实际输出尺寸；复用输入分类函数不代表程序保证上游一定返回所请求的分辨率。

## 格式兼容与表单保存

后端也接受每个模型下的数组写法：

```json
{
  "image_size_capabilities": {
    "gpt-image-2": [
      { "tier": "4k", "upstream": "gpt-image-2-4K" }
    ]
  }
}
```

当前编辑表单可以读取对象或数组写法，保存时统一写成「模型 → 档位 → 上游名」的嵌套对象。表单要求每条规则完整、档位为 1K / 2K / 4K，并且同一模型的同一档位只能配置一次；未完成的规则需要补全或删除。清空配置时删除该 extra 字段，并保留账号的其他 extra 设置。

## 当前名称限制

APIKey Images 路径会在入口、渠道映射后以及最终上游模型名确定后三次调用名称类别校验。当前认可的名称包括：

- 以 `gpt-image-` 开头的名称；
- `grok-imagine`、`grok-imagine-edit` 或以 `grok-imagine-image` 开头的名称；
- 以 `gemini-` 开头，且以 `-image` 结尾或包含 `-image-` 的名称。

这是网关现有的名称判断规则，不是精确的官方型号清单，也不代表任意供应商名称都可用。例如 `gpt-image-2-custom-4K` 能通过类别判断，`vendor-image-4k` 则会被本地拒绝。仅添加账号或渠道别名不能使任意对外名称通过入口校验；composite 在入口校验前已解析为可识别模型名的情况另当别论。

OAuth 路径还会根据已接入的明确型号选择 Codex Images 直调或 Responses 图片工具请求，协议构造与 APIKey 兼容上游不同。不能据此把当前前缀规则说成所有第三方供应商都必须遵守的协议要求。

## 调度与解析器的现状

实际 Images 调度走 `OpenAIGatewayService.SelectAccountWithSchedulerForImages`，高级和旧调度分支都未接入尺寸表过滤。因此，未配置某档位不会让该账号在 Images 调度中被排除。

后端仍保留较早的另一套语义：`ResolveImageSizeUpstream` 在当前模型缺少请求档位时返回 `supported=false`，`GatewayService.isModelSupportedByAccountWithContext` 会据此过滤账号。但这不是上述 Images 请求实际使用的调度链；APIKey Images 转发当前只在 `supported=true` 且上游名非空时覆盖，否则保留基础映射。

所以，本文的“缺档位回原逻辑”描述的是当前 Images API 的实际行为与产品约定，**不表示该 helper 返回值、旧注释或其他调度路径已经完成统一修改**。后续维护应明确区分这两者，避免误将旧过滤逻辑接入 Images 后改变约定。

## 常见问题

**只填 4K，1K / 2K 请求会怎样？** 沿用原有映射；不需要为所有档位补齐规则。

**渠道映射和尺寸覆盖谁优先？** 依次执行渠道映射、账号映射、尺寸覆盖。尺寸表若按渠道后的请求名命中，会覆盖前两者的上游结果；原始对外名因渠道改名未命中时则没有覆盖。

**客户直接请求带尺寸后缀的模型名呢？** 仍按正常模型准入和映射处理；只有尺寸表恰好配置了该请求名并命中档位时才覆盖，没有针对 `gpt-image-2` 的硬编码特例。

**它会保证账号支持该尺寸或检查返回图尺寸吗？** 不会。当前功能是模型名覆盖；账号实际能力、上游尺寸错误与输出质量仍需单独确认。

## 验证与相关代码

查看已保存配置：

```sql
SELECT id, name, extra->'image_size_capabilities' AS overrides
FROM accounts
WHERE deleted_at IS NULL AND platform = 'openai'
  AND extra ? 'image_size_capabilities';
```

检查实际转发记录时应包含上游模型名：

```sql
SELECT created_at, requested_model, model, upstream_model,
       image_size, account_id, total_cost
FROM usage_logs
WHERE requested_model LIKE 'gpt-image%' OR model LIKE 'gpt-image%'
ORDER BY created_at DESC LIMIT 10;
```

`backend/internal/service/image_size_routing_test.go` 验证配置解析、判档与 context；这些 helper 单元测试不等于已覆盖真实调度和 HTTP 转发行为。前端配置与工作台测试分别位于 `EditAccountModal.imageSizeCapability.spec.ts`、`modelRegistry.spec.ts`。

主要代码位置：

- `backend/internal/handler/openai_images.go`：解析入口、写入 size context、Images 调度调用。
- `backend/internal/service/openai_images.go`：APIKey 模型映射、尺寸覆盖、名称校验与请求改写。
- `backend/internal/service/image_size_routing.go`：配置解析及档位 helper。
- `backend/internal/service/openai_account_scheduler.go`、`openai_gateway_scheduling.go`：实际 OpenAI 调度链。
- `frontend/src/components/account/CreateAccountModal.vue`、`EditAccountModal.vue`：创建与编辑时写入尺寸配置。
- `frontend/src/components/account/ImageSizeCapabilityEditor.vue`、`frontend/src/composables/useImageSizeCapabilities.ts`：共用编辑界面、格式兼容及校验。
- `frontend/src/features/imageStudio/lib/modelRegistry.ts`：工作台对外请求模型名。
