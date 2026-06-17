package doctype

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var matrixColumns = []string{"doctype", "subtype", "capability_tier", "is_starred_rare"}

// PostgresMatrixStore 以 PostgreSQL 为权威源存取文种能力档分级表。
type PostgresMatrixStore struct {
	db *sql.DB
}

// NewPostgresMatrixStore 构造基于给定连接的分级表存储。
func NewPostgresMatrixStore(db *sql.DB) *PostgresMatrixStore {
	return &PostgresMatrixStore{db: db}
}

// List 返回分级表全部记录（按文种、子类排序）。
func (s *PostgresMatrixStore) List(ctx context.Context) ([]MatrixEntry, error) {
	rows, err := s.db.QueryContext(ctx, selectMatrixSQL("ORDER BY doctype, subtype"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []MatrixEntry
	for rows.Next() {
		entry, err := scanMatrixEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// Lookup 精确查找 (文种, 子类) 记录，缺失返回 ErrMatrixEntryNotFound。
func (s *PostgresMatrixStore) Lookup(ctx context.Context, doctype, subtype string) (MatrixEntry, error) {
	row := s.db.QueryRowContext(ctx, selectMatrixSQL("WHERE doctype = $1 AND subtype = $2"), doctype, subtype)
	entry, err := scanMatrixEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return MatrixEntry{}, ErrMatrixEntryNotFound
	}
	if err != nil {
		return MatrixEntry{}, err
	}
	return entry, nil
}

// SeedMatrix 幂等地将默认分级表写入 Postgres；已存在的 (文种, 子类) 保留管理员的维护值。
// 默认种子的权威来源为 Go 侧 DefaultMatrix（PRD 文种覆盖矩阵），迁移脚本仅建表不内联种子，
// 与 c02 密级路由「迁移建表 + Go 默认值」同一口径，避免 SQL 与 Go 种子漂移。
func SeedMatrix(ctx context.Context, db *sql.DB, entries []MatrixEntry) error {
	for _, e := range entries {
		if _, err := db.ExecContext(ctx, `
INSERT INTO doctype_capability_matrix (doctype, subtype, capability_tier, is_starred_rare)
VALUES ($1, $2, $3, $4)
ON CONFLICT (doctype, subtype) DO NOTHING`,
			e.Doctype, e.Subtype, string(e.Tier), e.IsStarredRare); err != nil {
			return err
		}
	}
	return nil
}

func selectMatrixSQL(suffix string) string {
	query := "SELECT " + strings.Join(matrixColumns, ", ") + " FROM doctype_capability_matrix"
	if suffix != "" {
		query += " " + suffix
	}
	return query
}

func matrixEntryColumns() []string {
	out := make([]string, len(matrixColumns))
	copy(out, matrixColumns)
	return out
}

type matrixScanner interface {
	Scan(dest ...any) error
}

func scanMatrixEntry(scanner matrixScanner) (MatrixEntry, error) {
	var entry MatrixEntry
	var tier string
	if err := scanner.Scan(&entry.Doctype, &entry.Subtype, &tier, &entry.IsStarredRare); err != nil {
		return MatrixEntry{}, err
	}
	entry.Tier = CapabilityTier(tier)
	return entry, nil
}
