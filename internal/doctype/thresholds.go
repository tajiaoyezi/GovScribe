package doctype

import (
	"context"
	"sync"
)

// Thresholds 是 c06 判别 / 候选 / 澄清的可调阈值参数（design Open Questions、PRD「指标阈值待实测定档」）。
// 当前取 MVP 经验默认值，首版实测后与客户共同定档。
type Thresholds struct {
	ConfidenceThreshold float64 // 置信度低于此值进入 Top-N 候选分支
	AmbiguityGap        float64 // Top-1 与 Top-2 置信度差小于此值视为多义，进入候选分支
	TopN                int     // 低置信 / 多义时返回的候选个数
	MaxClarifyRounds    int     // 澄清式追问的轮次上限
}

// defaultThresholds 返回 MVP 经验默认阈值（待实测定档）。
func defaultThresholds() Thresholds {
	return Thresholds{
		ConfidenceThreshold: 0.6,
		AmbiguityGap:        0.15,
		TopN:                3,
		MaxClarifyRounds:    3,
	}
}

// DefaultThresholds 返回 MVP 默认阈值的副本，供 Postgres 初始化种子使用。
func DefaultThresholds() Thresholds { return defaultThresholds() }

// ThresholdStore 是阈值参数的存取抽象，支持不改代码前提下调整。
type ThresholdStore interface {
	Get(ctx context.Context) (Thresholds, error)
	Save(ctx context.Context, t Thresholds) error
}

// MemoryThresholdStore 用 MVP 默认阈值初始化，供测试与内存模式使用。
type MemoryThresholdStore struct {
	mu sync.RWMutex
	t  Thresholds
}

// NewMemoryThresholdStore 返回以默认阈值初始化的内存存储。
func NewMemoryThresholdStore() *MemoryThresholdStore {
	return &MemoryThresholdStore{t: defaultThresholds()}
}

// Get 返回当前阈值参数。
func (s *MemoryThresholdStore) Get(_ context.Context) (Thresholds, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.t, nil
}

// Save 覆盖当前阈值参数（不改代码即可调整）。
func (s *MemoryThresholdStore) Save(_ context.Context, t Thresholds) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.t = t
	return nil
}
