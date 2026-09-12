-- OfferLog migration 00006: 事件驱动的阶段推导
--
-- 00005 把「发生了什么」的选择权交给了用户（application_milestones），但那一层
-- 刻意不碰状态机：岗位的阶段仍然只能靠「更新进度」弹窗在一条固定流水线上挑格子。
-- 本迁移完成换代——用户只管添加事件，阶段由事件推导：
--
--   当前状态 = 时间线上最后一个带 status_effect 的节点的 status_effect
--
-- 「最后一个」用的就是用户看到的排序（业务时间升序、时间未定的排最后），所以
-- 「补一个更早的面试」不会改变当前状态，「补一个更晚的 Offer」会。规则只有这一条，
-- 看图就能预期。
--
-- 两处结构改动：
--   1. application_milestones.status_effect —— 节点落地时由服务端按 kind 写入。
--      存列而不是每次按 kind 现算：kind 是开放集合（用户可写任意 slug），存下来
--      才能保证「当时记的是什么阶段」不随建议清单的改动而漂移。
--   2. 视图 application_stage_points —— 把「状态事件」和「用户节点」两个来源
--      统一成一张 (application_id, owner_id, status, occurred_at) 表。
--      分析漏斗、桑基图、stage_history、HadOffer、ProgressSince 原本直接查
--      application_events；改查这个视图后，新老两种来源一起算，语义不变。
--
-- 为什么不让 milestone 去写 application_events：那张表是追加式审计
-- （sequence 单调、corrects_event_id 挂更正），而用户节点可编辑可删除。
-- 让可删的行去改写审计链会同时毁掉两者的语义。视图把它们在**读**的一侧合并，
-- 写的一侧各自保持自己的生命周期。
--
-- Pure additive: 没有删列、没有改类型、没有丢数据。

ALTER TABLE application_milestones
    ADD COLUMN IF NOT EXISTS status_effect TEXT NOT NULL DEFAULT '';

-- 回填 00005 期间已经存在的节点。旧的 kind 集合是 oa/screen/interview/offer/
-- phone/custom；前四个有明确的阶段含义，后两个只是记事。
UPDATE application_milestones SET status_effect = 'assessment'    WHERE kind = 'oa'        AND status_effect = '';
UPDATE application_milestones SET status_effect = 'screening'     WHERE kind = 'screen'    AND status_effect = '';
UPDATE application_milestones SET status_effect = 'interviewing'  WHERE kind = 'interview' AND status_effect = '';
UPDATE application_milestones SET status_effect = 'offer'         WHERE kind = 'offer'     AND status_effect = '';

-- 推导只关心带 status_effect 的节点，且总是按 (岗位, 业务时间) 取最后一条。
CREATE INDEX IF NOT EXISTS milestones_status_effect_idx
    ON application_milestones(application_id, occurred_at)
    WHERE status_effect <> '';

-- 统一的「阶段落点」读模型。
--
-- substatus 只有事件侧可能携带（旧的「更新进度」弹窗和更正会写它）；节点侧恒为
-- NULL，于是「加一个事件」自然会把上一段的细化进度清掉，而不是让它挂在新阶段上。
--
-- note 列是「这一步为什么发生」：事件侧取 reason（终态转换当时必填的那个原因），
-- 节点侧取用户写的备注。RecomputeStatus 用它回填 applications.reason，这样抽屉里
-- 那张「原因」卡片在新旧两种记录方式下都说得出话。
--
-- 事件侧沿用原先散落在 analytics/repo.go 的 effectiveToStatusSQL 语义：correction
-- 行本身不产生落点，它只覆盖 corrects_event_id 指向的那条事件的状态与业务时间
-- （同一条事件被多次更正时，sequence 最大的那次生效）。把这段逻辑收进视图，
-- Go 侧就只剩一份实现。
CREATE OR REPLACE VIEW application_stage_points AS
SELECT
    e.application_id,
    e.owner_id,
    COALESCE(c.to_status, e.to_status)     AS status,
    COALESCE(c.to_substatus, e.to_substatus) AS substatus,
    COALESCE(c.occurred_at, e.occurred_at) AS occurred_at,
    e.reason                               AS note,
    'event'::TEXT                          AS source,
    e.id                                   AS source_id,
    e.sequence                             AS sequence,
    (e.event_type = 'created')             AS pinned_first
FROM application_events e
LEFT JOIN LATERAL (
    SELECT x.to_status, x.to_substatus, x.occurred_at
    FROM application_events x
    WHERE x.corrects_event_id = e.id
      AND x.event_type = 'correction'
      AND x.to_status IS NOT NULL
    ORDER BY x.sequence DESC
    LIMIT 1
) c ON TRUE
WHERE e.event_type <> 'correction'
  AND COALESCE(c.to_status, e.to_status) IS NOT NULL

UNION ALL

SELECT
    m.application_id,
    m.owner_id,
    m.status_effect AS status,
    -- 用户不再挑子状态（迁移 00006）：节点只说发生了什么，阶段内的细化进度
    -- 只由 OA / 面试轮次面板派生。NULL 表示「这一格没有主张子状态」。
    NULL::TEXT      AS substatus,
    m.occurred_at   AS occurred_at,
    m.note          AS note,
    'milestone'::TEXT AS source,
    m.id            AS source_id,
    NULL::INTEGER   AS sequence,
    FALSE           AS pinned_first
FROM application_milestones m
WHERE m.status_effect <> '';
