package enrichmenttransition

import (
	"testing"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/wallettransition"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.ScanResultEntity{}, &domain.ScanEnrichmentEntity{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	return db
}

func seedScan(t *testing.T, db *gorm.DB, user uuid.UUID, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := db.Create(&domain.ScanResultEntity{ID: id, UserID: user, Address: "0xabc0000000000000000000000000000000000001", Status: status}).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func pendingCmd(user uuid.UUID, transitionID string) Command {
	return Command{
		TransitionID: transitionID, UserID: user, Family: domain.EnrichmentFamilyHistory,
		SourceStates: []string{domain.EnrichmentStatusPending}, TargetState: domain.EnrichmentStatusPending, QuotaAction: "none",
		Result: &EnrichmentTransitionResult{Chains: []domain.EnrichmentChain{
			{ChainID: 10, Outcome: domain.EnrichmentOutcomeNotObserved},
			{ChainID: 1, Outcome: domain.EnrichmentOutcomePending},
		}},
	}
}

func countEnrichments(t *testing.T, db *gorm.DB, scanID uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := db.Unscoped().Model(&domain.ScanEnrichmentEntity{}).Where("scan_id = ?", scanID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// Design §5.8, Missing enrichment: target PENDING inserts the row.
func TestApplyEnrichmentTransition_CreatesMissingPending(t *testing.T) {
	db := openDB(t)
	user := uuid.New()
	scanID, enrichmentID := seedScan(t, db, user, scan.StateSUCCESS), uuid.New()

	res := Apply(db, scanID, enrichmentID, pendingCmd(user, "t-1"))
	if res.Outcome != wallettransition.OutcomeApplied || res.Status == nil || *res.Status != domain.EnrichmentStatusPending {
		t.Fatalf("result %+v", res)
	}
	var row domain.ScanEnrichmentEntity
	if err := db.Where("enrichment_id = ?", enrichmentID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	chains, err := row.DecodeChains()
	if err != nil || len(chains) != 2 || chains[0].ChainID != 10 || chains[1].ChainID != 1 {
		t.Fatalf("chains %+v err %v (body order expected)", chains, err)
	}
	if row.ScanID != scanID || row.UserID != user || row.Family != domain.EnrichmentFamilyHistory ||
		row.Status != domain.EnrichmentStatusPending || row.Error != "" || row.TransitionID != "t-1" {
		t.Fatalf("row %+v", row)
	}
}

// Design §5.8 and §5.9 (History columns): DONE creation writes the history columns of the scan in the same transaction.
func TestApplyEnrichmentTransition_CreatesMissingDoneWritesHistoryColumns(t *testing.T) {
	db := openDB(t)
	user := uuid.New()
	scanID := seedScan(t, db, user, scan.StateSUCCESS)
	cmd := pendingCmd(user, "t-done")
	cmd.TargetState = domain.EnrichmentStatusDone
	cmd.Result = &EnrichmentTransitionResult{
		Chains:            []domain.EnrichmentChain{{ChainID: 1, Outcome: domain.EnrichmentOutcomeObserved, TransactionHash: "0xaaa"}},
		PublicKey:         "04abcd",
		TransactionHash:   "0xaaa",
		ExposedNetwork:    "ethereum",
		PublicKeyRecovery: "recovered",
	}

	res := Apply(db, scanID, uuid.New(), cmd)
	if res.Outcome != wallettransition.OutcomeApplied || res.Status == nil || *res.Status != domain.EnrichmentStatusDone {
		t.Fatalf("result %+v", res)
	}
	var s domain.ScanResultEntity
	if err := db.Where("id = ?", scanID).First(&s).Error; err != nil {
		t.Fatal(err)
	}
	if s.PublicKey != "04abcd" || s.TransactionHash != "0xaaa" || s.ExposedNetwork != "ethereum" || string(s.PublicKeyRecovery) != "recovered" {
		t.Fatalf("history columns %+v", s)
	}
	if s.Status != scan.StateSUCCESS || s.Chains != "" {
		t.Fatalf("other scan columns written: %+v", s)
	}
}

// Design §5.8: ERROR creation (direct record, every chain in error) writes no history column.
func TestApplyEnrichmentTransition_CreatesMissingErrorWritesNoHistoryColumns(t *testing.T) {
	db := openDB(t)
	user := uuid.New()
	scanID := seedScan(t, db, user, scan.StateSUCCESS)
	cmd := pendingCmd(user, "t-err")
	cmd.TargetState = domain.EnrichmentStatusError
	cmd.Error = "nonce unread"
	cmd.Result = &EnrichmentTransitionResult{Chains: []domain.EnrichmentChain{{ChainID: 1, Outcome: domain.EnrichmentOutcomeError, Code: "nonce_unread"}}}

	res := Apply(db, scanID, uuid.New(), cmd)
	if res.Outcome != wallettransition.OutcomeApplied || res.Status == nil || *res.Status != domain.EnrichmentStatusError {
		t.Fatalf("result %+v", res)
	}
	var s domain.ScanResultEntity
	if err := db.Where("id = ?", scanID).First(&s).Error; err != nil {
		t.Fatal(err)
	}
	if s.PublicKey != "" || s.TransactionHash != "" || s.ExposedNetwork != "" || s.PublicKeyRecovery != "" {
		t.Fatalf("history columns written on ERROR: %+v", s)
	}
}

// Design §5.8: RUNNING, or ERROR without result, on a missing enrichment is rejected and creates nothing.
func TestApplyEnrichmentTransition_MissingRunningOrBareErrorIsRejected(t *testing.T) {
	cases := map[string]func(c *Command){
		"running on a missing row": func(c *Command) { c.TargetState = domain.EnrichmentStatusRunning; c.Result = nil },
		"error without result": func(c *Command) {
			c.TargetState = domain.EnrichmentStatusError
			c.Error = "abandoned"
			c.Result = nil
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			db := openDB(t)
			user := uuid.New()
			scanID := seedScan(t, db, user, scan.StateSUCCESS)
			cmd := pendingCmd(user, "t-x")
			mutate(&cmd)
			if res := Apply(db, scanID, uuid.New(), cmd); res.Outcome != wallettransition.OutcomeRejected {
				t.Fatalf("result %+v", res)
			}
			if n := countEnrichments(t, db, scanID); n != 0 {
				t.Fatalf("rows = %d", n)
			}
		})
	}
}

// Design §5.8, The scan check (OP-5.5 (c)): a missing, soft-deleted, foreign, or not-SUCCESS scan rejects every target and writes nothing.
func TestApplyEnrichmentTransition_ScanCheckRejectsWithoutWrite(t *testing.T) {
	user := uuid.New()
	cases := map[string]func(t *testing.T, db *gorm.DB) uuid.UUID{
		"missing scan": func(t *testing.T, db *gorm.DB) uuid.UUID { return uuid.New() },
		"soft-deleted scan": func(t *testing.T, db *gorm.DB) uuid.UUID {
			id := seedScan(t, db, user, scan.StateSUCCESS)
			if err := db.Delete(&domain.ScanResultEntity{}, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			return id
		},
		"scan of another user": func(t *testing.T, db *gorm.DB) uuid.UUID { return seedScan(t, db, uuid.New(), scan.StateSUCCESS) },
		"scan not SUCCESS":     func(t *testing.T, db *gorm.DB) uuid.UUID { return seedScan(t, db, user, scan.StateRUNNING) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			for _, target := range []string{domain.EnrichmentStatusPending, domain.EnrichmentStatusDone, domain.EnrichmentStatusError} {
				db := openDB(t)
				scanID := setup(t, db)
				cmd := pendingCmd(user, "t-1")
				cmd.TargetState = target
				res := Apply(db, scanID, uuid.New(), cmd)
				if res.Outcome != wallettransition.OutcomeRejected || res.Status != nil {
					t.Fatalf("%s: result %+v", target, res)
				}
				if n := countEnrichments(t, db, scanID); n != 0 {
					t.Fatalf("%s: rows = %d", target, n)
				}
				var s domain.ScanResultEntity
				if err := db.Unscoped().Where("id = ?", scanID).First(&s).Error; err == nil && (s.PublicKey != "" || s.PublicKeyRecovery != "") {
					t.Fatalf("%s: history columns written: %+v", target, s)
				}
			}
		})
	}
}

// Design §5.8, Soft-deleted enrichment: an insert that meets a soft-deleted row, or a (scan_id, family) conflict under another
// id, affects no row; the re-read finds nothing and the command is rejected, the stored row untouched.
func TestApplyEnrichmentTransition_InsertConflictWithoutVisibleRowIsRejected(t *testing.T) {
	db := openDB(t)
	user := uuid.New()
	scanID := seedScan(t, db, user, scan.StateSUCCESS)

	t.Run("soft-deleted enrichment with the same id", func(t *testing.T) {
		id := uuid.New()
		if err := db.Create(&domain.ScanEnrichmentEntity{
			EnrichmentID: id, ScanID: scanID, Family: "other", UserID: user, Status: domain.EnrichmentStatusPending, Chains: "[]", TransitionID: "old",
		}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Delete(&domain.ScanEnrichmentEntity{}, "enrichment_id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		cmd := pendingCmd(user, "t-new")
		cmd.Family = "other"
		if res := Apply(db, scanID, id, cmd); res.Outcome != wallettransition.OutcomeRejected {
			t.Fatalf("result %+v", res)
		}
		var row domain.ScanEnrichmentEntity
		if err := db.Unscoped().Where("enrichment_id = ?", id).First(&row).Error; err != nil || !row.DeletedAt.Valid || row.TransitionID != "old" {
			t.Fatalf("stored row changed: %+v err %v", row, err)
		}
	})

	t.Run("same scan and family under another id", func(t *testing.T) {
		winner := uuid.New()
		if err := db.Create(&domain.ScanEnrichmentEntity{
			EnrichmentID: winner, ScanID: scanID, Family: domain.EnrichmentFamilyHistory, UserID: user,
			Status: domain.EnrichmentStatusPending, Chains: "[]", TransitionID: "winner",
		}).Error; err != nil {
			t.Fatal(err)
		}
		if res := Apply(db, scanID, uuid.New(), pendingCmd(user, "t-loser")); res.Outcome != wallettransition.OutcomeRejected {
			t.Fatalf("result %+v", res)
		}
		var row domain.ScanEnrichmentEntity
		if err := db.Where("enrichment_id = ?", winner).First(&row).Error; err != nil || row.TransitionID != "winner" {
			t.Fatalf("winner changed: %+v err %v", row, err)
		}
		if n := countEnrichments(t, db, scanID); n != 2 {
			t.Fatalf("rows = %d", n)
		}
	})
}
