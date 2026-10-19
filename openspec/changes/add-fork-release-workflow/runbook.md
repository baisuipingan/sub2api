# 二开发布 Runbook（方案稿）

> 对应脚本和 workflow 尚未实现。本文件用于固定目标操作顺序，不能直接当成已经上线的命令。

## 1. 查看官方是否有新版

```bash
./scripts/fork-release.sh status
```

确认输出中的：

- 当前集成分支和 commit。
- 已合入的 `upstream_version`。
- GitHub 最新正式 Release 和 Release Notes。
- 落后提交、变更文件和新迁移。

快捷只读检查也可以直接访问：

```text
https://github.com/Wei-Shaw/sub2api/releases/latest
```

## 2. 准备合并官方版本

工作区必须干净。当前进行中的计费改造应先提交到自己的分支，不能带着未提交文件做发布迁移。

```bash
./scripts/fork-release.sh prepare v0.2.19
```

脚本会创建 `sync/v0.2.19` 分支并执行：

```bash
git merge --no-ff v0.2.19
```

冲突必须人工解决。重点审查：

```bash
git diff --stat origin/main...HEAD
git diff origin/main...HEAD -- \
  backend/migrations \
  backend/internal/service \
  backend/internal/handler \
  backend/internal/repository \
  frontend/src
```

数据库规则：

- 只新增表、列、索引和可空字段通常允许一个周期的应用回退。
- `DROP`、`RENAME`、收紧约束或修改既有语义必须递增 `schema_epoch` 并标记 `database.rollback=backup-required`。
- 模型定价、余额、积分和订单相关迁移必须在数据库副本演练，记录迁移前后数据对账。
- 二开迁移使用 `dsapi_001_*`、`dsapi_002_*`，官方数字编号继续保留给上游。生产库当前只应用到官方 `245`，尚未执行 fork 的 `246/247`，首次发布前先完成更名。

## 3. 测试

按改动范围运行完整检查。至少包含：

```bash
cd backend
go test ./...

cd ../frontend
pnpm test:run
pnpm run type-check
pnpm run build
```

若本轮含迁移，还要在数据库副本执行迁移和“上一版本二进制 + 新 schema”的 smoke test。测试通过后再把维护分支合回 `main`。

## 4. 创建二开 Release tag

回到干净、已更新的 `main`：

```bash
git switch main
./scripts/fork-release.sh publish v0.2.19
```

脚本应输出候选版本，例如：

```text
fork-v0.2.19-1 -> 0.2.19-ds.1
```

先检查 tag 指向的提交：

```bash
git show --stat fork-v0.2.19-1
```

确认后显式推送：

```bash
git push origin main
git push origin fork-v0.2.19-1
```

GitHub Actions 校验并构建：

```text
ghcr.io/baisuipingan/sub2api:0.2.19-ds.1
ghcr.io/baisuipingan/sub2api@sha256:<digest>
```

发布记录必须包含：

- `release_version`
- `upstream_version`
- `upstream_commit`
- `source_commit`
- 镜像 tag 和 digest
- 最大数据库迁移
- `schema_epoch`
- `database.rollback=image-only|backup-required`
- 测试结果

任何 tag 已存在、官方 tag 不是祖先、VERSION 不一致或测试失败，都终止发布，不复用旧 tag。

## 5. 部署到服务器

首次切换先确认服务器 Compose overlay 和服务端脚本已经安装。生产只接受 digest：

```bash
/root/sub2api-deploy/bin/sub2api-release deploy \
  ghcr.io/baisuipingan/sub2api@sha256:<digest>
```

脚本内部必须使用：

```bash
docker compose \
  -f /root/sub2api-deploy/docker-compose.yml \
  -f /root/sub2api-deploy/docker-compose.fork.yml \
  --env-file /root/sub2api-deploy/.env \
  up -d --no-deps --force-recreate sub2api
```

它会依次完成：

1. 加发布锁。
2. 检查当前服务健康。
3. 备份 PostgreSQL 并验证 dump。
4. 拉取新 digest。
5. 原子更新 `.env` 中的 `SUB2API_IMAGE`。
6. 只重建应用容器。
7. 等待稳定健康状态和版本号。
8. 写入 current/previous 与备份路径。

发布后人工检查：

```bash
/root/sub2api-deploy/bin/sub2api-release status

docker inspect --format '{{.Config.Image}}' sub2api
docker exec sub2api /app/sub2api --version
curl -fsS http://127.0.0.1:8080/health
```

再检查后台登录、公开设置、API Key 鉴权、计费展示和模型网关请求。只看 `/health` 不足以判断业务完整。

## 6. 回退

回到上一成功部署：

```bash
/root/sub2api-deploy/bin/sub2api-release rollback
```

脚本读取 previous digest，重新执行备份、切换、健康检查和状态记录。它不会自动把数据库也恢复到旧时间点。

显式回退到更早版本：

```bash
/root/sub2api-deploy/bin/sub2api-release rollback \
  --release 0.2.17-ds.1 \
  --digest sha256:<digest>
```

如果目标版本的 `schema_epoch` 与当前不同，或目标版本是 `backup-required`，脚本只打印对应备份路径并拒绝静默切换。数据库恢复需要独立的维护操作和人工确认，因为会丢失备份时间点之后的新数据。

## 7. 发布失败处理

### 容器起不来或健康检查失败

1. 保存 `docker compose logs --since=15m sub2api`。
2. 查看本次部署前的数据库迁移和备份记录。
3. manifest 为 `image-only` 且没有不可逆迁移时，让脚本恢复 previous 镜像。
4. manifest 为 `backup-required` 或迁移已部分执行时，停止反复重建，先判断 schema 状态。

### 新版本启动后业务异常

不要先删除新镜像或覆盖 tag。先把异常版本、digest、日志和复现请求保存下来，再执行显式回退，保证问题可以复盘。

### GitHub 发布新版后左上角仍不消警

检查：

```bash
docker exec sub2api /app/sub2api --version
```

并确认 API 返回的 `upstream_version` 是中国版基线而非旧缓存。新镜像必须重新构建并注入新基线；只修改 `.env` 不会有作用。

## 8. 首次生产切换记录

2026-10-19 只读检查到的官方运行基线：

```text
image: weishaw/sub2api:latest
program: 0.2.17
commit: 060d0160417782cd962a0c7f460f6cd4565d4916
digest: sha256:44b59fc74b0b582750ef5f38146782db177767ef9d3f34351f1c75c157b1bb88
platform: linux/amd64
port: 127.0.0.1:8080
health: ok
```

首次部署二开镜像前，把上述官方 digest 记录为 previous。切换完成并验证后，删除生产 Compose 对 `latest` 的隐式依赖。
