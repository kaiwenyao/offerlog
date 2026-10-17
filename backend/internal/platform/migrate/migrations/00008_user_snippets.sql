-- OfferLog migration 00008: 常用片段（snippets）
--
-- 投简历时总要反复粘贴同几段文字：实习经历的 role description、自我介绍、
-- Why us……本迁移给每个用户一张片段表，保存「标题 + 内容 + 分类」，在片段库
-- 页面和 ⌘K 面板里一键复制。片段是用户级的，不挂在任何岗位上。
--
-- 分类是自由文本（「实习经历」「自我介绍」…），不设白名单；空串 = 未分类。
--
-- Pure additive: nothing existing is altered or dropped.

CREATE TABLE IF NOT EXISTS user_snippets (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id   BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      TEXT NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    category   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS user_snippets_owner_idx
    ON user_snippets(owner_id, category, updated_at DESC);
