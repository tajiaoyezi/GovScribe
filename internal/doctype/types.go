// Package doctype 实现 c06「文种选择引导与路由」能力的领域模型与配置。
//
// 本文件定义文种能力档分级（PRD「文种覆盖矩阵」5 档阶梯）以及按能力档分流到
// c05 深做生成 / c07 通用兜底的路由口径（对齐 design D-06-3）。分级表为 c06
// 拥有的只读共享配置（owner=c06），是路由分流与判别受限标签集的权威来源。
package doctype

import "errors"

// CapabilityTier 是 PRD「文种覆盖矩阵」的 5 档能力阶梯（由高到低）。
type CapabilityTier string

const (
	TierDeep            CapabilityTier = "deep_generation"  // ① 模板生成（深做，A 表 9 文种）
	TierTemplateAssist  CapabilityTier = "template_assist"  // ② 模版辅助写
	TierFramework       CapabilityTier = "framework"        // ③ 框架写
	TierPlannedTraining CapabilityTier = "planned_training" // ④ 待计划训练
	TierFallback        CapabilityTier = "fallback"         // ⑤ 通用兜底
)

var validTiers = map[CapabilityTier]bool{
	TierDeep:            true,
	TierTemplateAssist:  true,
	TierFramework:       true,
	TierPlannedTraining: true,
	TierFallback:        true,
}

// Valid 报告能力档取值是否合法。
func (t CapabilityTier) Valid() bool { return validTiers[t] }

// TargetCapability 是 c06 路由分流的目标生成能力。
type TargetCapability string

const (
	CapabilityC05 TargetCapability = "c05" // 高频文种深做生成
	CapabilityC07 TargetCapability = "c07" // 通用公文兜底生成
)

// MatrixEntry 是文种能力档分级表的一条记录：(文种, 代表子类) → 能力档 + 是否标黄稀缺。
// Subtype 为空表示该文种的文种级条目：A 表文种用作「其它 XX」兜底档，B 表文种用作整文种级归类。
type MatrixEntry struct {
	Doctype       string
	Subtype       string
	Tier          CapabilityTier
	IsStarredRare bool // 标黄稀缺子类（训练数据 <100 条），即便属深做档也降级至 c07
}

// TargetCapability 按 design D-06-3 计算分流目标：仅 A 表深做档且非标黄稀缺命中 c05；
// 其余（标黄稀缺 / B 表各档 / 兜底）一律 c07，全程无「无法处理」死路。
func (e MatrixEntry) TargetCapability() TargetCapability {
	if e.Tier == TierDeep && !e.IsStarredRare {
		return CapabilityC05
	}
	return CapabilityC07
}

// ErrMatrixEntryNotFound 表示分级表中不存在精确匹配的 (文种, 子类) 记录。
var ErrMatrixEntryNotFound = errors.New("doctype capability matrix entry not found")
