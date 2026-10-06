//go:build integration

package scanddl

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMigrateScanSchema_matchesGoldenIndexes(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		host := envOr("POSTGRES_HOST", "127.0.0.1")
		port := envOr("POSTGRES_PORT", "5432")
		user := envOr("POSTGRES_USER", "cafe")
		pass := envOr("POSTGRES_PASSWORD", "cafe")
		dbname := envOr("POSTGRES_DATABASE", "cafe")
		sslmode := envOr("POSTGRES_SSLMODE", "disable")
		dsn = "host=" + host + " port=" + port + " user=" + user + " password=" + pass + " dbname=" + dbname + " sslmode=" + sslmode
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := MigrateScanSchema(db); err != nil {
		t.Fatalf("MigrateScanSchema: %v", err)
	}

	got, err := listPublicIndexes(db, ScanTableNames)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	for _, legacy := range LegacyIndexNames {
		if slices.Contains(got, legacy) {
			t.Fatalf("legacy index %q must be absent after migration", legacy)
		}
	}
	for _, required := range RequiredIndexNames {
		if !slices.Contains(got, required) {
			t.Fatalf("required index %q missing; got %v", required, got)
		}
	}

	want := loadGoldenIndexNames(t)
	if !slices.Equal(got, want) {
		t.Fatalf("index snapshot mismatch:\n  got:  %v\n  want: %v", got, want)
	}
	assertWalletRecoveryColumns(t, db)
}

func assertWalletRecoveryColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	type row struct {
		ColumnName string
	}
	var rows []row
	err := db.Raw(`
SELECT column_name
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name = 'scan_results'
  AND column_name IN ('first_seen', 'last_seen', 'public_key_recovery')
ORDER BY column_name`).Scan(&rows).Error
	if err != nil {
		t.Fatalf("list columns: %v", err)
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.ColumnName)
	}
	if slices.Contains(names, "first_seen") || slices.Contains(names, "last_seen") {
		t.Fatalf("activity date columns must be absent, got %v", names)
	}
	if !slices.Contains(names, "public_key_recovery") {
		t.Fatalf("public_key_recovery missing, got %v", names)
	}

	var constraints []string
	err = db.Raw(`
SELECT conname
FROM pg_constraint
WHERE conname = 'chk_scan_results_public_key_recovery'`).Scan(&constraints).Error
	if err != nil {
		t.Fatalf("list constraint: %v", err)
	}
	if len(constraints) != 1 {
		t.Fatalf("public_key_recovery check constraint missing, got %v", constraints)
	}
}

func listPublicIndexes(db *gorm.DB, tables []string) ([]string, error) {
	type row struct {
		IndexName string
	}
	var rows []row
	err := db.Raw(`
SELECT indexname AS index_name
FROM pg_indexes
WHERE schemaname = 'public'
  AND tablename IN ?
ORDER BY tablename, indexname`, tables).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.IndexName)
	}
	sort.Strings(names)
	return names, nil
}

func loadGoldenIndexNames(t *testing.T) []string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "ddl", "scan_indexes.golden")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open golden file: %v", err)
	}
	defer f.Close()

	var names []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		names = append(names, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	sort.Strings(names)
	return names
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
