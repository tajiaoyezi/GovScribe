-- c05 高频文种深做：9 个高频文种的结构契约配置表。
-- 本表只承载文种结构契约标量字段；文种 / 子类能力档、标黄稀缺、
-- 路由目标等分级判定字段仍归 c06 doctype_capability_matrix 单一权威维护。
-- 默认种子的权威来源为 Go 侧 draft.DefaultStructureContracts，
-- 由应用启动时经 draft.SeedStructureContracts 幂等写入。

CREATE TABLE IF NOT EXISTS high_freq_doctype_structure_contracts (
    doctype TEXT NOT NULL,
    direction TEXT NOT NULL DEFAULT '' CHECK (direction IN ('', 'upward', 'downward', 'horizontal')),
    title_rule TEXT NOT NULL,
    salutation_rule TEXT NOT NULL,
    recipient_rule TEXT NOT NULL,
    body_structure JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(body_structure) = 'array'),
    required_slots JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(required_slots) = 'array'),
    closing_rule TEXT NOT NULL,
    signature_rule TEXT NOT NULL,
    tone_rules JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(tone_rules) = 'array'),
    redline_rules JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(redline_rules) = 'array'),
    template_object_key TEXT NOT NULL DEFAULT '',
    template_version TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (doctype, direction)
);
