# OfferLog 求职追踪系统

全栈求职管理应用：给每个岗位自由添加时间线事件（投递 / 初筛 / OA / 面试 / Offer / 任意自定义），时间线按发生时间自动排序、岗位状态随之推导——不是每个岗位都有 OA 或初筛，流程由你决定。另有类 Notion 数据库视图、跨岗位日历与站内提醒、桑基分析、私有附件存储。
后端 Go + Gin + pgx/PostgreSQL，前端 React + TypeScript + Vite + ECharts。

## 主要能力

- **今日待办**：服务端全量统计（非分页推算）、本周投递/回复/面试（半开周窗口、用户时区）、统一行动清单（完成 / 撤销 / 延期）。
- **面试日历**：跨岗位周/月/议程视图，汇总面试（排除取消）、待办截止与岗位截止，保留原始时区。
- **提醒**：服务端每日生成站内通知（逾期待办 / 面试前一天 / 投递满 N 天未回复），偏好持久化、幂等不重复；顶部铃铛与通知页支持已读 / 忽略 / 跳转。
- **统计分析**：0% 与“暂无样本”语义分离，被拒 / 主动撤回 / 岗位关闭分开展示，分子分母可核对。
- 迭代计划与完成状态见 [docs/OfferLog-后续迭代与完善计划.md](./docs/OfferLog-后续迭代与完善计划.md)。

## 快速开始

前置：安装 Docker（含 docker compose）即可，无需本机 Go / Node 环境。

```bash
cp .env.example .env    # 必需：本地配置文件（默认值可直接用，按需修改）
make up
```

构建并启动 postgres + api + worker，并自动建好本地测试账号。完成后打开
<http://localhost:8080>，用 `make up` 最后打印的账号登录：

```
  测试账号：demo@offerlog.local  /  offerlog-demo-1234
```

账号默认值见 `.env.example` 的 `SEED_USER_*`（可改 `.env`，或 `make up SEED_USER_EMAIL=...`）；
账号已存在时不会重置密码，`SEED_USER=0` 可完全跳过建号。登录页「注册」标签也能自助
开号（`REGISTRATION_OPEN=true`），smoke/e2e 验收脚本同样自动注册一次性账号；要闭门
使用就在 `.env` 设 `REGISTRATION_OPEN=false`，再用 api-admin create-user
建号（见 [docs/runbook.md](./docs/runbook.md)）。

```bash
make down
```

停止并移除所有容器数据：容器、网络、数据库与附件卷，全部清空，不可恢复。

说明：

- `make up` 强制要求根目录存在 `.env`（缺失会直接报错并提示 cp 命令），所有本地变量集中在 `.env`。
- `make up` 最后会创建 / 校验本地测试账号并打印账号密码（见 `.env.example` 的 `SEED_USER_*`）。
- 数据保存在 Docker 命名卷中，`make up` 反复重启不丢数据；只有 `make down` 会清空。
- 改 `.env` 里的 postgres 口令只对新初始化的卷生效：需 `make down` 清卷后再 `make up`。
- postgres 同时暴露在 `127.0.0.1:55432`，便于本机 psql / DBeaver 连接排查。

## 目录

```
├── backend/            Go API + worker + admin CLI
├── frontend/           React SPA（今日/表格/看板/日历/统计/文件/设置/通知）
├── api/openapi.yaml    API 契约
├── deploy/             Docker Compose 部署（本地栈：compose.local.yaml）
├── docs/               架构说明、运维手册
└── scripts/            冒烟 / E2E 验收脚本
```

架构与生产部署见 [docs/architecture.md](./docs/architecture.md)、[docs/runbook.md](./docs/runbook.md)；
完整设计见 [OfferLog-全栈开发计划.md](./OfferLog-全栈开发计划.md)。

> 仓库不含真实凭证；`CSRF_SECRET` / 数据库口令等经 `.env` 或环境变量注入。
