package doctype

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDefaultRequiredSlotsCoverNineDeepDoctypes(t *testing.T) {
	byDoctype := make(map[string]int)
	for _, it := range defaultRequiredSlots() {
		byDoctype[it.Doctype]++
	}
	for _, d := range deepDoctypeOrder {
		if byDoctype[d] == 0 {
			t.Fatalf("deep doctype %q has no required slots", d)
		}
	}
	if len(byDoctype) != len(deepDoctypeOrder) {
		t.Fatalf("doctypes with slots = %d, want %d", len(byDoctype), len(deepDoctypeOrder))
	}
}

func TestMemorySlotStoreReturnsRequiredSlots(t *testing.T) {
	store := NewMemorySlotStore()
	got, err := store.RequiredSlots(context.Background(), "请示", DirectionUpward)
	if err != nil {
		t.Fatalf("required slots: %v", err)
	}
	want := []RequiredSlot{SlotIssuer, SlotRecipient, SlotSubject, SlotKeyMatter}
	if len(got) != len(want) {
		t.Fatalf("slots = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("slots = %#v, want %#v", got, want)
		}
	}
}

func TestMemorySlotStoreUnknownDoctypeReturnsEmpty(t *testing.T) {
	store := NewMemorySlotStore()
	got, err := store.RequiredSlots(context.Background(), "未规划文种", DirectionUnspecified)
	if err != nil {
		t.Fatalf("required slots: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("slots = %#v, want empty", got)
	}
}

func TestPostgresSlotStoreReturnsRequiredSlots(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresSlotStore(db)
	rows := sqlmock.NewRows([]string{"slot"}).
		AddRow(string(SlotIssuer)).
		AddRow(string(SlotRecipient)).
		AddRow(string(SlotRecipient)) // 重复应被去重
	mock.ExpectQuery("SELECT slot FROM doctype_required_slots WHERE doctype = \\$1").
		WithArgs("请示", "upward").
		WillReturnRows(rows)

	got, err := store.RequiredSlots(context.Background(), "请示", DirectionUpward)
	if err != nil {
		t.Fatalf("required slots: %v", err)
	}
	want := []RequiredSlot{SlotIssuer, SlotRecipient}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("slots = %#v, want %#v", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresSlotStoreLists(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresSlotStore(db)
	rows := sqlmock.NewRows(requiredSlotColumnList()).
		AddRow("请示", "", string(SlotIssuer)).
		AddRow("请示", "", string(SlotRecipient))
	mock.ExpectQuery("SELECT doctype, direction, slot FROM doctype_required_slots ORDER BY doctype, direction, slot").
		WillReturnRows(rows)

	got, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0].Slot != SlotIssuer || got[1].Slot != SlotRecipient {
		t.Fatalf("list = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSeedRequiredSlotsUpserts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	items := []SlotRequirement{
		{Doctype: "请示", Slot: SlotIssuer},
		{Doctype: "请示", Slot: SlotRecipient},
	}
	for _, it := range items {
		mock.ExpectExec("INSERT INTO doctype_required_slots").
			WithArgs(it.Doctype, string(it.Direction), string(it.Slot)).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	if err := SeedRequiredSlots(context.Background(), db, items); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
