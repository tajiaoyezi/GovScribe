package doctype

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDefaultThresholdsValues(t *testing.T) {
	got := defaultThresholds()
	want := Thresholds{ConfidenceThreshold: 0.6, AmbiguityGap: 0.15, TopN: 3, MaxClarifyRounds: 3}
	if got != want {
		t.Fatalf("defaults = %#v, want %#v", got, want)
	}
}

func TestMemoryThresholdStoreSaveAndGet(t *testing.T) {
	store := NewMemoryThresholdStore()
	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != defaultThresholds() {
		t.Fatalf("get = %#v, want defaults", got)
	}

	updated := Thresholds{ConfidenceThreshold: 0.7, AmbiguityGap: 0.1, TopN: 5, MaxClarifyRounds: 2}
	if err := store.Save(context.Background(), updated); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err = store.Get(context.Background())
	if err != nil {
		t.Fatalf("get after save: %v", err)
	}
	if got != updated {
		t.Fatalf("get = %#v, want %#v (adjustable without code change)", got, updated)
	}
}

func TestPostgresThresholdStoreGetFallsBackToDefault(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresThresholdStore(db)
	mock.ExpectQuery("SELECT confidence_threshold, ambiguity_gap, top_n, max_clarify_rounds FROM doctype_routing_thresholds WHERE id = TRUE").
		WillReturnRows(sqlmock.NewRows(copyColumns(thresholdColumns)))

	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != defaultThresholds() {
		t.Fatalf("get = %#v, want defaults on empty table", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresThresholdStoreGetReadsRow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresThresholdStore(db)
	mock.ExpectQuery("SELECT confidence_threshold, ambiguity_gap, top_n, max_clarify_rounds FROM doctype_routing_thresholds WHERE id = TRUE").
		WillReturnRows(sqlmock.NewRows(copyColumns(thresholdColumns)).AddRow(0.55, 0.2, 4, 2))

	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	want := Thresholds{ConfidenceThreshold: 0.55, AmbiguityGap: 0.2, TopN: 4, MaxClarifyRounds: 2}
	if got != want {
		t.Fatalf("get = %#v, want %#v", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresThresholdStoreSaveUpserts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresThresholdStore(db)
	t0 := Thresholds{ConfidenceThreshold: 0.65, AmbiguityGap: 0.12, TopN: 3, MaxClarifyRounds: 4}
	mock.ExpectExec("INSERT INTO doctype_routing_thresholds").
		WithArgs(t0.ConfidenceThreshold, t0.AmbiguityGap, t0.TopN, t0.MaxClarifyRounds).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := store.Save(context.Background(), t0); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
