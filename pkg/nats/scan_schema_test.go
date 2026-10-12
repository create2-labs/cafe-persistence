package nats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryOmitsLocalScanMessages(t *testing.T) {
	root := moduleRoot(t)
	needles := []string{
		"type WalletScanMessage", "type TLSScanMessage", "type ScanStartedMessage",
		"type ScanCompletedMessage", "type ScanFailedMessage", "type ScanReadyMessage",
		"type ScannerHeartbeatMessage",
		`= "scan.started"`, `= "scan.completed"`, `= "scan.failed"`, `= "scan.ready"`,
		`= "scan.requested.wallet"`, `= "scan.requested.tls"`,
		`= "scanner.heartbeat.tls"`, `= "scanner.heartbeat.wallet"`, `= "persistence.ready"`,
		`= "cafe.discovery.wallet.scan"`, `= "cafe.discovery.tls.scan"`,
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "reports" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "scan_schema_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, needle := range needles {
			if strings.Contains(string(body), needle) {
				t.Errorf("%s contains %s", path, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
