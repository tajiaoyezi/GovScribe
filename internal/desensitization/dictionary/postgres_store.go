package dictionary

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var dictionaryEntrySelectColumns = []string{
	"id", "entry_text", "entry_type", "deleted", "created_at", "updated_at",
}

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Save(ctx context.Context, entry Entry) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO desensitization_dictionary_entries (
	id, entry_text, entry_type, deleted, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE SET
	entry_text = EXCLUDED.entry_text,
	entry_type = EXCLUDED.entry_type,
	deleted = EXCLUDED.deleted,
	created_at = EXCLUDED.created_at,
	updated_at = EXCLUDED.updated_at`,
		entry.ID, entry.Text, string(entry.Type), entry.Deleted, entry.CreatedAt, entry.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) Get(ctx context.Context, id string) (Entry, error) {
	row := s.db.QueryRowContext(ctx, selectDictionaryEntrySQL("WHERE id = $1"), id)
	return scanDictionaryEntry(row)
}

func (s *PostgresStore) ListActive(ctx context.Context) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx, selectDictionaryEntrySQL("WHERE deleted = FALSE ORDER BY entry_type, entry_text"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []Entry
	for rows.Next() {
		entry, err := scanDictionaryEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func selectDictionaryEntrySQL(suffix string) string {
	query := "SELECT " + strings.Join(dictionaryEntrySelectColumns, ", ") + " FROM desensitization_dictionary_entries"
	if suffix != "" {
		query += " " + suffix
	}
	return query
}

func dictionaryEntryColumns() []string {
	out := make([]string, len(dictionaryEntrySelectColumns))
	copy(out, dictionaryEntrySelectColumns)
	return out
}

type dictionaryEntryScanner interface {
	Scan(dest ...any) error
}

func scanDictionaryEntry(scanner dictionaryEntryScanner) (Entry, error) {
	var entry Entry
	var entryType string
	err := scanner.Scan(&entry.ID, &entry.Text, &entryType, &entry.Deleted, &entry.CreatedAt, &entry.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrEntryNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	entry.Type = EntryType(entryType)
	return entry, nil
}
