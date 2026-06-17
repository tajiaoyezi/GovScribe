package doctype

// defaultMatrix 依 PRD「文种覆盖矩阵」与客户《支持公文类型》
// （docs/other/【写公文】支持公文类型.pdf）落地的文种能力档分级表种子数据。
//
// A 表 9 个深做文种逐代表子类录入、标黄稀缺子类（<100 条）单独标记；B 表其余文种按
// 能力档整文种级归类。标黄稀缺降级移交 c07 的判定在路由阶段查本表一次性完成（见 Matrix.Resolve）。
//
// 标黄稀缺子类（数据 <100 条，PDF 标黄）：通知-举办比赛、函-复函、方案-调研方案(8)、方案-会议方案(3)。
// 讲话稿在 PRD 标注「多数子类 <100（黄）」但 PDF 未给逐子类数据量，此处按 A 表深做录入、暂不逐项标黄，
// 待客户《支持公文类型》逐子类数据量确认后由管理员在分级表维护标黄标记。
func defaultMatrix() []MatrixEntry {
	var b matrixBuilder

	// ===== A 表：MVP 深做 9 文种（模板生成档）=====
	b.deep("通知", "工作事务", "召开会议", "开展活动", "人事任免", "转发", "批转", "印发")
	b.starred("通知", "举办比赛") // <100 标黄稀缺 → c07
	b.fallback("通知")        // 其它通知 → 通用兜底

	b.deep("请示", "资金费用申请", "材料上报审定印发", "重大决策事项请求审定", "回复意见", "组织成立")
	b.fallback("请示")

	b.deep("报告", "年度", "季度", "专项工作", "答复", "报送", "会议情况", "紧急", "行业发展", "政府工作报告")
	b.fallback("报告")

	b.deep("函", "工作商洽", "问题询问", "问题答复", "请求批准", "审批答复", "告知",
		"公开征求意见", "面向司局征求意见", "邀请", "人大建议复文", "政协提案复文", "参观拜访")
	b.starred("函", "复函") // <100 标黄稀缺 → c07
	b.fallback("函")

	b.deep("会议纪要", "工作例会", "专项工作", "问题研讨会", "主题座谈会", "培训",
		"常务", "办公", "党组", "党政联席", "党组扩大")
	b.fallback("会议纪要")

	b.deep("通报", "表彰性", "批评性", "工作事务类情况", "事故处理类", "巡查整改情况")
	b.fallback("通报")

	b.deep("批复", "表态式", "阐发式", "否定性", "解答式")
	b.fallback("批复")

	b.deep("讲话稿", "会议活动受邀致辞", "会议总结讲话", "工作报告讲话", "工作部署讲话", "社会问题评论",
		"节日庆典讲话", "政策解读讲话", "学术会议讲话", "开幕式致辞", "闭幕式致辞",
		"追悼会致辞", "纪念会致辞", "动员讲话稿")
	b.fallback("讲话稿")

	b.deep("方案", "工作方案", "活动方案", "整治方案")
	b.starred("方案", "调研方案") // 8 条 <100 → c07
	b.starred("方案", "会议方案") // 3 条 <100 → c07
	b.fallback("方案")

	// ===== B 表（对应 PRD「B. 其余已规划文种」按能力档归类）：非 MVP 深做，整文种级归类，统一路由 c07 =====
	// 各 bTable 调用即「能力档 → 涵盖文种」映射，与 PRD B 表逐档对应（命令/公告/通告在 PRD 同列模版辅助写与框架写，取较高档模版辅助写）。
	b.bTable(TierTemplateAssist, "命令", "决定", "意见", "通告", "公告", "总结", "学习心得")
	b.bTable(TierFramework, "公报", "决议")
	b.bTable(TierPlannedTraining, "条例", "守则", "声明", "公示", "规划", "计划", "规定", "办法",
		"准则", "细则", "启事", "安排", "贺电", "预案", "调研报告", "简报")

	return b.entries
}

// matrixBuilder 按文种聚合地构造分级表种子，避免重复字面量。
type matrixBuilder struct{ entries []MatrixEntry }

// deep 录入 A 表深做档子类（非标黄）。
func (b *matrixBuilder) deep(doctype string, subtypes ...string) {
	for _, s := range subtypes {
		b.entries = append(b.entries, MatrixEntry{Doctype: doctype, Subtype: s, Tier: TierDeep})
	}
}

// starred 录入 A 表深做档下的标黄稀缺子类（<100 条），路由降级至 c07。
func (b *matrixBuilder) starred(doctype, subtype string) {
	b.entries = append(b.entries, MatrixEntry{Doctype: doctype, Subtype: subtype, Tier: TierDeep, IsStarredRare: true})
}

// fallback 录入 A 表文种的「其它 XX」文种级兜底档（子类为空）。
func (b *matrixBuilder) fallback(doctype string) {
	b.entries = append(b.entries, MatrixEntry{Doctype: doctype, Tier: TierFallback})
}

// bTable 录入 B 表文种的整文种级能力档归类（子类为空）。
func (b *matrixBuilder) bTable(tier CapabilityTier, doctypes ...string) {
	for _, d := range doctypes {
		b.entries = append(b.entries, MatrixEntry{Doctype: d, Tier: tier})
	}
}

// Matrix 是分级表的内存解析视图，提供常量级路由解析。
type Matrix struct {
	byKey map[matrixKey]MatrixEntry
}

type matrixKey struct{ doctype, subtype string }

// NewMatrix 用一组分级表记录构造解析视图。
func NewMatrix(entries []MatrixEntry) *Matrix {
	m := &Matrix{byKey: make(map[matrixKey]MatrixEntry, len(entries))}
	for _, e := range entries {
		m.byKey[matrixKey{e.Doctype, e.Subtype}] = e
	}
	return m
}

// Resolve 按 design D-06-3 解析 (文种, 子类) 的能力档与分流目标，保证无死路：
//  1. 精确命中 (文种, 子类)；
//  2. 命中文种级条目（子类为空：A 表「其它 XX」兜底档或 B 表整文种归类）；
//  3. 未知文种 → 合成兜底档。
//
// 标黄稀缺子类的一次性降级在 MatrixEntry.TargetCapability 内完成。
func (m *Matrix) Resolve(doctype, subtype string) (MatrixEntry, TargetCapability) {
	if e, ok := m.byKey[matrixKey{doctype, subtype}]; ok {
		return e, e.TargetCapability()
	}
	if e, ok := m.byKey[matrixKey{doctype, ""}]; ok {
		entry := MatrixEntry{Doctype: doctype, Subtype: subtype, Tier: e.Tier, IsStarredRare: e.IsStarredRare}
		return entry, entry.TargetCapability()
	}
	entry := MatrixEntry{Doctype: doctype, Subtype: subtype, Tier: TierFallback}
	return entry, entry.TargetCapability()
}
