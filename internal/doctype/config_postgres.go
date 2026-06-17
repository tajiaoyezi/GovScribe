package doctype

import (
	"context"
	"database/sql"
	"errors"
)

var requiredSlotColumns = []string{"doctype", "direction", "slot"}

var thresholdColumns = []string{"confidence_threshold", "ambiguity_gap", "top_n", "max_clarify_rounds"}

// PostgresSlotStore 以 PostgreSQL 为权威源存取必需要素清单。
type PostgresSlotStore struct {
	db *sql.DB
}

// NewPostgresSlotStore 构造基于给定连接的必需要素清单存储。
func NewPostgresSlotStore(db *sql.DB) *PostgresSlotStore {
	return &PostgresSlotStore{db: db}
}

// RequiredSlots 返回 (文种, 行文方向) 的必需要素集合：方向精确匹配项与方向无关项并集，保序去重。
func (s *PostgresSlotStore) RequiredSlots(ctx context.Context, doctype string, direction WritingDirection) ([]RequiredSlot, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT slot FROM doctype_required_slots WHERE doctype = $1 AND (direction = $2 OR direction = '') ORDER BY direction, slot`,
		doctype, string(direction))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var slots []RequiredSlot
	seen := make(map[RequiredSlot]bool)
	for rows.Next() {
		var slot string
		if err := rows.Scan(&slot); err != nil {
			return nil, err
		}
		if rs := RequiredSlot(slot); !seen[rs] {
			seen[rs] = true
			slots = append(slots, rs)
		}
	}
	return slots, rows.Err()
}

// List 返回全部必需要素配置（按文种、方向、要素排序）。
func (s *PostgresSlotStore) List(ctx context.Context) ([]SlotRequirement, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT doctype, direction, slot FROM doctype_required_slots ORDER BY doctype, direction, slot`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []SlotRequirement
	for rows.Next() {
		var doctype, direction, slot string
		if err := rows.Scan(&doctype, &direction, &slot); err != nil {
			return nil, err
		}
		items = append(items, SlotRequirement{Doctype: doctype, Direction: WritingDirection(direction), Slot: RequiredSlot(slot)})
	}
	return items, rows.Err()
}

// SeedRequiredSlots 幂等地将默认必需要素清单写入 Postgres；已存在项保留管理员的维护值。
func SeedRequiredSlots(ctx context.Context, db *sql.DB, items []SlotRequirement) error {
	for _, it := range items {
		if _, err := db.ExecContext(ctx, `
INSERT INTO doctype_required_slots (doctype, direction, slot)
VALUES ($1, $2, $3)
ON CONFLICT (doctype, direction, slot) DO NOTHING`,
			it.Doctype, string(it.Direction), string(it.Slot)); err != nil {
			return err
		}
	}
	return nil
}

// PostgresThresholdStore 以 PostgreSQL 为权威源存取可调阈值参数（单行配置）。
type PostgresThresholdStore struct {
	db *sql.DB
}

// NewPostgresThresholdStore 构造基于给定连接的阈值参数存储。
func NewPostgresThresholdStore(db *sql.DB) *PostgresThresholdStore {
	return &PostgresThresholdStore{db: db}
}

// Get 返回当前阈值参数；未初始化（无行）时回退至 MVP 默认值，与 c02 路由配置同口径。
func (s *PostgresThresholdStore) Get(ctx context.Context) (Thresholds, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT confidence_threshold, ambiguity_gap, top_n, max_clarify_rounds FROM doctype_routing_thresholds WHERE id = TRUE`)
	var t Thresholds
	err := row.Scan(&t.ConfidenceThreshold, &t.AmbiguityGap, &t.TopN, &t.MaxClarifyRounds)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultThresholds(), nil
	}
	if err != nil {
		return Thresholds{}, err
	}
	return t, nil
}

// Save 覆盖阈值参数（单行 upsert，不改代码即可调整）。
func (s *PostgresThresholdStore) Save(ctx context.Context, t Thresholds) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO doctype_routing_thresholds (id, confidence_threshold, ambiguity_gap, top_n, max_clarify_rounds, updated_at)
VALUES (TRUE, $1, $2, $3, $4, now())
ON CONFLICT (id) DO UPDATE SET
	confidence_threshold = EXCLUDED.confidence_threshold,
	ambiguity_gap = EXCLUDED.ambiguity_gap,
	top_n = EXCLUDED.top_n,
	max_clarify_rounds = EXCLUDED.max_clarify_rounds,
	updated_at = EXCLUDED.updated_at`,
		t.ConfidenceThreshold, t.AmbiguityGap, t.TopN, t.MaxClarifyRounds)
	return err
}

// SeedThresholds 幂等地写入默认阈值（已初始化则保留管理员的维护值）。
func SeedThresholds(ctx context.Context, db *sql.DB, t Thresholds) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO doctype_routing_thresholds (id, confidence_threshold, ambiguity_gap, top_n, max_clarify_rounds)
VALUES (TRUE, $1, $2, $3, $4)
ON CONFLICT (id) DO NOTHING`,
		t.ConfidenceThreshold, t.AmbiguityGap, t.TopN, t.MaxClarifyRounds)
	return err
}
