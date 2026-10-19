package wallettransition

import (
	"testing"
	"time"

	"cafe-persistence/internal/domain"
	"cafe-persistence/internal/repository"
	"cafe-persistence/pkg/scan"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestApplyMissing_InsertConflictQualifiesWinner(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.ScanResultEntity{}, &domain.ScanUsageEventEntity{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	ledger := repository.NewScanUsageLedgerRepository(db)
	user := uuid.New()
	addr := "0xabc0000000000000000000000000000000000001"
	success := Command{
		UserID: user, Address: addr, SourceStates: []string{scan.StatePENDING, scan.StateRUNNING},
		TargetState: scan.StateSUCCESS, QuotaAction: QuotaKeep,
		Result: &domain.ScanResult{
			Address: addr, Type: domain.AccountTypeEOA, Algorithm: domain.AlgorithmECDSAsecp256k1,
			NISTLevel: domain.NISTLevel1, IsEOA: true, RiskScore: 4,
		},
	}

	t.Run("running winner is updated", func(t *testing.T) {
		scanID := uuid.New()
		if err := ledger.RecordSuccessUsage(user, scanID, domain.ScanUsageKindWallet); err != nil {
			t.Fatal(err)
		}
		winner := "winner-running"
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&domain.ScanResultEntity{
				ID: scanID, UserID: user, Address: addr, Status: scan.StateRUNNING, TransitionID: &winner,
			}).Error; err != nil {
				return err
			}
			success.TransitionID = "loser-success"
			res, err := applyMissing(tx, ledger, scanID, success)
			if err != nil || res.Outcome != OutcomeApplied || res.Status != scan.StateSUCCESS {
				t.Fatalf("result %+v err %v", res, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("terminal winner is not rewritten", func(t *testing.T) {
		scanID := uuid.New()
		if err := ledger.RecordSuccessUsage(user, scanID, domain.ScanUsageKindWallet); err != nil {
			t.Fatal(err)
		}
		winner := "winner-success"
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&domain.ScanResultEntity{
				ID: scanID, UserID: user, Address: addr, Status: scan.StateSUCCESS, TransitionID: &winner, RiskScore: 1,
			}).Error; err != nil {
				return err
			}
			success.TransitionID = "loser-terminal"
			success.Result.RiskScore = 9
			res, err := applyMissing(tx, ledger, scanID, success)
			if err != nil || res.Outcome != OutcomeAlreadyTerminal || res.Status != scan.StateSUCCESS {
				t.Fatalf("result %+v err %v", res, err)
			}
			var stored domain.ScanResultEntity
			if err := tx.Where("id = ?", scanID).First(&stored).Error; err != nil {
				return err
			}
			if stored.RiskScore != 1 || stored.TransitionID == nil || *stored.TransitionID != winner {
				t.Fatalf("winner rewritten: %+v", stored)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var n int64
		if err := db.Model(&domain.ScanUsageEventEntity{}).Where("scan_id = ?", scanID).Count(&n).Error; err != nil || n != 1 {
			t.Fatalf("ledger = %d err %v", n, err)
		}
	})

	t.Run("second start is already running", func(t *testing.T) {
		scanID := uuid.New()
		winner := "winner-start"
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&domain.ScanResultEntity{
				ID: scanID, UserID: user, Address: addr, Status: scan.StateRUNNING, TransitionID: &winner,
				CreatedAt: time.Now(), UpdatedAt: time.Now(),
			}).Error; err != nil {
				return err
			}
			res, err := applyMissing(tx, ledger, scanID, Command{
				TransitionID: "loser-start", UserID: user, Address: addr,
				SourceStates: []string{scan.StatePENDING}, TargetState: scan.StateRUNNING, QuotaAction: QuotaNone,
			})
			if err != nil || res.Outcome != OutcomeAlreadyRunning || res.Status != scan.StateRUNNING {
				t.Fatalf("result %+v err %v", res, err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
