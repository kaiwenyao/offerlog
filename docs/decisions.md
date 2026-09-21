# OfferLog 决策记录

## ADR-001 后端框架与数据库访问
- 采用 Gin（HTTP）+ pgx/pgxpool（PostgreSQL 驱动与连接池）。
- SQL 用显式字符串查询 + 类型化扫描（repository 层），不上 ORM：
  事务可见、易审计；迁移用内嵌版本化 SQL（go:embed + advisory lock），
  生产二进制自包含，不需要外部 goose 工具链。
- 理由：单机个人部署，控制依赖面；对比计划原文的 chi/sqlc 属等价值实现差异，
  领域/服务/仓储分层与事务边界保持不变。

## ADR-002 对象存储可替换适配器
- `objectstore.Store` 接口（Put/Get/Delete/Stat），两个实现：
  `local`（磁盘目录，开发与自包含 compose 默认）与 `s3`（AWS SDK v2 →
  SeaweedFS S3 gateway，path-style）。
- 上传流程：pending 记录 → 流式写 staging → 校验大小/SHA-256/类型 →
  promote 到 final key（owners/{owner}/files/{uuid}/content）→ ready。
- 外部 S3 调用不放入持锁事务；清理由 worker 兜底（宽限 24h）。

## ADR-003 状态与事件模型
- 面试轮次、行动项、备注为独立实体，不扩张顶层状态枚举。
- 每次真实状态变化写 application_events（occurred_at/sequence），
  状态快照与事件在同一事务更新；纠正事件保留 corrects_event_id；
  统计从有效事件重算（补录/纠正后结果变化是预期）。
- 转换校验规则实现于 domain 包（与 web 框架解耦），返回稳定错误码。

## ADR-004 筛选 DSL 安全边界
- 前端只传 JSON 树（字段名/操作符/值），后端解析为参数化 SQL；
  字段白名单 = 内置列 + 当前用户自定义属性键；深度 ≤3、条件 ≤30；
  测试覆盖注入尝试（`evil; drop`）返回 400 而非执行。

## ADR-005 桑基图口径
- 当前进度桑基 A：样本=未删除未归档申请；未投递是叶子；已投递按实际走过的过程阶段展开，终态挂在链尾。每条申请恰好落在一个叶子上，叶子入流合计=cohort。
- 历史桑基 B：按事件重建，(step_index,status) 作节点 ID 防环；
  连续同状态不算迁移；>12 步折叠；导入记录只画“导入起点 → 当前状态”。
- 下钻 token 短期存 analytics_snapshots，与图表生成时刻绑定。
- 2026-09 更新（口径版本 v2）：「未投递」从 submitted_at IS NOT NULL 改为
  与「待投递」指标同口径（saved/preparing 且无投递、无回复事实）——
  内推/猎头免正式投递的记录没有 submitted_at，却已进入招聘流程，
  旧口径把这类记录算成未投递，与同页指标卡和列表自相矛盾。
  渲染上「未投递」固定在第二层（echarts 节点 depth），
  不再被 justify 挤到最右列横穿中间层造成丝带重叠。
  同批把「已投递」卡、回复/面试/Offer 率的分母（样本）与各渠道面板
  统一到同一「进入流程」谓词（repo.toApplyFactSQL 的补集）：
  旧分母按 submitted_at 锚定，会把免正式投递的记录从所有比率里藏掉，
  而同一条记录就显示在上方的「进行中」里。回复中位耗时仍要求
  submitted_at 与 first_response_at 齐备（需要投递时间锚点）。
- 2026-09 更新（口径版本 v3）：当前进度不再把已投递一次性按当前状态摊平。
  已投递分支按 `application_stage_points` 里已经发生的过程阶段展开
  （投递 → 笔试 → 初筛 → 面试 → Offer，只保留真正出现过的），终态
  （被拒绝 / 岗位关闭 / 已撤回 / 已接受）接到链尾。于是「笔试后被拒」
  会经过「笔试作业」再到「被拒绝」，而不是与「投递后直接被拒」混在同一条边上。
  跳过的阶段不补边。未投递仍是叶子，v2 待投递口径不变。完整来回（含回退）
  仍看历史桑基 B。

## ADR-006 单账号但全量 owner_id
- 首版关闭注册、单管理员账号；但 users/companies/applications/… 全带 owner_id，
  服务层每条读取都校验归属，为多用户扩展留出无重构路径。
- 2026-09 更新：开放注册由 `REGISTRATION_OPEN` 控制（默认关闭，cli 建号不变）；
  打开后 `/auth/register` 自助开号并即签会话，默认初始用户随之移除
  （compose 不再 bootstrap me@example.com，本地栈默认开放注册）。
  owner_id 归属校验保证多账号并存时数据隔离。

## ADR-007 前端架构
- Vite + React Router + TanStack Query（服务端状态/缓存失效）+ 手写 fetch 客户端
  （CSRF header、统一错误信封）。ECharts 仅分析页懒加载分包。
- 组件库为原生 HTML + 少量 class（无障碍焦点/Esc 由组件实现），未引入 Tailwind，
  视觉 token（canvas/surface/ink/primary/positive/attention）落实为 CSS 变量。

## ADR-008 部署形态
- 单机 Compose：postgres + api + worker + caddy；SeaweedFS 以 profile 可选自包含。
- 生产入口仅 HTTPS；DB/内部 S3 端口不暴露公网；备份 RPO≤24h/RTO≤2h 目标，
  每月空环境演练（见 docs/runbook.md）。
