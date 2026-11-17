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
	assertWalletTransitionID(t, db)
	assertQualificationColumns(t, db)
	assertAccountTypeConstraint(t, db)
	assertRelatedAddressTable(t, db)
	assertRelatedAddressConstraints(t, db)
}

func assertWalletTransitionID(t *testing.T, db *gorm.DB) {
	t.Helper()
	type col struct {
		IsNullable string
		DataType   string
	}
	var wallet col
	err := db.Raw(`
SELECT is_nullable, data_type
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name = 'scan_results'
  AND column_name = 'transition_id'`).Scan(&wallet).Error
	if err != nil {
		t.Fatalf("read scan_results.transition_id: %v", err)
	}
	if wallet.DataType != "text" || wallet.IsNullable != "YES" {
		t.Fatalf("scan_results.transition_id must be nullable text, got %+v", wallet)
	}

	var tlsCount int64
	err = db.Raw(`
SELECT COUNT(*)
FROM information_schema.columns
WHERE table_schema = 'public'
  AND table_name = 'tls_scan_results'
  AND column_name = 'transition_id'`).Scan(&tlsCount).Error
	if err != nil {
		t.Fatalf("read tls_scan_results.transition_id: %v", err)
	}
	if tlsCount != 0 {
		t.Fatalf("tls_scan_results must not have transition_id, count %d", tlsCount)
	}
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

type columnInfo struct {
	DataType      string
	IsNullable    string
	ColumnDefault *string
	CharMaxLength *int
}

func readColumn(t *testing.T, db *gorm.DB, table, column string) (columnInfo, bool) {
	t.Helper()
	var rows []columnInfo
	err := db.Raw(`
SELECT data_type, is_nullable, column_default, character_maximum_length AS char_max_length
FROM information_schema.columns
WHERE table_schema = 'public' AND table_name = ? AND column_name = ?`, table, column).Scan(&rows).Error
	if err != nil {
		t.Fatalf("read %s.%s: %v", table, column, err)
	}
	if len(rows) == 0 {
		return columnInfo{}, false
	}
	return rows[0], true
}

// wantColumn checks a column's type, nullability, and default. wantDefault "" means no default;
// "”" means the empty-string default.
func wantColumn(t *testing.T, db *gorm.DB, table, column, dataType string, maxLen int, nullable bool, wantDefault string) {
	t.Helper()
	got, ok := readColumn(t, db, table, column)
	if !ok {
		t.Fatalf("%s.%s missing", table, column)
	}
	if got.DataType != dataType {
		t.Fatalf("%s.%s type = %q, want %q", table, column, got.DataType, dataType)
	}
	if maxLen > 0 && (got.CharMaxLength == nil || *got.CharMaxLength != maxLen) {
		t.Fatalf("%s.%s length = %v, want %d", table, column, got.CharMaxLength, maxLen)
	}
	if (got.IsNullable == "YES") != nullable {
		t.Fatalf("%s.%s nullable = %s, want %v", table, column, got.IsNullable, nullable)
	}
	switch {
	case wantDefault == "" && got.ColumnDefault != nil:
		t.Fatalf("%s.%s default = %q, want none", table, column, *got.ColumnDefault)
	case wantDefault != "" && (got.ColumnDefault == nil || !strings.HasPrefix(*got.ColumnDefault, wantDefault)):
		t.Fatalf("%s.%s default = %v, want prefix %q", table, column, got.ColumnDefault, wantDefault)
	}
}

// ET8 (OC-P21): the five qualification columns of scan_results.
func assertQualificationColumns(t *testing.T, db *gorm.DB) {
	t.Helper()
	wantColumn(t, db, "scan_results", "account_type", "character varying", 20, false, "''")
	wantColumn(t, db, "scan_results", "chains", "text", 0, false, "''")
	wantColumn(t, db, "scan_results", "unobserved_chain_ids", "text", 0, false, "''")
	wantColumn(t, db, "scan_results", "anomalies", "text", 0, false, "''")
	wantColumn(t, db, "scan_results", "observed_at", "timestamp with time zone", 0, true, "")
}

// probe runs one statement in a transaction that is always rolled back and returns its error.
func probe(t *testing.T, db *gorm.DB, query string, args ...any) error {
	t.Helper()
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	defer tx.Rollback()
	return tx.Exec(query, args...).Error
}

func wantAccepted(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s must be accepted, got %v", what, err)
	}
}

func wantRefused(t *testing.T, err error, constraint, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s must be refused by %s", what, constraint)
	}
	if !strings.Contains(err.Error(), constraint) {
		t.Fatalf("%s must be refused by %s, got %v", what, constraint, err)
	}
}

