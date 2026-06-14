package dictionary

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestImportEntriesRequiresDictManagePermissionAndStoresTypedEntries(t *testing.T) {
	store := NewMemoryStore()
	auth := &recordingAuthorizer{}
	svc := NewService(store, auth)
	svc.now = func() time.Time { return time.Unix(1700000000, 0).UTC() }

	entries, err := svc.ImportEntries(context.Background(), Principal{ID: "admin-1"}, []ImportEntry{
		{Text: "市发展改革委", Type: EntryTypeOrganization},
		{Text: "张三", Type: EntryTypePerson},
		{Text: "春风行动", Type: EntryTypeProjectCode},
		{Text: "绝密项目", Type: EntryTypeSecretKeywordBlacklist},
	})
	if err != nil {
		t.Fatalf("import entries: %v", err)
	}
	if auth.lastPermission != PermissionDictManage {
		t.Fatalf("permission = %q, want dict.manage", auth.lastPermission)
	}
	if len(entries) != 4 {
		t.Fatalf("entry count = %d, want 4", len(entries))
	}
	for _, entry := range entries {
		if entry.ID == "" {
			t.Fatalf("entry missing id: %#v", entry)
		}
		if entry.CreatedAt.IsZero() || entry.UpdatedAt.IsZero() {
			t.Fatalf("entry timestamps must be set: %#v", entry)
		}
		if entry.Deleted {
			t.Fatalf("new entry must not be deleted: %#v", entry)
		}
	}

	listed, err := svc.ListEntries(context.Background(), Principal{ID: "admin-1"})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(listed) != 4 {
		t.Fatalf("listed count = %d, want 4", len(listed))
	}
}

func TestUnauthorizedPrincipalCannotMaintainDictionary(t *testing.T) {
	svc := NewService(NewMemoryStore(), denyingAuthorizer{})

	_, err := svc.CreateEntry(context.Background(), Principal{ID: "user-1"}, CreateEntryRequest{
		Text: "市财政局",
		Type: EntryTypeOrganization,
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("create error = %v, want ErrUnauthorized", err)
	}

	if err := svc.DeleteEntry(context.Background(), Principal{ID: "user-1"}, "entry-1"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("delete error = %v, want ErrUnauthorized", err)
	}
}

func TestDeleteEntrySoftDeletesAndHidesFromActiveList(t *testing.T) {
	store := NewMemoryStore()
	svc := NewService(store, allowAuthorizer{})
	created, err := svc.CreateEntry(context.Background(), Principal{ID: "admin-1"}, CreateEntryRequest{
		Text: "市财政局",
		Type: EntryTypeOrganization,
	})
	if err != nil {
		t.Fatalf("create entry: %v", err)
	}

	if err := svc.DeleteEntry(context.Background(), Principal{ID: "admin-1"}, created.ID); err != nil {
		t.Fatalf("delete entry: %v", err)
	}
	listed, err := svc.ListEntries(context.Background(), Principal{ID: "admin-1"})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("listed count = %d, want deleted entry hidden", len(listed))
	}

	deleted, err := store.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("get deleted entry from store: %v", err)
	}
	if !deleted.Deleted {
		t.Fatalf("deleted flag = false, want true: %#v", deleted)
	}
}

func TestInvalidEntryTypeIsRejected(t *testing.T) {
	svc := NewService(NewMemoryStore(), allowAuthorizer{})

	_, err := svc.CreateEntry(context.Background(), Principal{ID: "admin-1"}, CreateEntryRequest{
		Text: "市财政局",
		Type: EntryType("unknown"),
	})
	if !errors.Is(err, ErrInvalidEntryType) {
		t.Fatalf("create error = %v, want ErrInvalidEntryType", err)
	}
}

type recordingAuthorizer struct {
	lastPermission Permission
}

func (a *recordingAuthorizer) Authorize(_ context.Context, _ Principal, permission Permission) error {
	a.lastPermission = permission
	return nil
}

type allowAuthorizer struct{}

func (allowAuthorizer) Authorize(context.Context, Principal, Permission) error {
	return nil
}

type denyingAuthorizer struct{}

func (denyingAuthorizer) Authorize(context.Context, Principal, Permission) error {
	return errors.New("denied")
}
