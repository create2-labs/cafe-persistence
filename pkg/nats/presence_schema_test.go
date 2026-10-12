package nats

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRepositoryOmitsLocalScannerPresenceSchema(t *testing.T) {
	root := moduleRoot(t)
	needles := []string{
		"type " + "ScannerPresenceMessage",
		"ScannerPresence" + "Joined",
		"ScannerPresence" + "Left",
		"ScannerPresence" + "Event",
		"ScannerPresence" + "Message",
		"Subject" + "ScannerPresence",
		"cafe.discovery.scanners." + "presence",
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".gomodcache", "reports", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() == "presence_schema_test.go" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 1<<20 {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, needle := range needles {
			if strings.Contains(text, needle) {
				t.Errorf("%s contains %s", rel, needle)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
