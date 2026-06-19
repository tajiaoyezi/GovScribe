package draft

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/tajiaoyezi/GovScribe/internal/doctype"
)

var ErrStructureContractNotFound = errors.New("high-frequency doctype structure contract not found")

type StructureContract struct {
	Doctype           string
	Direction         doctype.WritingDirection
	TitleRule         string
	SalutationRule    string
	RecipientRule     string
	BodyStructure     []string
	RequiredSlots     []doctype.RequiredSlot
	ClosingRule       string
	SignatureRule     string
	ToneRules         []string
	RedlineRules      []string
	TemplateObjectKey string
	TemplateVersion   string
}

type StructureContractStore interface {
	Get(context.Context, string) (StructureContract, error)
	List(context.Context) ([]StructureContract, error)
}

type MemoryStructureContractStore struct {
	mu        sync.RWMutex
	contracts map[string]StructureContract
}

func NewMemoryStructureContractStore() *MemoryStructureContractStore {
	contracts := make(map[string]StructureContract)
	for _, c := range DefaultStructureContracts() {
		contracts[c.Doctype] = copyStructureContract(c)
	}
	return &MemoryStructureContractStore{contracts: contracts}
}

func (s *MemoryStructureContractStore) Get(_ context.Context, doctypeName string) (StructureContract, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.contracts[doctypeName]
	if !ok {
		return StructureContract{}, ErrStructureContractNotFound
	}
	return copyStructureContract(c), nil
}

func (s *MemoryStructureContractStore) List(context.Context) ([]StructureContract, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]StructureContract, 0, len(s.contracts))
	for _, c := range s.contracts {
		out = append(out, copyStructureContract(c))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Doctype == out[j].Doctype {
			return out[i].Direction < out[j].Direction
		}
		return out[i].Doctype < out[j].Doctype
	})
	return out, nil
}

var structureContractColumns = []string{
	"doctype",
	"direction",
	"title_rule",
	"salutation_rule",
	"recipient_rule",
	"body_structure",
	"required_slots",
	"closing_rule",
	"signature_rule",
	"tone_rules",
	"redline_rules",
	"template_object_key",
	"template_version",
}

type PostgresStructureContractStore struct {
	db *sql.DB
}

func NewPostgresStructureContractStore(db *sql.DB) *PostgresStructureContractStore {
	return &PostgresStructureContractStore{db: db}
}

func (s *PostgresStructureContractStore) Get(ctx context.Context, doctypeName string) (StructureContract, error) {
	row := s.db.QueryRowContext(ctx, selectStructureContractSQL("WHERE doctype = $1"), doctypeName)
	c, err := scanStructureContract(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StructureContract{}, ErrStructureContractNotFound
	}
	if err != nil {
		return StructureContract{}, err
	}
	return c, nil
}

