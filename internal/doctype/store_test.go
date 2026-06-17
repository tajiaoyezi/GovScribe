package doctype

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMemoryMatrixStoreLooksUpAndReportsMissing(t *testing.T) {
	store := NewMemoryMatrixStore()

	entry, err := store.Lookup(context.Background(), "请示", "组织成立")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if entry.Tier != TierDeep || entry.IsStarredRare {
		t.Fatalf("请示-组织成立 = %#v, want deep non-starred", entry)
	}

	if _, err := store.Lookup(context.Background(), "请示", "不存在的子类"); !errors.Is(err, ErrMatrixEntryNotFound) {
		t.Fatalf("lookup missing error = %v, want ErrMatrixEntryNotFound", err)
	}
}

func TestMemoryMatrixStoreListReturnsDefaults(t *testing.T) {
	store := NewMemoryMatrixStore()
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != len(defaultMatrix()) {
		t.Fatalf("list len = %d, want %d", len(entries), len(defaultMatrix()))
	}
}

func TestPostgresMatrixStoreLooksUpAndMapsMissingRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresMatrixStore(db)

	rows := sqlmock.NewRows(copyColumns(matrixColumns)).AddRow("通知", "举办比赛", "deep_generation", true)
	mock.ExpectQuery("SELECT doctype, subtype, capability_tier, is_starred_rare FROM doctype_capability_matrix WHERE doctype = \\$1 AND subtype = \\$2").
		WithArgs("通知", "举办比赛").
		WillReturnRows(rows)

	entry, err := store.Lookup(context.Background(), "通知", "举办比赛")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	want := MatrixEntry{Doctype: "通知", Subtype: "举办比赛", Tier: TierDeep, IsStarredRare: true}
	if entry != want {
		t.Fatalf("entry = %#v, want %#v", entry, want)
	}

	mock.ExpectQuery("SELECT doctype, subtype, capability_tier, is_starred_rare FROM doctype_capability_matrix WHERE doctype = \\$1 AND subtype = \\$2").
		WithArgs("通知", "缺失").
		WillReturnRows(sqlmock.NewRows(copyColumns(matrixColumns)))
	if _, err := store.Lookup(context.Background(), "通知", "缺失"); !errors.Is(err, ErrMatrixEntryNotFound) {
		t.Fatalf("lookup missing error = %v, want ErrMatrixEntryNotFound", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresMatrixStoreListsEntries(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresMatrixStore(db)
	rows := sqlmock.NewRows(copyColumns(matrixColumns)).
		AddRow("函", "复函", "deep_generation", true).
		AddRow("函", "邀请", "deep_generation", false)
	mock.ExpectQuery("SELECT doctype, subtype, capability_tier, is_starred_rare FROM doctype_capability_matrix ORDER BY doctype, subtype").
		WillReturnRows(rows)

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []MatrixEntry{
		{Doctype: "函", Subtype: "复函", Tier: TierDeep, IsStarredRare: true},
		{Doctype: "函", Subtype: "邀请", Tier: TierDeep},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("list = %#v, want %#v", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSeedMatrixUpsertsEntries(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	entries := []MatrixEntry{
		{Doctype: "通知", Subtype: "召开会议", Tier: TierDeep},
		{Doctype: "命令", Tier: TierTemplateAssist},
	}
	for _, e := range entries {
		mock.ExpectExec("INSERT INTO doctype_capability_matrix").
			WithArgs(e.Doctype, e.Subtype, string(e.Tier), e.IsStarredRare).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}

	if err := SeedMatrix(context.Background(), db, entries); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
