package dictionary

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

type Service struct {
	store    Store
	auth     Authorizer
	reloader EntryReloader
	now      func() time.Time
}

func NewService(store Store, auth Authorizer) *Service {
	return NewServiceWithReloader(store, auth, nil)
}

func NewServiceWithReloader(store Store, auth Authorizer, reloader EntryReloader) *Service {
	return &Service{store: store, auth: auth, reloader: reloader, now: time.Now}
}

func (s *Service) CreateEntry(ctx context.Context, principal Principal, req CreateEntryRequest) (Entry, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return Entry{}, err
	}
	entry, err := s.newEntry(req.Text, req.Type)
	if err != nil {
		return Entry{}, err
	}
	if err := s.store.Save(ctx, entry); err != nil {
		return Entry{}, err
	}
	if err := s.reloadDictionary(ctx); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *Service) ImportEntries(ctx context.Context, principal Principal, imports []ImportEntry) ([]Entry, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, max(len(imports), defaultDictionaryImportBatchCapacity))
	for _, item := range imports {
		entry, err := s.newEntry(item.Text, item.Type)
		if err != nil {
			return nil, err
		}
		if err := s.store.Save(ctx, entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := s.reloadDictionary(ctx); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Service) ListEntries(ctx context.Context, principal Principal) ([]Entry, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return nil, err
	}
	entries, err := s.store.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type == entries[j].Type {
			return entries[i].Text < entries[j].Text
		}
		return entries[i].Type < entries[j].Type
	})
	return entries, nil
}

func (s *Service) UpdateEntry(ctx context.Context, principal Principal, id string, req UpdateEntryRequest) (Entry, error) {
	if err := s.authorize(ctx, principal); err != nil {
		return Entry{}, err
	}
	entry, err := s.store.Get(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	if req.Text != "" {
		entry.Text = strings.TrimSpace(req.Text)
	}
	if req.Type != "" {
		entry.Type = req.Type
	}
	if err := validateEntry(entry.Text, entry.Type); err != nil {
		return Entry{}, err
	}
	entry.UpdatedAt = s.now()
	if err := s.store.Save(ctx, entry); err != nil {
		return Entry{}, err
	}
	if err := s.reloadDictionary(ctx); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *Service) DeleteEntry(ctx context.Context, principal Principal, id string) error {
	if err := s.authorize(ctx, principal); err != nil {
		return err
	}
	entry, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	entry.Deleted = true
	entry.UpdatedAt = s.now()
	if err := s.store.Save(ctx, entry); err != nil {
		return err
	}
	return s.reloadDictionary(ctx)
}

func (s *Service) newEntry(text string, entryType EntryType) (Entry, error) {
	text = strings.TrimSpace(text)
	if err := validateEntry(text, entryType); err != nil {
		return Entry{}, err
	}
	now := s.now()
	return Entry{
		ID:        newID(),
		Text:      text,
		Type:      entryType,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *Service) authorize(ctx context.Context, principal Principal) error {
	if s.auth == nil {
		return ErrUnauthorized
	}
	if err := s.auth.Authorize(ctx, principal, PermissionDictManage); err != nil {
		return ErrUnauthorized
	}
	return nil
}

func (s *Service) reloadDictionary(ctx context.Context) error {
	if s.reloader == nil {
		return nil
	}
	entries, err := s.store.ListActive(ctx)
	if err != nil {
		return err
	}
	return s.reloader.ReloadDictionary(ctx, entries)
}

func validateEntry(text string, entryType EntryType) error {
	if strings.TrimSpace(text) == "" {
		return ErrInvalidEntryText
	}
	if !entryType.Valid() {
		return ErrInvalidEntryType
	}
	return nil
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
