-- OfferLog migration 00005: 用户自定义时间线节点（milestones）
--
-- 之前时间线只由固定的状态机事件（application_events，追加式审计）构成，
-- 每个岗位都被要求沿同一条「规范流程」走。实际上不同岗位的流程差别很大：
-- 有的没有 OA，有的没有初筛，有的直接约面。本迁移把「发生了什么」的选择权
-- 交给用户：任何岗位都可以自行添加节点（OA / 初筛 / 面试 / Offer / 任意
-- 自定义事件），并选择发生时间；时间线面板把这些节点与状态事件按时间合并
-- 排序展示。
--
-- 之所以单独建表而不是复用 application_events：那张表是追加式审计
-- （sequence 单调、corrects_event_id 挂更正），语义是「状态机的证据链」，
-- 而用户自建节点是可编辑、可删除的自由记录，两者的生命周期不同。
--
-- Pure additive: nothing existing is altered or dropped.

CREATE TABLE IF NOT EXISTS application_milestones (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    owner_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 节点类型（slug，前端给出建议清单）：oa | screen | interview | offer | …
    -- 不设白名单约束——「让用户自己选择事件」是本表存在的意义；未知类型按
    -- 自定义展示。空值归一为 custom。
    kind           TEXT NOT NULL DEFAULT 'custom',
    -- 展示名（「一面」「笔试」「背调」…）。空时由展示层按 kind 给默认名。
    label          TEXT NOT NULL DEFAULT '',
    -- 发生时间。可空 = 时间未定（例如「收到 OA 邀请了但还没约时间」），
    -- 排序时排在有时间的节点之后，绝不伪造精确时间。
    occurred_at    TIMESTAMPTZ,
    note           TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS milestones_app_occurred_idx
    ON application_milestones(application_id, occurred_at);
CREATE INDEX IF NOT EXISTS milestones_owner_idx
    ON application_milestones(owner_id, occurred_at);