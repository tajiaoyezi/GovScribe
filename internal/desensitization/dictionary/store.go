package dictionary

import (
	"context"
	"sync"
)

type Store interface {
	Save(context.Context, Entry) error
	Get(context.Context, string) (Entry, error)
	ListActive(context.Context) ([]Entry, error)
}

type MemoryStore struct {
	mu      sync.RWMutex
	entries map[string]Entry
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: make(map[string]Entry)}
}

func (s *MemoryStore) Save(_ context.Context, entry Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[entry.ID] = entry
	return nil
}

func (s *MemoryStore) Get(_ context.Context, id string) (Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[id]
	if !ok {
		return Entry{}, ErrEntryNotFound
	}
	return entry, nil
}

func (s *MemoryStore) ListActive(_ context.Context) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, 0, len(s.entries))
	for _, entry := range s.entries {
		if !entry.Deleted {
			out = append(out, entry)
		}
	}
	return out, nil
}
