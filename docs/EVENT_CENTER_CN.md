# 活动中心

活动中心用于维护团队活动，并向已登录用户提供列表、详情和地图视图。活动数据与 AI 网关的用户、订阅分组隔离；活动中心关闭时，用户接口统一返回 404，管理员接口仍可维护数据。

## 数据边界

- `events` 保存活动内容、发布状态、可见范围和人工维护标记。
- `event_occurrences` 保存一个活动的一个或多个场次。坐标统一以 WGS84 存储。
- `event_categories` 保存可配置分类及地图标记颜色。
- `event_sources` 描述人工录入、JSON 和未来爬虫来源。
- `event_source_records` 保存来源记录、external ID、指纹和内容哈希，用于幂等同步。
- `event_import_batches` 与 `event_import_items` 保存 JSON 预检和提交过程，支持审计和失败重试分析。

迁移采用前向兼容方式：基础表在 `182_event_center_foundation.sql`，幂等指纹索引和 audience JSONB 索引在 `183_event_center_hardening.sql`。不要修改已执行的迁移文件。

## 管理接口

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET/POST` | `/api/v1/admin/events` | 分页列表、新建 |
| `GET/PUT/DELETE` | `/api/v1/admin/events/:id` | 详情、编辑、软删除 |
| `POST` | `/api/v1/admin/events/:id/publish` | 发布 |
| `POST` | `/api/v1/admin/events/:id/cancel` | 取消并记录原因 |
| `POST` | `/api/v1/admin/events/:id/archive` | 归档 |
| `GET/POST/PUT/DELETE` | `/api/v1/admin/event-categories` | 分类管理 |
| `GET/POST/PUT/DELETE` | `/api/v1/admin/event-sources` | 来源管理 |
| `GET/PUT` | `/api/v1/admin/event-settings` | 活动开关和高德地图配置 |
| `POST` | `/api/v1/admin/event-imports/preview` | JSON 预检，最大 5 MiB、1000 条 |
| `GET` | `/api/v1/admin/event-imports/:id` | 查询预检批次 |
| `POST` | `/api/v1/admin/event-imports/:id/commit` | 提交新增或更新 |

所有管理员变更请求经过既有 AdminAuth 和 AuditLog 中间件。用户端只有认证后的只读接口：`/events`、`/events/map`、`/events/categories` 和 `/events/:id`。

## JSON 导入

导入文件的顶层格式固定为：

```json
{
  "type": "sub2api-events",
  "version": 1,
  "source": "json",
  "mode": "upsert",
  "defaults": {
    "timezone": "Asia/Shanghai",
    "coordinate_system": "wgs84",
    "country": "中国",
    "province": "上海市",
    "city": "上海"
  },
  "events": []
}
```

单条活动至少需要 `title` 和一个 `occurrences`。活动可提供 `external_id`；没有 external ID 时，系统使用 `source_id + fingerprint`（标题、首场开始时间、城市、地址）去重。内容哈希相同会标记为 `unchanged`，内容变化在 `upsert` 模式下标记为 `update`。同一文件中的重复 external ID 或指纹会在预检阶段标记为冲突。

导入分两阶段执行：预检只写入批次和明细，不修改活动；提交通过状态抢占保证同一批次只能提交一次。人工编辑会写入 `manual_override_fields`，后续来源同步不会覆盖该活动的来源管理字段。

## 可见范围与地图

- `authenticated`：所有已登录用户可见。
- `targeted`：活动的 `audience.subscription_group_ids` 与用户当前有效订阅分组有交集时可见。
- `visible_from` / `visible_until` 控制展示时间窗口。
- 用户列表和地图查询在数据库层先过滤可见范围、时间、城市和 bbox，再做分页/数量限制，避免目标分组活动占满候选窗口。
- 高德地图只在前端加载；管理员在“活动配置”填写 Web Key 和安全密钥。高德返回的 GCJ-02 坐标在写入前转换为 WGS84，展示时再转换回 GCJ-02。
- 用户筛选活动后，地图自动缩放到当前活动点范围；分类颜色同时用于列表和地图标记，近距离活动点由 `MarkerCluster` 聚合。点击列表活动会定位并高亮地图点，点击地图信息卡可进入完整详情。
- 只有包含有效经纬度的线下或混合场次才生成地图点。没有坐标的活动保留在列表并明确提示“暂无地图位置”，系统不会根据地点文本伪造默认坐标。

## 新增来源类型

当前内置 `manual` 和 `json`，数据库已保留 `crawler` 类型。后续接入活动行等公共来源时，建议新增独立同步任务：抓取原始内容写入 `event_source_records.raw_payload`，适配器只负责映射为 `EventImportCandidate`，继续复用现有预检、指纹、内容哈希、人工覆盖和审计流程。不要让爬虫直接写 `events`。
