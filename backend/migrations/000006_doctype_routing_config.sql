-- c06 文种选择引导与路由：必需要素清单与可调阈值参数配置表。
-- 与 000005 分级表同口径——迁移仅建表，默认种子的权威来源为 Go 侧
-- doctype.DefaultRequiredSlots / doctype.DefaultThresholds，由应用启动时经
-- doctype.SeedRequiredSlots / doctype.SeedThresholds 幂等写入。

-- 必需要素清单：按「文种 + 行文方向」可维护（direction 为空表示该文种所有方向通用）。
CREATE TABLE IF NOT EXISTS doctype_required_slots (
    doctype TEXT NOT NULL,
    direction TEXT NOT NULL DEFAULT '' CHECK (direction IN ('', 'upward', 'downward', 'horizontal')),
    slot TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (doctype, direction, slot)
);

-- 可调阈值参数：单行配置（id 仅可为 TRUE 以约束单例），支持不改代码运行时调整。
CREATE TABLE IF NOT EXISTS doctype_routing_thresholds (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    confidence_threshold DOUBLE PRECISION NOT NULL,
    ambiguity_gap DOUBLE PRECISION NOT NULL,
    top_n INTEGER NOT NULL,
    max_clarify_rounds INTEGER NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
