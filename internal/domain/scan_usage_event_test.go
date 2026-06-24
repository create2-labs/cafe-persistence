package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestScanUsageEventEntity_AutoMigrateAndUniqueScanID(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&ScanUsageEventEntity{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	userID := uuid.New()
	scanID := uuid.New()
	event := &ScanUsageEventEntity{
		UserID:     userID,
		ScanID:     scanID,
		ScanKind:   ScanUsageKindWallet,
		ConsumedAt: time.Now().UTC(),
	}
	if err := db.Create(event).Error; err != nil {
		t.Fatalf("create event: %v", err)
	}

	dup := &ScanUsageEventEntity{
		UserID:     userID,
		ScanID:     scanID,
		ScanKind:   ScanUsageKindWallet,
		ConsumedAt: time.Now().UTC(),
	}
	if err := db.Create(dup).Error; err == nil {
		t.Fatal("expected unique constraint violation on scan_id")
	}
}
