-- OfferLog migration 00003: 前端设计改版（Industry / 蓝图）后端补口。
--
--   1. 截图按面试轮次归档：application_files 增加可空 interview_id，
--      附件（截图 / 简历版本 / Offer 文件）可挂到具体面试轮次，详情抽屉
--      「截图与附件」按轮次分组渲染（画布：⌘V 直接粘贴截图，自动挂到当前轮次）。
--      ON DELETE SET NULL —— 轮次删除后附件退化为「不归属任何轮次」而非被连带删除。
--   2. 其余改版能力（阶段历史批量、跨实体搜索、JD 链接预填、image 自定义字段、
--      DEV_ALLOWED_ORIGINS）不需要 DDL：阶段历史读 application_events，
--      搜索读现有表，image 只是 property_definitions.data_type 的新取值（TEXT 列）。
--
-- Pure additive: nothing existing is altered or dropped.

ALTER TABLE application_files ADD COLUMN IF NOT EXISTS interview_id BIGINT REFERENCES interviews(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS app_files_interview_idx
    ON application_files(interview_id) WHERE interview_id IS NOT NULL;
