package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/tajiaoyezi/GovScribe/internal/llm"
)

func TestMemoryRouteConfigDefaultsFailClosedAndHardensClassified(t *testing.T) {
	store := NewMemoryRouteConfigStore()

	sensitive, err := store.GetPolicy(context.Background(), llm.ContentSecurityLevelSensitive)
	if err != nil {
		t.Fatalf("get sensitive policy: %v", err)
	}
	if sensitive.AllowDegradedPublic {
		t.Fatal("sensitive degraded public must default to false")
	}

	err = store.SavePolicy(context.Background(), RoutePolicy{
		Level:               llm.ContentSecurityLevelClassified,
		TargetNetwork:       llm.NetworkPublic,
		AllowDegradedPublic: true,
	})
	if err != nil {
		t.Fatalf("save classified policy: %v", err)
	}
	classified, err := store.GetPolicy(context.Background(), llm.ContentSecurityLevelClassified)
	if err != nil {
		t.Fatalf("get classified policy: %v", err)
	}
	if classified.TargetNetwork != llm.NetworkPrivate || classified.AllowDegradedPublic {
		t.Fatalf("classified policy = %#v, want private and no degraded public", classified)
	}

	err = store.SavePolicy(context.Background(), RoutePolicy{
		Level:               llm.ContentSecurityLevel("unknown"),
		TargetNetwork:       llm.NetworkPublic,
		AllowDegradedPublic: true,
	})
	if err != nil {
		t.Fatalf("save literal unknown policy: %v", err)
	}
	unknown, err := store.GetPolicy(context.Background(), llm.ContentSecurityLevel("unknown"))
	if err != nil {
		t.Fatalf("get literal unknown policy: %v", err)
	}
	if unknown.TargetNetwork != llm.NetworkPrivate || unknown.AllowDegradedPublic {
		t.Fatalf("unknown policy = %#v, want private and no degraded public", unknown)
	}
}

