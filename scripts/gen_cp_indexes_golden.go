// One-off helper to refresh testdata/ddl/cp_indexes.golden after CP DDL changes.
//go:build ignore

package main

import (
	"fmt"
	"os"
	"sort"

	"cafe-persistence/internal/cpddl"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		env("POSTGRES_HOST", "127.0.0.1"),
		env("POSTGRES_PORT", "5432"),
		env("POSTGRES_USER", "cafe"),
		env("POSTGRES_PASSWORD", "cafe"),
		env("POSTGRES_DATABASE", "cafe"),
		env("POSTGRES_SSLMODE", "disable"),
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "open: %v\n", err)
		os.Exit(1)
	}
	if err := cpddl.MigrateCPSchema(db); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	type row struct {
		IndexName string
	}
	var rows []row
	if err := db.Raw(`
SELECT indexname AS index_name
FROM pg_indexes
WHERE schemaname = 'public'
  AND tablename IN ('crypto_policy_drafts', 'crypto_policies', 'draft_persist_state')
ORDER BY tablename, indexname`).Scan(&rows).Error; err != nil {
		fmt.Fprintf(os.Stderr, "query: %v\n", err)
		os.Exit(1)
	}
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.IndexName
	}
	sort.Strings(names)
	out, err := os.Create("testdata/ddl/cp_indexes.golden")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create: %v\n", err)
		os.Exit(1)
	}
	defer out.Close()
	fmt.Fprintln(out, "# pg_indexes on crypto_policy_drafts, crypto_policies, draft_persist_state (ADR §8.4)")
	for _, n := range names {
		fmt.Fprintln(out, n)
	}
}

func env(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}
