# OfferLogs 架构说明

分层与依赖方向符合计划 §6/§7：

```
HTTP (gin, /api/v1)  →  应用服务（用例/事务/权限）  →  领域（状态机/不变量）
                     ↘  repository（SQL 适配器，pgx）
                     ↘  objectstore 适配器（本地磁盘 | S3/SeaweedFS）
后台 worker（jobs 表租约 + outbox）与 cron 清理
```

## 模块
- **identity**：Argon2id、服务端会话、CSRF、限流（登录），v1 单账号但全部按 owner_id 归属。
- **applications**：domain（11 状态 + 转换规则/必须补录原因/Offer 前提）、service（创建/更新/转换/纠正/归档/软删）、repository、transport。
- **activities**：面试轮次、行动项、备注（挂在 /applications/:id/...），另有 /actions 供“今日待办”。
- **views**：自定义属性定义（JSONB）、筛选 DSL（校验→参数化 SQL，深 3/条件 30）、保存视图、排序分组。
- **files**：上传流式 + MIME/SHA-256 校验 → staging key → promote → ready；仅 ready 可下载/关联；引用保护。
- **analytics**：指标定义（§5.2）、桑基 A（当前快照三层守恒）、桑基 B（事件重建历史路径，防环、(step,status) 节点）、快照 token 下钻。
- **transfers**：CSV 预检导入（映射/类型/重复候选/幂等提交）与导出（公式注入转义）。
- **platform**：config、database(事务)、migrate（嵌入式版本化 SQL）、objectstore、jobs、httpx、observability。

## 一致性要点
- 状态变更、事件写入、快照更新、outbox 在同一事务；外部 S3 调用不在持锁事务内。
- 乐观锁 version；转换 idempotency_key；纠正保留 corrects_event_id 审计并重算状态。
- 时间：业务事件 occurred_at + 用户时区；纯日期用 date，精确时间 timestamptz。
