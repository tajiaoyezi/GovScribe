package dictionary

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresStoreSavesAndListsActiveEntries(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	now := time.Unix(1700000000, 0).UTC()
	entry := Entry{
		ID: "entry-1", Text: "市财政局", Type: EntryTypeOrganization,
		CreatedAt: now, UpdatedAt: now,
	}

	mock.ExpectExec("INSERT INTO desensitization_dictionary_entries").
		WithArgs(entry.ID, entry.Text, string(entry.Type), entry.Deleted, entry.CreatedAt, entry.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.Save(context.Background(), entry); err != nil {
		t.Fatalf("save: %v", err)
	}

	rows := sqlmock.NewRows(dictionaryEntryColumns()).AddRow(
		entry.ID, entry.Text, string(entry.Type), entry.Deleted, entry.CreatedAt, entry.UpdatedAt,
	)
	mock.ExpectQuery("SELECT id, entry_text, entry_type, deleted, created_at, updated_at FROM desensitization_dictionary_entries WHERE deleted = FALSE ORDER BY entry_type, entry_text").
		WillReturnRows(rows)
	got, err := store.ListActive(context.Background())
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(got) != 1 || got[0] != entry {
		t.Fatalf("entries = %#v, want %#v", got, []Entry{entry})
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresStoreGetsEntryAndMapsMissingRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresStore(db)
	mock.ExpectQuery("SELECT id, entry_text, entry_type, deleted, created_at, updated_at FROM desensitization_dictionary_entries WHERE id = \\$1").
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows(dictionaryEntryColumns()))

	if _, err := store.Get(context.Background(), "missing"); err != ErrEntryNotFound {
		t.Fatalf("get error = %v, want ErrEntryNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
