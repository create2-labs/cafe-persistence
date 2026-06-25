//go:build integration

package cpddl

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

func TestMigrateCPSchema_matchesGoldenIndexes(t *testing.T) {
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
	if err := MigrateCPSchema(db); err != nil {
		t.Fatalf("MigrateCPSchema: %v", err)
	}

	got, err := listPublicIndexes(db, CPTableNames)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
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
	path := filepath.Join(filepath.Dir(file), "..", "..", "testdata", "ddl", "cp_indexes.golden")
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