func TestPostgresRouteConfigStoreSavesAndReadsPolicy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresRouteConfigStore(db)
	now := time.Unix(1700000000, 0).UTC()
	policy := RoutePolicy{
		Level:               llm.ContentSecurityLevelSensitive,
		TargetNetwork:       llm.NetworkPublic,
		ModelConfigID:       "cfg-public",
		AllowDegradedPublic: true,
		UpdatedBy:           "admin-1",
		UpdatedAt:           now,
	}

	mock.ExpectExec("INSERT INTO security_classification_routes").
		WithArgs(string(policy.Level), string(policy.TargetNetwork), policy.ModelConfigID, policy.AllowDegradedPublic, policy.UpdatedBy, policy.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.SavePolicy(context.Background(), policy); err != nil {
		t.Fatalf("save policy: %v", err)
	}

	rows := sqlmock.NewRows(routePolicyColumns()).AddRow(
		string(policy.Level), string(policy.TargetNetwork), policy.ModelConfigID,
		policy.AllowDegradedPublic, policy.UpdatedBy, policy.UpdatedAt,
	)
	mock.ExpectQuery("SELECT classification, target_network, model_config_id, allow_degraded_public, updated_by, updated_at FROM security_classification_routes WHERE classification = \\$1").
		WithArgs(string(policy.Level)).
		WillReturnRows(rows)
	got, err := store.GetPolicy(context.Background(), policy.Level)
	if err != nil {
		t.Fatalf("get policy: %v", err)
	}
	if got != policy {
		t.Fatalf("policy = %#v, want %#v", got, policy)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestPostgresRouteConfigStoreHardensLiteralUnknownPolicy(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}
	defer db.Close()

	store := NewPostgresRouteConfigStore(db)
	now := time.Unix(1700000000, 0).UTC()
	policy := RoutePolicy{
		Level:               llm.ContentSecurityLevel("unknown"),
		TargetNetwork:       llm.NetworkPublic,
		ModelConfigID:       "cfg-public",
		AllowDegradedPublic: true,
		UpdatedBy:           "admin-1",
		UpdatedAt:           now,
	}

	mock.ExpectExec("INSERT INTO security_classification_routes").
		WithArgs("unknown", string(llm.NetworkPrivate), nil, false, policy.UpdatedBy, policy.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := store.SavePolicy(context.Background(), policy); err != nil {
		t.Fatalf("save unknown policy: %v", err)
	}

	rows := sqlmock.NewRows(routePolicyColumns()).AddRow(
		"unknown", string(llm.NetworkPublic), "cfg-public", true, policy.UpdatedBy, policy.UpdatedAt,
	)
	mock.ExpectQuery("SELECT classification, target_network, model_config_id, allow_degraded_public, updated_by, updated_at FROM security_classification_routes WHERE classification = \\$1").
		WithArgs("unknown").
		WillReturnRows(rows)
	got, err := store.GetPolicy(context.Background(), llm.ContentSecurityLevel("unknown"))
	if err != nil {
		t.Fatalf("get unknown policy: %v", err)
	}
	if got.TargetNetwork != llm.NetworkPrivate || got.AllowDegradedPublic || got.ModelConfigID != "" {
		t.Fatalf("unknown policy = %#v, want hardened private policy", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}

func TestRoutePolicyServiceRequiresManagePermission(t *testing.T) {
	svc := NewRoutePolicyService(NewMemoryRouteConfigStore(), denyingRouteAuthorizer{})

	_, err := svc.SavePolicy(context.Background(), Principal{ID: "user-1"}, RoutePolicy{
		Level:         llm.ContentSecurityLevelSensitive,
		TargetNetwork: llm.NetworkPublic,
	})
	if !errors.Is(err, ErrUnauthorizedRoutePolicy) {
		t.Fatalf("save error = %v, want ErrUnauthorizedRoutePolicy", err)
	}
	if _, err := svc.GetPolicy(context.Background(), Principal{ID: "user-1"}, llm.ContentSecurityLevelSensitive); !errors.Is(err, ErrUnauthorizedRoutePolicy) {
		t.Fatalf("get error = %v, want ErrUnauthorizedRoutePolicy", err)
	}
}

func TestRoutePolicyServiceStampsAdminAndSavesPolicy(t *testing.T) {
	auth := &recordingRouteAuthorizer{}
	svc := NewRoutePolicyService(NewMemoryRouteConfigStore(), auth)
	now := time.Unix(1700000000, 0).UTC()
	svc.now = func() time.Time { return now }

	saved, err := svc.SavePolicy(context.Background(), Principal{ID: "admin-1"}, RoutePolicy{
		Level:         llm.ContentSecurityLevelSensitive,
		TargetNetwork: llm.NetworkPrivate,
		ModelConfigID: "cfg-private",
	})
	if err != nil {
		t.Fatalf("save policy: %v", err)
	}
	if auth.lastPermission != PermissionRoutePolicyManage {
		t.Fatalf("permission = %q, want route.policy.manage", auth.lastPermission)
	}
	if saved.UpdatedBy != "admin-1" || !saved.UpdatedAt.Equal(now) {
		t.Fatalf("saved policy audit fields = %#v, want admin id and timestamp", saved)
	}

	got, err := svc.GetPolicy(context.Background(), Principal{ID: "admin-1"}, llm.ContentSecurityLevelSensitive)
	if err != nil {
		t.Fatalf("get policy: %v", err)
	}
	if got.TargetNetwork != llm.NetworkPrivate || got.ModelConfigID != "cfg-private" {
		t.Fatalf("stored policy = %#v, want private cfg", got)
	}
}

type recordingRouteAuthorizer struct {
	lastPermission Permission
}

func (a *recordingRouteAuthorizer) Authorize(_ context.Context, _ Principal, permission Permission) error {
	a.lastPermission = permission
	return nil
}

type denyingRouteAuthorizer struct{}

func (denyingRouteAuthorizer) Authorize(context.Context, Principal, Permission) error {
	return errors.New("denied")
}
