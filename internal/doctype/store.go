package doctype

import (
	"context"
	"sync"
)

// MatrixStore 是文种能力档分级表的存取抽象（只读共享配置 + 管理员维护）。
type MatrixStore interface {
	List(ctx context.Context) ([]MatrixEntry, error)
	Lookup(ctx context.Context, doctype, subtype string) (MatrixEntry, error)
}

// MemoryMatrixStore 用 PRD 默认分级表种子初始化，供测试与内存模式使用。
type MemoryMatrixStore struct {
	mu      sync.RWMutex
	entries []MatrixEntry
}

// NewMemoryMatrixStore 返回以默认分级表种子初始化的内存存储。
func NewMemoryMatrixStore() *MemoryMatrixStore {
	return &MemoryMatrixStore{entries: defaultMatrix()}
}

// List 返回分级表全部记录的副本。
func (s *MemoryMatrixStore) List(_ context.Context) ([]MatrixEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MatrixEntry, len(s.entries))
	copy(out, s.entries)
	return out, nil
}

// Lookup 精确查找 (文种, 子类) 记录，缺失返回 ErrMatrixEntryNotFound。
func (s *MemoryMatrixStore) Lookup(_ context.Context, doctype, subtype string) (MatrixEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.entries {
		if e.Doctype == doctype && e.Subtype == subtype {
			return e, nil
		}
	}
	return MatrixEntry{}, ErrMatrixEntryNotFound
}

// DefaultMatrix 返回 PRD「文种覆盖矩阵」默认分级表的副本，供 Postgres 初始化种子使用。
func DefaultMatrix() []MatrixEntry {
	src := defaultMatrix()
	out := make([]MatrixEntry, len(src))
	copy(out, src)
	return out
}
