# 生图尺寸能力路由（fork 变更说明）

> 本文档记录 MiloRouter fork 相对上游 sub2api 新增的**生图按尺寸路由**能力。
> 所有改动均带 `// fork: image-size-routing` 标记，便于合并官方代码时定位。
>
> 最后更新：2026-11-04 · 对应版本 `0.2.19-ds.47`

---

## 一、解决什么问题

对外只暴露一个模型名 `gpt-image-2`，客户只传 `size`。但各供应商对 2K / 4K 的卖法不同：

| 供应商类型 | 卖法 | 上游模型名 |
|---|---|---|
| 融合型 | 一个模型出所有尺寸 | `gpt-image-2` |
| 拆名型 | 2K / 4K 单独售卖 | `gpt-image-2-2K`、`gpt-image-2-4K` |
| 原生型 | 4K 是独立产品 | `gpt-image-2-原生4k` |

**改造前的两个问题：**

1. 客户请求 4K 时，系统不知道路由给谁、该用哪个上游名
2. 只支持 1K 的供应商也会接单，返回小图，但页面显示成功（主要投诉来源）

**改造后的行为：** 客户始终只发 `gpt-image-2` + `size`，路由层自动选到支持该尺寸的供应商，并使用其对应的上游模型名。

---

## 二、数据结构

### 存放位置

```
account.Extra["image_size_capabilities"]
```

**为什么不放进 `model_mapping`？**

`model_mapping` 的 key 会被全站至少 4 处代码当作「可售模型清单」遍历：

- `service/admin_group.go`
- `service/gateway_service.go`
- `service/batch_image_public.go`
- `service/gemini_messages_compat_service.go`

往 key 里编码任何标记（例如 `gpt-image-2:4k`）都会让前端模型列表出现伪模型，上游名也可能被当模型名回传、二次加工成非法名。**这是 v1 方案被废弃的直接原因**（见第六节）。

`Extra` 不参与任何模型列表构建，物理隔离。

### 格式

```jsonc
{
  "image_size_capabilities": {
    "gpt-image-2": {
      "1k": "gpt-image-2",
      "2k": "gpt-image-2-2K",
      "4k": "gpt-image-2-4K"
    }
  }
}
```

外层 key 是**对外模型名**，内层是**档位 → 该档位要用的上游模型名**。

也接受数组写法（前端表单更易生成）：

```jsonc
{
  "image_size_capabilities": {
    "gpt-image-2": [
      { "tier": "4k", "upstream": "gpt-image-2-4K" }
    ]
  }
}
```

### 语义（三条，必须理解）

| 配置情况 | 行为 |
|---|---|
| **完全没有这个字段** | 不介入，完全按 `model_mapping` 处理。**存量账号零改动** |
| **有字段，且含该档位** | 支持该尺寸，转发时用配置的上游模型名 |
| **有字段，但不含该档位** | **明确不支持**，调度阶段跳过该账号，不接这单 |

第三条是「不降级」的实现方式：不需要任何特殊标记值，配置里没有该档位就是没有。

---

## 三、请求流程

```
客户请求
  POST /v1/images/generations
  { "model": "gpt-image-2", "size": "2880x2880" }
        │
        ▼
① handler 把 size 写入 context
   service.WithImageRequestSize(ctx, parsed.Size)
        │
        ▼
② 调度阶段判档（复用计费口径）
   ClassifyImageBillingTier("2880x2880") → "4K"
        │
        ▼
③ 账号筛选（12 条调度路径统一入口）
   isModelSupportedByAccountWithContext()
     ├─ 先走原有 model_mapping 判断
     └─ 再查 image_size_capabilities 是否有 "4k"
          ├─ 有   → 通过
          └─ 没有 → 判定不支持，跳过该账号
        │
        ▼
④ 转发阶段解析上游名
   model_mapping 得到基础名 → 能力表覆盖为 "gpt-image-2-4K"
        │
        ▼
⑤ 发给上游
   { "model": "gpt-image-2-4K", "size": "2880x2880" }
```

### 关键设计：为什么在 `isModelSupportedByAccountWithContext` 入口拦截

该函数被 **14 处调度路径**调用（粘性会话、窗口成本、常规筛选等）。在入口统一拦截可以保证：

