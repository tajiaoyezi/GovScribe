package doctype

import (
	"context"
	"sync"
)

// RequiredSlot 是公文必需要素类型（slot-clarification spec、design D-06-5）。
type RequiredSlot string

const (
	SlotIssuer    RequiredSlot = "发文单位"
	SlotRecipient RequiredSlot = "主送机关"
	SlotSubject   RequiredSlot = "事由"
	SlotKeyMatter RequiredSlot = "关键事项"
	SlotTimePlace RequiredSlot = "关键时间地点"
)

// SlotRequirement 是「文种 + 行文方向」对应的一条必需要素配置。
// Direction 为空表示适用于该文种的所有行文方向。
type SlotRequirement struct {
	Doctype   string
	Direction WritingDirection
	Slot      RequiredSlot
}

// SlotStore 是必需要素清单的存取抽象（按文种 + 行文方向可维护，配置与代码解耦）。
type SlotStore interface {
	RequiredSlots(ctx context.Context, doctype string, direction WritingDirection) ([]RequiredSlot, error)
	List(ctx context.Context) ([]SlotRequirement, error)
}

// MemorySlotStore 用默认必需要素清单种子初始化，供测试与内存模式使用。
type MemorySlotStore struct {
	mu    sync.RWMutex
	items []SlotRequirement
}

// NewMemorySlotStore 返回以默认必需要素清单初始化的内存存储。
func NewMemorySlotStore() *MemorySlotStore {
	return &MemorySlotStore{items: defaultRequiredSlots()}
}

// RequiredSlots 返回 (文种, 行文方向) 的必需要素集合：方向精确匹配项与方向无关项（Direction 为空）并集，保序去重。
func (s *MemorySlotStore) RequiredSlots(_ context.Context, doctype string, direction WritingDirection) ([]RequiredSlot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return collectRequiredSlots(s.items, doctype, direction), nil
}

// List 返回全部必需要素配置的副本。
func (s *MemorySlotStore) List(_ context.Context) ([]SlotRequirement, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SlotRequirement, len(s.items))
	copy(out, s.items)
	return out, nil
}

// collectRequiredSlots 从配置项中筛出某文种在某方向下的必需要素，保序去重。
func collectRequiredSlots(items []SlotRequirement, doctype string, direction WritingDirection) []RequiredSlot {
	var slots []RequiredSlot
	seen := make(map[RequiredSlot]bool)
	for _, it := range items {
		if it.Doctype != doctype {
			continue
		}
		if it.Direction != "" && it.Direction != direction {
			continue
		}
		if !seen[it.Slot] {
			seen[it.Slot] = true
			slots = append(slots, it.Slot)
		}
	}
	return slots
}

// defaultRequiredSlots 是覆盖首期 9 深做文种的默认必需要素清单种子（方向无关，direction 为空）。
// 不同客户的行文习惯差异较大，本清单为可维护默认值，交付期可按客户实际公文习惯调整。
func defaultRequiredSlots() []SlotRequirement {
	defaults := map[string][]RequiredSlot{
		"请示":   {SlotIssuer, SlotRecipient, SlotSubject, SlotKeyMatter},
		"报告":   {SlotIssuer, SlotRecipient, SlotSubject, SlotKeyMatter},
		"通知":   {SlotIssuer, SlotRecipient, SlotSubject, SlotKeyMatter},
		"函":    {SlotIssuer, SlotRecipient, SlotSubject},
		"批复":   {SlotIssuer, SlotRecipient, SlotSubject},
		"通报":   {SlotIssuer, SlotSubject, SlotKeyMatter},
		"会议纪要": {SlotIssuer, SlotSubject, SlotTimePlace, SlotKeyMatter},
		"讲话稿":  {SlotIssuer, SlotSubject, SlotKeyMatter},
		"方案":   {SlotIssuer, SlotSubject, SlotKeyMatter, SlotTimePlace},
	}
	var items []SlotRequirement
	for _, doctype := range deepDoctypeOrder {
		for _, slot := range defaults[doctype] {
			items = append(items, SlotRequirement{Doctype: doctype, Slot: slot})
		}
	}
	return items
}

// deepDoctypeOrder 固定默认必需要素清单的文种顺序，保证种子可复现。
var deepDoctypeOrder = []string{"通知", "请示", "报告", "函", "会议纪要", "通报", "批复", "讲话稿", "方案"}

// DefaultRequiredSlots 返回默认必需要素清单的副本，供 Postgres 初始化种子使用。
func DefaultRequiredSlots() []SlotRequirement {
	src := defaultRequiredSlots()
	out := make([]SlotRequirement, len(src))
	copy(out, src)
	return out
}