func (s *PostgresStructureContractStore) List(ctx context.Context) ([]StructureContract, error) {
	rows, err := s.db.QueryContext(ctx, selectStructureContractSQL("ORDER BY doctype, direction"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StructureContract
	for rows.Next() {
		c, err := scanStructureContract(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func SeedStructureContracts(ctx context.Context, db *sql.DB, contracts []StructureContract) error {
	for _, c := range contracts {
		body, err := json.Marshal(c.BodyStructure)
		if err != nil {
			return err
		}
		slots := requiredSlotsToStrings(c.RequiredSlots)
		requiredSlots, err := json.Marshal(slots)
		if err != nil {
			return err
		}
		toneRules, err := json.Marshal(c.ToneRules)
		if err != nil {
			return err
		}
		redlineRules, err := json.Marshal(c.RedlineRules)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO high_freq_doctype_structure_contracts (
    doctype,
    direction,
    title_rule,
    salutation_rule,
    recipient_rule,
    body_structure,
    required_slots,
    closing_rule,
    signature_rule,
    tone_rules,
    redline_rules,
    template_object_key,
    template_version
) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10::jsonb, $11::jsonb, $12, $13)
ON CONFLICT (doctype, direction) DO NOTHING`,
			c.Doctype,
			string(c.Direction),
			c.TitleRule,
			c.SalutationRule,
			c.RecipientRule,
			string(body),
			string(requiredSlots),
			c.ClosingRule,
			c.SignatureRule,
			string(toneRules),
			string(redlineRules),
			c.TemplateObjectKey,
			c.TemplateVersion,
		); err != nil {
			return err
		}
	}
	return nil
}

func DefaultStructureContracts() []StructureContract {
	slots := defaultRequiredSlotsByDoctype()
	redlines := []string{
		"政治性表述必须准确",
		"不得臆造事实、数据、文号、人名、单位名或单位全称",
		"不得输出敏感或不合规表述",
		"缺失关键要素必须留占位或待补提示",
		"产出仅为待人工核稿的初稿，不含自动发文或自动提交语义",
	}
	contracts := []StructureContract{
		{
			Doctype:        "通知",
			Direction:      doctype.DirectionDownward,
			TitleRule:      "关于 + 事由 + 通知",
			SalutationRule: "主送相关下级机关或业务单位",
			RecipientRule:  "主送机关应为下级机关、内设机构或需知悉执行的相关单位",
			BodyStructure:  []string{"开头说明通知缘由", "主体列明事项、时间、地点、要求", "结尾提出执行或反馈要求"},
			RequiredSlots:  slots["通知"],
			ClosingRule:    "以执行要求或特此通知类语句收束",
			SignatureRule:  "发文单位署名 + 成文日期",
			ToneRules:      []string{"下行部署口吻", "事项明确", "要求具体可执行"},
			RedlineRules:   redlines,
		},
		{
			Doctype:        "请示",
			Direction:      doctype.DirectionUpward,
			TitleRule:      "关于 + 事由 + 的请示",
			SalutationRule: "主送一个有审批权限的上级机关",
			RecipientRule:  "主送机关必须是上级主管机关，避免多头主送",
			BodyStructure:  []string{"开头说明请示缘由和依据", "主体说明请示事项和必要性", "结尾明确请示事项"},
			RequiredSlots:  slots["请示"],
			ClosingRule:    "妥否，请批示。",
			SignatureRule:  "发文单位署名 + 成文日期",
			ToneRules:      []string{"上行呈请口吻", "一文一事", "请示事项明确"},
			RedlineRules:   redlines,
		},
		{
			Doctype:        "报告",
			Direction:      doctype.DirectionUpward,
			TitleRule:      "关于 + 事由 + 的报告",
			SalutationRule: "主送上级机关",
			RecipientRule:  "主送机关为需要了解情况的上级机关",
			BodyStructure:  []string{"开头概述报告背景", "主体报告进展、情况或问题", "结尾提出后续安排或请予审阅"},
			RequiredSlots:  slots["报告"],
			ClosingRule:    "特此报告。",
			SignatureRule:  "发文单位署名 + 成文日期",
			ToneRules:      []string{"上行报告口吻", "重在陈述情况", "不得夹带请示事项"},
			RedlineRules:   redlines,
		},
		{
			Doctype:        "函",
			Direction:      doctype.DirectionHorizontal,
			TitleRule:      "关于 + 事由 + 的函",
			SalutationRule: "主送不相隶属机关或同级单位",
			RecipientRule:  "主送机关为平级或不相隶属机关",
			BodyStructure:  []string{"开头说明来函缘由", "主体写明商洽、询问或答复事项", "结尾提出办理或函复要求"},
			RequiredSlots:  slots["函"],
			ClosingRule:    "特此函达，请予函复。",
			SignatureRule:  "发文单位署名 + 成文日期",
			ToneRules:      []string{"平行商洽口吻", "询问事项明确", "语气平实", "事项边界清楚"},
			RedlineRules:   redlines,
		},
		{
			Doctype:        "会议纪要",
			Direction:      doctype.DirectionDownward,
			TitleRule:      "会议名称 + 纪要",
			SalutationRule: "面向参会单位或执行单位",
			RecipientRule:  "主送或分送与会议事项相关的单位",
			BodyStructure:  []string{"会议基本情况", "议定事项", "任务分工与落实要求"},
			RequiredSlots:  slots["会议纪要"],
			ClosingRule:    "以落实要求或会议议定事项收束",
			SignatureRule:  "会议主办单位或纪要形成单位 + 日期",
			ToneRules:      []string{"纪实准确", "事项清晰", "责任明确"},
			RedlineRules:   redlines,
		},
		{
			Doctype:        "通报",
			Direction:      doctype.DirectionDownward,
			TitleRule:      "关于 + 事由 + 的通报",
			SalutationRule: "主送相关单位或下级机关",
			RecipientRule:  "主送机关为需知悉、学习或整改的单位",
			BodyStructure:  []string{"开头概述通报背景", "主体说明事实、评价或问题", "结尾提出要求"},
			RequiredSlots:  slots["通报"],
			ClosingRule:    "以学习贯彻或整改落实要求收束",
			SignatureRule:  "发文单位署名 + 成文日期",
			ToneRules:      []string{"下行告知口吻", "事实准确", "评价审慎"},
			RedlineRules:   redlines,
		},
		{
			Doctype:        "批复",
			Direction:      doctype.DirectionDownward,
			TitleRule:      "关于 + 请示事项 + 的批复",
			SalutationRule: "主送请示单位",
			RecipientRule:  "主送机关为提出请示的下级机关",
			BodyStructure:  []string{"引述来文", "明确批复意见", "提出执行要求"},
			RequiredSlots:  slots["批复"],
			ClosingRule:    "此复。",
			SignatureRule:  "发文单位署名 + 成文日期",
			ToneRules:      []string{"下行答复口吻", "告知批复结论", "态度明确", "执行要求清楚"},
			RedlineRules:   redlines,
		},
		{
			Doctype:        "讲话稿",
			Direction:      doctype.DirectionUnspecified,
			TitleRule:      "围绕会议或活动主题拟定标题",
			SalutationRule: "根据会议对象设置称呼",
			RecipientRule:  "面向参会人员或特定听众",
			BodyStructure:  []string{"开场引入", "主体观点和工作部署", "结尾号召或要求"},
			RequiredSlots:  slots["讲话稿"],
			ClosingRule:    "以号召、要求或总结性语句收束",
			SignatureRule:  "讲话人或单位 + 日期",
			ToneRules:      []string{"讲话场景口吻", "观点鲜明", "层次清楚"},
			RedlineRules:   redlines,
		},
		{
			Doctype:        "方案",
			Direction:      doctype.DirectionUnspecified,
			TitleRule:      "事项 + 实施方案",
			SalutationRule: "按方案适用对象设置",
			RecipientRule:  "适用对象为执行或参与单位",
			BodyStructure:  []string{"总体要求", "目标任务", "工作安排", "保障措施"},
			RequiredSlots:  slots["方案"],
			ClosingRule:    "以组织实施和责任落实要求收束",
			SignatureRule:  "制定单位 + 日期",
			ToneRules:      []string{"部署安排口吻", "任务可执行", "责任边界清晰"},
			RedlineRules:   redlines,
		},
	}
	for i := range contracts {
		contracts[i].TemplateObjectKey = mustPromptTemplateObjectKey(contracts[i].Doctype, DefaultPromptTemplateVersion)
		contracts[i].TemplateVersion = DefaultPromptTemplateVersion
	}
	out := make([]StructureContract, len(contracts))
	for i, c := range contracts {
		out[i] = copyStructureContract(c)
	}
	return out
}

func selectStructureContractSQL(suffix string) string {
	query := "SELECT " + strings.Join(structureContractColumns, ", ") + " FROM high_freq_doctype_structure_contracts"
	if suffix != "" {
		query += " " + suffix
	}
	return query
}

type structureContractScanner interface {
	Scan(dest ...any) error
}

func scanStructureContract(scanner structureContractScanner) (StructureContract, error) {
	var c StructureContract
	var direction string
	var bodyRaw, slotsRaw, toneRaw, redlineRaw []byte
	if err := scanner.Scan(
		&c.Doctype,
		&direction,
		&c.TitleRule,
		&c.SalutationRule,
		&c.RecipientRule,
		&bodyRaw,
		&slotsRaw,
		&c.ClosingRule,
		&c.SignatureRule,
		&toneRaw,
		&redlineRaw,
		&c.TemplateObjectKey,
		&c.TemplateVersion,
	); err != nil {
		return StructureContract{}, err
	}
	c.Direction = doctype.WritingDirection(direction)
	if err := json.Unmarshal(bodyRaw, &c.BodyStructure); err != nil {
		return StructureContract{}, err
	}
	var slotStrings []string
	if err := json.Unmarshal(slotsRaw, &slotStrings); err != nil {
		return StructureContract{}, err
	}
	c.RequiredSlots = stringsToRequiredSlots(slotStrings)
	if err := json.Unmarshal(toneRaw, &c.ToneRules); err != nil {
		return StructureContract{}, err
	}
	if err := json.Unmarshal(redlineRaw, &c.RedlineRules); err != nil {
		return StructureContract{}, err
	}
	return c, nil
}

func defaultRequiredSlotsByDoctype() map[string][]doctype.RequiredSlot {
	out := make(map[string][]doctype.RequiredSlot)
	for _, item := range doctype.DefaultRequiredSlots() {
		out[item.Doctype] = append(out[item.Doctype], item.Slot)
	}
	for k, slots := range out {
		cp := make([]doctype.RequiredSlot, len(slots))
		copy(cp, slots)
		out[k] = cp
	}
	return out
}

func requiredSlotsToStrings(slots []doctype.RequiredSlot) []string {
	out := make([]string, len(slots))
	for i, slot := range slots {
		out[i] = string(slot)
	}
	return out
}

func stringsToRequiredSlots(slots []string) []doctype.RequiredSlot {
	out := make([]doctype.RequiredSlot, len(slots))
	for i, slot := range slots {
		out[i] = doctype.RequiredSlot(slot)
	}
	return out
}

func copyStructureContract(c StructureContract) StructureContract {
	c.BodyStructure = append([]string(nil), c.BodyStructure...)
	c.RequiredSlots = append([]doctype.RequiredSlot(nil), c.RequiredSlots...)
	c.ToneRules = append([]string(nil), c.ToneRules...)
	c.RedlineRules = append([]string(nil), c.RedlineRules...)
	return c
}

func copyColumns(cols []string) []string {
	out := make([]string, len(cols))
	copy(out, cols)
	return out
}
