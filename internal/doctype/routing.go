package doctype

// RouteLabel 是 c06 按能力档分流的结构化路由标签（design D-06-3、「按能力档分流路由」spec）。
//
// 仅为结构化标签：含目标生成能力、文种、子类、行文方向，随场景上下文移交；
// 不直接调用 c05 / c07，对下游生成的调用由上层编排在放行后发起（4.4 约束）。
type RouteLabel struct {
	TargetCapability TargetCapability
	Doctype          string
	Subtype          string
	Direction        WritingDirection
}

// Route 依判别结果按能力档分流为结构化路由标签，保证全程无死路（4.3）：
// A 表深做且非标黄稀缺命中 c05；其余（标黄稀缺 / B 表各档 / 兜底 / 未知文种）一律 c07，绝不返回「无法处理」。
// Route 是纯函数：仅据已标注的能力档产出标签，不发起任何对下游生成能力的调用。
func Route(result ClassificationResult) RouteLabel {
	return RouteLabel{
		TargetCapability: routeCapability(result.Tier, result.IsStarredRare),
		Doctype:          result.Doctype,
		Subtype:          result.Subtype,
		Direction:        result.Direction,
	}
}

// routeCapability 是按能力档判定目标生成能力的单一权威规则（D-06-3），由 Route 与
// MatrixEntry.TargetCapability 共用，确保分流口径一致。
func routeCapability(tier CapabilityTier, isStarredRare bool) TargetCapability {
	if tier == TierDeep && !isStarredRare {
		return CapabilityC05
	}
	return CapabilityC07
}
