-- OfferLog migration 00007: 把「只写了列、时间线上没有落点」的投递时间补成落点
--
-- 迁移 00006 之后 applications.submitted_at 是**派生**列：RecomputeStatus 从时间线
-- 上第一个「已投递」落点回填它。CSV 导入（internal/transfers）当时只写列，另外只补
-- 一条业务时间 = 导入时刻的 created 事件，于是用户一旦加／改／删一个时间线节点：
--
--   * 导入时状态是「已投递」的记录 —— 真实投递时间被换成导入时刻；
--   * 导入时状态更靠后的记录 —— 投递时间直接被清空。
--
-- 代码侧已经修好（repository.InitialEvents 现在是建档落点的唯一定义，手工建档和
-- 导入共用）。这支迁移负责存量：把这些记录补齐成同一套落点，这样它们的投递时间
-- 在下一次回放时**有据可依**，不会被顶掉。
--
-- 只修「从导入／建档之后就没被动过」的记录，判据严格到不可能误伤：
--   submitted_at 非空、阶段不是预投递、全表只有一条 created 事件、没有任何用户
--   节点，且那条 created 事件本身还不是一个时间对得上的投递落点。
-- 任何被编辑过的记录（≥2 条事件或有节点）一律不碰——它的 submitted_at 早已是回放
-- 的产物，再补落点只会改写用户看得见的历史。
--
-- 幂等：修完的记录有 2~3 条事件，不再满足上面的判据。

CREATE TEMP TABLE orphan_submissions ON COMMIT DROP AS
SELECT a.id, a.owner_id, a.status, a.submitted_at,
       e.id AS created_id, e.sequence AS created_seq, e.occurred_at AS created_at
FROM applications a
JOIN application_events e ON e.application_id = a.id
WHERE a.submitted_at IS NOT NULL
  AND a.status NOT IN ('saved', 'preparing')
  AND e.event_type = 'created'
  AND NOT (COALESCE(e.to_status, '') = 'applied' AND e.occurred_at = a.submitted_at)
  AND (SELECT count(*) FROM application_events x WHERE x.application_id = a.id) = 1
  AND NOT EXISTS (SELECT 1 FROM application_milestones m WHERE m.application_id = a.id);

-- 投递那一刻单独成行，业务时间就是列里那个用户填的时间。
INSERT INTO application_events(application_id, owner_id, sequence, event_type,
                               from_status, to_status, note, change_type, occurred_at, actor_id)
SELECT o.id, o.owner_id, o.created_seq + 1, 'status_change',
       'saved', 'applied', '', 'advance', o.submitted_at, o.owner_id
FROM orphan_submissions o;

-- 建档那一格必须描述投递**之前**的状态：它在时间线上被钉在最前面，而回放取第一个
-- 投递落点——留着它声称「已投递」就等于继续把建档时刻当成投递时间。
UPDATE application_events e SET to_status = 'saved'
FROM orphan_submissions o WHERE e.id = o.created_id;

-- 建在更靠后阶段的记录：补一行「进入当前阶段」，时间用建档时刻（哪天进去的无从
-- 得知），且不早于投递时间，否则时间线最后一格会变成投递、阶段被退回去。
INSERT INTO application_events(application_id, owner_id, sequence, event_type,
                               from_status, to_status, note, change_type, occurred_at, actor_id)
SELECT o.id, o.owner_id, o.created_seq + 2, 'status_change',
       'applied', o.status, '', 'advance', GREATEST(o.created_at, o.submitted_at), o.owner_id
FROM orphan_submissions o
WHERE o.status <> 'applied';
