-- OfferLog migration 00004: 求职进度细化与状态回退（docs/求职进度细化与状态回退方案.md）
--
--   1. applications.substatus —— 大阶段内的具体进度。NULL 表示「未细分」，旧数据
--      一律保持 NULL（不默认映射为准备，也不凭停留时长判断已完成）。
--      focus_activity_kind / focus_activity_id —— 当前关注的活动（assessment /
--      interview），列表主标签展示用户选定的关注阶段，不被最后编辑的活动覆盖。
--   2. application_events 扩展前后子状态、活动引用与变更类型，使时间线能区分
--      「流程实际退回（rollback）」与「之前选错了（correct / reopen）」。旧事件
--      新列留空，按未知处理。
--   3. assessment_rounds —— OA / 测评轮次：类型、名称、进度、结果，以及收到 /
--      计划 / 截止 / 实际完成四类互不覆盖的时间。完成与结果分离：面完 / 做完
--      不等于通过。
--   4. interviews —— 活动进度、收到邀请时间、实际完成时间；完成事实独立于
--      result，取消日程不等于撤回申请。
--
-- Pure additive: nothing existing is altered or dropped.

ALTER TABLE applications ADD COLUMN IF NOT EXISTS substatus TEXT;
ALTER TABLE applications ADD COLUMN IF NOT EXISTS focus_activity_kind TEXT;
ALTER TABLE applications ADD COLUMN IF NOT EXISTS focus_activity_id BIGINT;

CREATE INDEX IF NOT EXISTS applications_owner_substatus_idx
    ON applications(owner_id, status, substatus) WHERE deleted_at IS NULL;

ALTER TABLE application_events ADD COLUMN IF NOT EXISTS from_substatus TEXT;
ALTER TABLE application_events ADD COLUMN IF NOT EXISTS to_substatus TEXT;
ALTER TABLE application_events ADD COLUMN IF NOT EXISTS activity_kind TEXT;
ALTER TABLE application_events ADD COLUMN IF NOT EXISTS activity_id BIGINT;
ALTER TABLE application_events ADD COLUMN IF NOT EXISTS change_type TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS assessment_rounds (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id    BIGINT NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    owner_id          BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind              TEXT NOT NULL DEFAULT 'online_test',  -- online_test | take_home | other
    name              TEXT NOT NULL DEFAULT '',
    progress          TEXT NOT NULL DEFAULT 'preparing',    -- preparing | completed | cancelled
    result            TEXT NOT NULL DEFAULT 'unknown',      -- unknown | passed | failed
    invited_at        TIMESTAMPTZ,
    planned_at        TIMESTAMPTZ,
    due_at            TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    completed_unknown BOOLEAN NOT NULL DEFAULT FALSE,
    link              TEXT NOT NULL DEFAULT '',
    notes             TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS assessment_rounds_app_idx ON assessment_rounds(application_id, created_at);

ALTER TABLE interviews ADD COLUMN IF NOT EXISTS progress TEXT NOT NULL DEFAULT '';
ALTER TABLE interviews ADD COLUMN IF NOT EXISTS invited_at TIMESTAMPTZ;
ALTER TABLE interviews ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;
ALTER TABLE interviews ADD COLUMN IF NOT EXISTS completed_unknown BOOLEAN NOT NULL DEFAULT FALSE;

-- 旧数据迁移：只有已经记录了「通过 / 未通过」结果的轮次才有明确的完成证据，
-- 把它映射为 completed 并标注完成时间未知（不伪造精确时间）；其余保持未知。
UPDATE interviews SET progress = 'completed', completed_unknown = TRUE
 WHERE progress = '' AND result IN ('passed', 'failed');
UPDATE interviews SET progress = 'cancelled'
 WHERE progress = '' AND EXISTS (
     SELECT 1 FROM schedule_links sl
      WHERE sl.interview_id = interviews.id AND sl.cancelled = TRUE
 );
