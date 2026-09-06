# OfferLog 求职追踪系统

全栈求职管理应用：收藏 → 投递 → 面试 → Offer → 接受/撤回 的完整追踪，类 Notion 数据库视图、桑基分析、私有附件存储。
后端 Go + Gin + pgx/PostgreSQL，前端 React + TypeScript + Vite + ECharts。

## 快速开始

前置：安装 Docker（含 docker compose）即可，无需本机 Go / Node 环境。

```bash
make up
```

构建并启动 postgres + api + worker，自动创建首个账号。完成后打开
<http://localhost:8080>，默认登录 `me@example.com / testpass12345`
（可用 `LOCAL_ADMIN_EMAIL` / `LOCAL_ADMIN_PASSWORD` 覆盖）。

```bash
make down
```

停止并移除所有容器数据：容器、网络、数据库与附件卷，全部清空，不可恢复。

说明：

- 数据保存在 Docker 命名卷中，`make up` 反复重启不丢数据；只有 `make down` 会清空。
- postgres 同时暴露在 `127.0.0.1:55432`，便于本机 psql / DBeaver 连接排查。

## 目录

```
├── backend/            Go API + worker + admin CLI
├── frontend/           React SPA（表格/看板/列表/详情/统计/文件/设置）
├── api/openapi.yaml    API 契约
├── deploy/             Docker Compose 部署（本地栈：compose.local.yaml）
├── docs/               架构说明、运维手册
└── scripts/            冒烟 / E2E 验收脚本
```

架构与生产部署见 [docs/architecture.md](./docs/architecture.md)、[docs/runbook.md](./docs/runbook.md)；
完整设计见 [OfferLog-全栈开发计划.md](./OfferLog-全栈开发计划.md)。

> 仓库不含真实凭证；`CSRF_SECRET` / 数据库口令等经 `.env` 或环境变量注入。