// ET8 (OC-P21): chk_scan_results_account_type.
func assertAccountTypeConstraint(t *testing.T, db *gorm.DB) {
	t.Helper()
	const insert = `
INSERT INTO scan_results (id, user_id, address, type, algorithm, nist_level, key_exposed, is_eoa, is_erc4337, risk_score, account_type)
VALUES ('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-0000000000b1', '0xaaaa', 'unknown', '', 0, false, false, false, 0, ?)`
	for _, ok := range []string{"", "eoa", "contract", "blank"} {
		wantAccepted(t, probe(t, db, insert, ok), "account_type "+ok)
	}
	for _, bad := range []string{"unknown", "EOA", "not_deployed"} {
		wantRefused(t, probe(t, db, insert, bad), "chk_scan_results_account_type", "account_type "+bad)
	}
}

// ET8 (OC-P21): the scan_related_addresses table, its primary key, and its soft-delete column.
func assertRelatedAddressTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	const table = "scan_related_addresses"
	wantColumn(t, db, table, "scan_id", "character", 36, false, "")
	wantColumn(t, db, table, "chain_id", "bigint", 0, false, "")
	wantColumn(t, db, table, "role", "character varying", 32, false, "")
	wantColumn(t, db, table, "address", "text", 0, false, "")
	wantColumn(t, db, table, "enrichment_id", "character", 36, true, "")
	wantColumn(t, db, table, "creation_tx_hash", "text", 0, false, "''")
	wantColumn(t, db, table, "created_at", "timestamp with time zone", 0, true, "")
	wantColumn(t, db, table, "deleted_at", "timestamp with time zone", 0, true, "")

	var defs []string
	err := db.Raw(`
SELECT pg_get_constraintdef(oid)
FROM pg_constraint
WHERE conname = 'scan_related_addresses_pkey' AND conrelid = 'scan_related_addresses'::regclass AND contype = 'p'`).Scan(&defs).Error
	if err != nil {
		t.Fatalf("read primary key: %v", err)
	}
	if len(defs) != 1 || defs[0] != "PRIMARY KEY (scan_id, chain_id, role, address)" {
		t.Fatalf("scan_related_addresses_pkey = %v, want PRIMARY KEY (scan_id, chain_id, role, address)", defs)
	}

	var fks int64
	err = db.Raw(`SELECT COUNT(*) FROM pg_constraint WHERE conrelid = 'scan_related_addresses'::regclass AND contype = 'f'`).Scan(&fks).Error
	if err != nil {
		t.Fatalf("count foreign keys: %v", err)
	}
	if fks != 0 {
		t.Fatalf("scan_related_addresses must have no foreign key, got %d", fks)
	}
}

// ET8 (OC-P21): chk_scan_related_addresses_role and chk_scan_related_addresses_enrichment.
func assertRelatedAddressConstraints(t *testing.T, db *gorm.DB) {
	t.Helper()
	const insert = `
INSERT INTO scan_related_addresses (scan_id, chain_id, role, address, enrichment_id)
VALUES ('00000000-0000-0000-0000-0000000000a2', 1, ?, '0xbbbb', ?)`
	const enrichmentID = "00000000-0000-0000-0000-0000000000c2"
	const role = "chk_scan_related_addresses_role"
	const enrichment = "chk_scan_related_addresses_enrichment"

	wantAccepted(t, probe(t, db, insert, "delegation_target", nil), "delegation_target without enrichment_id")
	wantAccepted(t, probe(t, db, insert, "deployer", enrichmentID), "deployer with enrichment_id")

	// An unknown role also fails the enrichment check, and PostgreSQL reports only one of the two,
	// so the role constraint is also read back from the catalog.
	wantRefused(t, probe(t, db, insert, "owner", nil), "chk_scan_related_addresses_", "role owner")
	wantRefused(t, probe(t, db, insert, "", enrichmentID), "chk_scan_related_addresses_", "empty role")
	var def string
	if err := db.Raw(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = ?`, role).Scan(&def).Error; err != nil {
		t.Fatalf("read %s: %v", role, err)
	}
	if !strings.Contains(def, "delegation_target") || !strings.Contains(def, "deployer") || strings.Count(def, "'") != 4 {
		t.Fatalf("%s must allow only delegation_target and deployer, got %q", role, def)
	}
	wantRefused(t, probe(t, db, insert, "delegation_target", enrichmentID), enrichment, "delegation_target with enrichment_id")
	wantRefused(t, probe(t, db, insert, "deployer", nil), enrichment, "deployer without enrichment_id")
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