- 所有分支行为一致
- 将来上游新增调度路径时不会漏掉
- 不需要改 14 个调用点

---

## 四、档位判定规则

复用 `service.ClassifyImageBillingTier`，与**计费口径完全一致**。

```
长边 ≤ 1024  → 1K
长边 ≤ 2048  → 2K
其余         → 4K

特殊值：
  "1k" / "2k" / "4k" 直接识别
  "2048x1152"         → 2K
  "3840x2160"         → 4K
  "auto" / ""         → 无法判档，不介入
```

**为什么必须复用而非另写：** 如果路由和计费各写一套阈值，早晚出现「路由按 4K 选账号、计费按 2K 收钱」的账目错位。

### 前端档位与 size 的对应

| 前端选项 | 实际发送 size |
|---|---|
| 1K | `1024x1024` |
| 2K | `2048x2048` |
| 4K | `2880x2880` |

注意 4K 不是 4096，是 `2880x2880`（长边 2880 > 2048，判为 4K）。

---

## 五、后台配置方式

**位置：** 账号编辑弹窗 → 「生图尺寸能力（可选）」

**仅 openai 平台显示。** 其他平台不写入该字段，避免影响。

### 操作

- **加一档**：点「添加尺寸映射」，填「对外模型 / 尺寸档位 / 上游模型名」
- **停用某档**：删掉那一行 → 该账号不再接这个尺寸的单
- **整块清空**：全部删掉 → 账号回到原始行为

「对外模型」输入框带候选提示，候选取自当前账号 `model_mapping` 里的生图模型，避免手打错。

### 数据写入路径

```
EditAccountModal.vue
  buildImageSizeCapabilities()        // 行 → 嵌套结构
  → updatePayload.extra
  → PUT /api/v1/admin/accounts/:id
  → account.Extra = normalizedExtra
  → repo.SetExtra(extra)
  → PostgreSQL accounts.extra (jsonb)
```

---

## 六、代码改动清单

### 新增文件

| 文件 | 内容 | 行数 |
|---|---|---|
| `backend/internal/service/image_size_routing.go` | 能力表解析、档位判定、上游名解析、context 传递 | ~200 |
| `backend/internal/service/image_size_routing_test.go` | 8 个测试用例 | ~180 |

### 修改文件

| 文件 | 改动 | 行数 |
|---|---|---|
| `backend/internal/service/gateway_scheduling.go` | `isModelSupportedByAccountWithContext` 入口加尺寸检查 | +14 |
| `backend/internal/handler/openai_images.go` | 把 `parsed.Size` 写入 context | +4 |
| `backend/internal/service/openai_images.go` | 转发时用能力表的上游名覆盖 | +9 |
| `frontend/src/components/account/EditAccountModal.vue` | 配置区 UI + 读写逻辑 | ~180 |
| `frontend/src/components/account/__tests__/EditAccountModal.imageSizeCapability.spec.ts` | 4 个测试用例 | ~180 |

### 关联改动（工作台）

**这部分不是路由功能本身，但必须一起改，否则 2K 永远按 4K 处理。**

| 文件 | 改动 |
|---|---|
| `frontend/src/features/imageStudio/lib/modelRegistry.ts` | 所有档位统一发基础模型名，不再按档位切换上游模型 |
| `frontend/src/features/imageStudio/lib/__tests__/modelRegistry.spec.ts` | 更新断言 + 新增不变量测试 |

---

## 七、v1 方案为什么被废弃（重要教训）

**v1 做法：** 把档位编码进 `model_mapping` 的 key

```jsonc
{
  "gpt-image-2":    "gpt-image-2",
  "gpt-image-2:4k": "gpt-image-2-4K"     // ← 档位作为 key 的一部分
}
```

**两个致命缺陷：**

1. **污染模型列表**
   `model_mapping` 的 key 被全站 4 处代码当作可售模型遍历，`gpt-image-2:4k` 直接出现在前端模型列表里，显示成「GPT Image 2:4k」这种伪模型。

2. **双重后缀**
   上游模型名 `gpt-image-2-4K` 被当成模型名回传后，被再次追加档位后缀，变成 `gpt-image-2-4K:4k`，上游必然 400。

