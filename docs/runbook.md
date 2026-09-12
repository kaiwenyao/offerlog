# OfferLog 运维手册

## 本地开发
```bash
scripts/dev-postgres.sh            # docker postgres on :5433
cd backend && go run ./cmd/admin create-user -email you@x.com -password '...'
cd backend && go run ./cmd/api     # :8080，静态由 frontend/dist 提供
cd frontend && npm run dev         # 可选：vite dev 代理 /api
```

> Vite 代理下写操作（POST/PATCH/DELETE）被 CSRF Origin 校验拦 403 的解法：
> vite.config.ts 的 /api 代理已默认把转发请求的 Origin 改写成后端目标（生产不受影响）；
> 若直接连外部 API 主机跑 dev，可在后端设 `DEV_ALLOWED_ORIGINS=http://localhost:5173`
> （仅开发环境设置；生产不设该变量，行为不变）。

## 部署（单机 Docker Compose，自包含 SeaweedFS）
```bash
cd deploy
cp .env.example .env   # 填入域名 / CSRF_SECRET / 数据库口令
docker compose up -d            # postgres + api + worker + caddy (本地对象存储)
docker compose --profile seaweedfs up -d   # 需要时一并启动 SeaweedFS
```

## 账号与注册
- 默认闭门：`REGISTRATION_OPEN` 未开启时登录页只显示登录，账号用 CLI 建
  （无固定默认密码）：`docker compose exec api /app/api-admin create-user -email admin@x.com -password '...'`
- 开放注册：`.env` 设 `REGISTRATION_OPEN=true` 重启 api，登录页出现「注册」标签，
  自助开号即签会话；密码规则 8-128 位，邮箱唯一。公开配置端点
  `GET /api/v1/auth/config` 返回 `{"registration_open": bool}` 供前端渲染。
- 本地部署（compose.local）`make up` 会自动建一个测试账号并打印账号密码
  （`.env` 的 `SEED_USER_*`，`SEED_USER=0` 关闭），同时默认开放注册；测试栈
  （compose.test）不 seed 初始账号，smoke/e2e 每次注册一次性账号（唯一邮箱，重跑免清库）。
- `api-admin create-user -if-not-exists` 在邮箱已注册时打印提示并退出 0、不改密码，
  供 `make up` 这类需要在保留数据卷上反复执行的场景使用。

连已有 SeaweedFS/S3：设 `OBJECTSTORE_PROVIDER=s3` 与 `S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY/S3_BUCKET`，
不启动 seaweedfs profile；bucket 禁止匿名读写，凭证只进后端。

## 健康与任务
- `/health/live` 进程存活；`/health/ready` 检查 DB + schema。
- worker 自动清理过期 pending/staging（宽限 24h）；任务按 kind 分发，未知类型标记失败
  （不误报成功）；租约过期（进程崩溃）后可由其它副本重新领取，配合幂等键不重复执行。
  每日提醒任务（逾期待办 / 面试前一天 / 投递满 N 天未回复）由 worker 生成站内通知。
- 查看失败任务：`docker compose exec api /app/api-admin jobs`。

## 备份与恢复（RPO≤24h, RTO≤2h 目标，需演练）
维护窗口方案：暂停写入与 worker → 同批次备份：
1. `pg_dump -Fc` PostgreSQL（含 schema_migrations）。
2. 备份对象卷（objects 或 SeaweedFS filer/master/volume 持久化目录 + 元数据）。
3. 保存清单（schema 版本、对象摘要 SHA-256、application 计数）。
恢复演练（每月）：从空环境起 compose → 恢复 pg_dump → 恢复对象 → 校验：
申请数量、随机附件摘要往返、附件关联、状态事件、看板结果与桑基守恒。

## 回滚
- 应用回滚：`docker compose up -d --no-deps <上一镜像>`。
- 数据库变更 expand/contract：先扩容部署，后收缩清理；破坏性迁移必须先备份。
