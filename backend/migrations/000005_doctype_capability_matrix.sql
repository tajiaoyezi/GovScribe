-- c06 文种选择引导与路由：文种能力档分级表（owner=c06，只读共享配置）。
-- 承载 PRD「文种覆盖矩阵」5 档能力阶梯与标黄稀缺子类标记，是路由分流（c05 / c07）
-- 与判别受限标签集的权威来源。默认种子的权威来源为 Go 侧 doctype.DefaultMatrix
-- （PRD 文种覆盖矩阵 / 客户《支持公文类型》），由应用启动时经 doctype.SeedMatrix 幂等写入，
-- 与 c02 security_classification_routes「迁移建表 + Go 默认值」同一口径，本脚本仅建表不内联种子。
CREATE TABLE IF NOT EXISTS doctype_capability_matrix (
    doctype TEXT NOT NULL,
    subtype TEXT NOT NULL DEFAULT '',
    capability_tier TEXT NOT NULL CHECK (capability_tier IN (
        'deep_generation',
        'template_assist',
        'framework',
        'planned_training',
        'fallback'
    )),
    is_starred_rare BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (doctype, subtype)
);