**教训：** 任何共享数据结构在被多处代码消费时，不能承载额外的隐式语义。档位信息应该走独立通道。

---

## 八、常见问题

### Q1：客户直接请求 `gpt-image-2-4K` 还能用吗？

**能。** 这条路径完全不受影响，走原有 `model_mapping`。

本功能只影响「请求 `gpt-image-2` + 传 size」这一条路径。两条路并存。

### Q2：不配这个字段会怎样？

行为与未安装此功能完全一致。存量账号零迁移。

### Q3：配了 1K / 2K 但没配 4K，客户请求 4K 会怎样？

**该账号被跳过**，调度会去找其他配了 4K 的账号。

如果所有账号都没有 4K → 返回 `no available accounts supporting model`（404/503）。**失败发生在发请求之前，不产生上游费用。**

### Q4：`size=auto` 会怎样？

无法判档，不介入。走原有 `model_mapping`。

### Q5：文本模型会受影响吗？

**不会。** 尺寸检查只在 `openai_images.go` 的路径上注入 context，文本请求的 context 里没有 size，检查直接跳过。

### Q6：上游返回的图尺寸不对怎么办？

**不做校验。** 中转站的成本模型是「上游扣费不可退」，收到图就算成功。尺寸是否达标由配置决定——配错了是配置问题，不在程序里兜底。

### Q7：渠道映射和尺寸能力谁优先？

渠道映射优先。`ForwardImages` 里如果 `channelMappedModel` 显式改写了模型名，以渠道为准。

---

## 九、合并官方代码注意事项

所有改动带 `// fork: image-size-routing` 标记。

```bash
# 查看所有 fork 标记位置
grep -rn "fork: image-size-routing" backend/internal frontend/src

# 合并上游后检查是否有冲突残留
git diff upstream/main -- backend/internal/service/gateway_scheduling.go
```

### 冲突风险

| 位置 | 风险 | 说明 |
|---|---|---|
| `image_size_routing.go` | 无 | 全新文件 |
| `gateway_scheduling.go` | 中 | `isModelSupportedByAccountWithContext` 入口插入，上游若改此函数需手工合并 |
| `openai_images.go`（handler） | 低 | 单点插入 |
| `openai_images.go`（service） | 低 | 单点插入 |
| `EditAccountModal.vue` | 高 | 文件很大（6500+ 行），上游改动频繁 |

### 合并后必跑

```bash
cd backend && go build ./... && go test ./internal/service/ ./internal/handler/
cd frontend && npx vue-tsc --noEmit && npx vitest run
```

---

## 十、版本演进

| 版本 | 变更 |
|---|---|
| `ds.40` | v1 方案上线（编码档位进 model_mapping） |
| `ds.41` | v1 回滚，改为独立能力表 `image_size_capabilities` |
| `ds.42` | 账号编辑弹窗加配置区（用原生 input/select） |
| `ds.43` | 改用组件库 Select，修掉 v-for index key 导致的数据串位 |
| `ds.44` | 修工作台：所有档位统一发基础模型名 |
| `ds.45` | 修复保存被 `props.account.extra` 覆盖导致配置丢失 |
| `ds.47` | 清理调试代码，稳定版 |

---

## 十一、验证方法

### 确认配置已写入

```sql
SELECT id, name, extra->'image_size_capabilities' AS caps
FROM accounts
WHERE deleted_at IS NULL AND platform = 'openai' AND extra ? 'image_size_capabilities';
```

### 确认路由生效

```sql
SELECT created_at, model, image_size, account_id, total_cost
FROM usage_logs
WHERE model LIKE 'gpt-image%'
ORDER BY created_at DESC LIMIT 10;
```

关键看「上游模型」列是否变成配置的名字（如 `gpt-image-2-4K`），以及 `image_size` 是否随请求变化（2K / 4K）。

### 单元测试

```bash
cd backend
go test ./internal/service/ -run "TestImageSizeRouting" -v
```

覆盖：存量账号零影响、三档映射、只支持 1K 的账号拒绝 2K/4K、能力表未提及的模型不介入、数组写法、`model_mapping` 不被污染、context 往返。
