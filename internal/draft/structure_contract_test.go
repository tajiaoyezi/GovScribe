package draft

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/tajiaoyezi/GovScribe/internal/doctype"
)

func TestDefaultStructureContractsCoverNineHighFreqDoctypes(t *testing.T) {
	contracts := DefaultStructureContracts()
	if len(contracts) != 9 {
		t.Fatalf("default contracts len = %d, want 9", len(contracts))
	}
	byDoctype := map[string]StructureContract{}
	for _, c := range contracts {
		if c.Doctype == "" {
			t.Fatalf("empty doctype in %#v", c)
		}
		if c.TitleRule == "" || c.SalutationRule == "" || c.BodyStructure == nil || c.SignatureRule == "" {
			t.Fatalf("contract for %s misses structural fields: %#v", c.Doctype, c)
		}
		if len(c.RequiredSlots) == 0 {
			t.Fatalf("contract for %s has no required slots", c.Doctype)
		}
		if len(c.RedlineRules) == 0 {
			t.Fatalf("contract for %s has no redline rules", c.Doctype)
		}
		byDoctype[c.Doctype] = c
	}

	wantDoctypes := []string{"通知", "请示", "报告", "函", "会议纪要", "通报", "批复", "讲话稿", "方案"}
	for _, doctypeName := range wantDoctypes {
		if _, ok := byDoctype[doctypeName]; !ok {
			t.Fatalf("missing default contract for %s", doctypeName)
		}
	}
	if byDoctype["请示"].Direction != doctype.DirectionUpward {
		t.Fatalf("请示 direction = %q, want upward", byDoctype["请示"].Direction)
	}
	if byDoctype["报告"].Direction != doctype.DirectionUpward {
		t.Fatalf("报告 direction = %q, want upward", byDoctype["报告"].Direction)
	}
	if byDoctype["通知"].Direction != doctype.DirectionDownward {
		t.Fatalf("通知 direction = %q, want downward", byDoctype["通知"].Direction)
	}
	if byDoctype["通报"].Direction != doctype.DirectionDownward {
		t.Fatalf("通报 direction = %q, want downward", byDoctype["通报"].Direction)
	}
	if byDoctype["批复"].Direction != doctype.DirectionDownward {
		t.Fatalf("批复 direction = %q, want downward", byDoctype["批复"].Direction)
	}
	if byDoctype["函"].Direction != doctype.DirectionHorizontal {
		t.Fatalf("函 direction = %q, want horizontal", byDoctype["函"].Direction)
	}
}

func TestMemoryStructureContractStoreGetsAndCopiesDefaults(t *testing.T) {
	store := NewMemoryStructureContractStore()

	got, err := store.Get(context.Background(), "请示")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Doctype != "请示" || got.Direction != doctype.DirectionUpward {
		t.Fatalf("got = %#v, want 请示/upward", got)
	}
	got.RequiredSlots[0] = doctype.RequiredSlot("污染")
	again, err := store.Get(context.Background(), "请示")
	if err != nil {
		t.Fatalf("get again: %v", err)
	}
	if again.RequiredSlots[0] == doctype.RequiredSlot("污染") {
		t.Fatal("store returned shared required slot slice")
	}

	if _, err := store.Get(context.Background(), "命令"); !errors.Is(err, ErrStructureContractNotFound) {
		t.Fatalf("missing err = %v, want ErrStructureContractNotFound", err)
	}
}

func TestPostgresStructureContractStoreGetAndList(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()
	store := NewPostgresStructureContractStore(db)

	rows := sqlmock.NewRows(copyColumns(structureContractColumns)).AddRow(
		"请示",
		"upward",
		"关于 + 事由 + 请示",
		"主送上级机关",
		"主送机关为上级机关",
		[]byte(`["缘由","事项"]`),
		[]byte(`["发文单位","主送机关","事由","关键事项"]`),
		"妥否，请批示。",
		"发文单位 + 日期",
		[]byte(`["呈请口吻"]`),
		[]byte(`["不得臆造事实"]`),
		"templates/请示/v1.md",
		"v1",
	)
	mock.ExpectQuery("SELECT .* FROM high_freq_doctype_structure_contracts WHERE doctype = \\$1").
		WithArgs("请示").
		WillReturnRows(rows)

	got, err := store.Get(context.Background(), "请示")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Doctype != "请示" || got.Direction != doctype.DirectionUpward || got.TemplateObjectKey != "templates/请示/v1.md" {
		t.Fatalf("got = %#v, want parsed 请示 contract", got)
	}
	if len(got.BodyStructure) != 2 || got.RequiredSlots[1] != doctype.SlotRecipient || got.ToneRules[0] != "呈请口吻" {
		t.Fatalf("contract arrays not parsed: %#v", got)
	}

	listRows := sqlmock.NewRows(copyColumns(structureContractColumns)).
		AddRow("函", "horizontal", "关于 + 事由 + 函", "平行机关", "同级或不相隶属机关", []byte(`["缘由"]`), []byte(`["发文单位"]`), "特此函达。", "署名日期", []byte(`["商洽口吻"]`), []byte(`["不得臆造事实"]`), "", "").
		AddRow("通知", "downward", "关于 + 事由 + 通知", "下级机关", "主送下级或相关单位", []byte(`["事项"]`), []byte(`["发文单位"]`), "请遵照执行。", "署名日期", []byte(`["部署口吻"]`), []byte(`["不得臆造事实"]`), "", "")
	mock.ExpectQuery("SELECT .* FROM high_freq_doctype_structure_contracts ORDER BY doctype").
		WillReturnRows(listRows)
	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Doctype != "函" || list[1].Doctype != "通知" {
		t.Fatalf("list = %#v", list)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestSeedStructureContractsInsertsWithoutClassificationFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	contracts := []StructureContract{
		{
			Doctype:        "通知",
			Direction:      doctype.DirectionDownward,
			TitleRule:      "关于 + 事由 + 通知",
			SalutationRule: "主送相关单位",
			RecipientRule:  "主送机关为下级或相关单位",
			BodyStructure:  []string{"缘由", "事项"},
			RequiredSlots:  []doctype.RequiredSlot{doctype.SlotIssuer, doctype.SlotRecipient},
			ClosingRule:    "请遵照执行。",
			SignatureRule:  "发文单位 + 日期",
			ToneRules:      []string{"部署口吻"},
			RedlineRules:   []string{"不得臆造事实"},
		},
	}
	mock.ExpectExec("INSERT INTO high_freq_doctype_structure_contracts").
		WithArgs("通知", string(doctype.DirectionDownward), "关于 + 事由 + 通知", "主送相关单位", "主送机关为下级或相关单位", sqlmock.AnyArg(), sqlmock.AnyArg(), "请遵照执行。", "发文单位 + 日期", sqlmock.AnyArg(), sqlmock.AnyArg(), "", "").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := SeedStructureContracts(context.Background(), db, contracts); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
