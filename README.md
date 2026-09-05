# OfferLogs 求职追踪系统

按 [OfferLogs-全栈开发计划.md](./OfferLogs-全栈开发计划.md) 实现的全栈求职管理应用：
收藏→投递→面试→Offer→接受/撤回 的完整追踪、类 Notion 数据库视图、桑基分析、私有附件存储。
后端 Go + Gin + pgx/PostgreSQL（对象存储可切换本地磁盘或 SeaweedFS S3），前端 React + TS + Vite + ECharts。

## 目录

```
├── backend/            Go API + worker + admin CLI
│   ├── cmd/{api,worker,admin}
│   ├── internal/       bootstrap · platform · identity · applications · activities · views · files · analytics · transfers
│   ├── db/migrations/  版本化 SQL（内嵌于二进制）
│   └── tests/          Go 集成测试（真实 PostgreSQL）
├── frontend/           React SPA（表格/看板/列表/详情/统计/文件/设置）
├── api/openapi.yaml    契约（错误结构、状态枚举、筛选 DSL 形状）
├── deploy/             compose.yaml + Caddyfile + api.Dockerfile + .env.example
├── docs/               架构说明、运维手册
└── scripts/            dev-postgres.sh · Playwright smoke/e2e
```

## 快速开始（本地开发）

```bash
make dev            # 启动 docker postgres(:5433)
cd backend && go run ./cmd/admin create-user -email you@x.com -password '...'
cd backend && go run ./cmd/api        # :8080（API + 前端静态）
# 浏览器打开 http://localhost:8080
```

## 部署（Docker Compose + Caddy HTTPS）

```bash
cd deploy && cp .env.example .env     # 填 PUBLIC_HOST / CSRF_SECRET / 数据库口令
docker compose up -d --build          # postgres + api + worker + caddy
# 需要自包含 SeaweedFS 时追加：
docker compose --profile seaweedfs up -d
# 初始化账号：
docker compose exec api /app/api-admin create-user -email admin@x.com -password '...'
```

连已有 S3/SeaweedFS：设置 `OBJECTSTORE_PROVIDER=s3` 与 `S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY/S3_BUCKET`。
详见 [docs/runbook.md](./docs/runbook.md) 与 [docs/architecture.md](./docs/architecture.md)。

## 测试与验收

```bash
cd backend && go test ./...                    # 领域规则 + 集成（需 TEST_DATABASE_URL，默认 :5433）
cd frontend && npx vitest run                  # 组件/工具测试
cd frontend && npm run build && npx tsc --noEmit
node scripts/smoke.cjs                         # 冒烟（登录→数据库→抽屉→转换→分析→文件→响应式）
node scripts/e2e.cjs                           # 主流程 E2E：新增→投递→上传→面试→Offer→接受 + 刷新持久化
E2E_BASE=http://localhost:8081 E2E_EMAIL=... E2E_PASSWORD=... node scripts/e2e.cjs  # 对 Compose 部署跑验收
```

## 主要实现要点（对应计划章节）

- **状态机**（§2.2）：11 状态、跳转校验（进入招聘阶段须有投递时间、accepted 须有 Offer 历史、终态重开须原因）；
  每次变更写事件（occurred_at/sequence），纠正保留 corrects_event_id 审计并重算状态；idempotency_key 防重复；
  乐观锁 version（409）。
- **数据库体验**（§3）：表格/看板/列表共用数据与 /views/query；筛选 DSL 服务端校验后参数化（嵌套≤3、条件≤30），
  注入测试通过；保存视图与自定义属性（JSONB）可筛选排序。
- **文件**（§10）：流式上传 → 校验 MIME/大小/SHA-256 → staging 提升 final key → ready；下载校验 owner；
  被引用删除返回 409；worker 清理 pending/staging。
- **统计**（§5）：指标口径（投递 cohort、有效回复率、Offer 率、中位耗时）；桑基 A 三层守恒；桑基 B 事件重建历史路径
  ((step,status) 防环、12 步折叠、导入起点)。
- **导入导出**（§13）：CSV 预检 → 幂等提交；导出带公式注入转义。
- **安全**（§11）：Argon2id、服务端会话、HttpOnly/SameSite cookie、CSRF 双重校验、登录限流、
  结构化错误不泄露内部信息、所有查询按 owner_id 归属。

> 说明：仓库不含真实凭证；`CSRF_SECRET`/数据库口令经 `.env` 注入。
