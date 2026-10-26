package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUploadCleanupRejectsUnsafeRegistryPaths(t *testing.T) {
	for _, name := range []string{"../outside", ".keep.json", "nested/file", "", "/tmp/outside"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(root, "outside")
			if err := os.WriteFile(outside, []byte("preserve"), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := NewUploadStore(filepath.Join(root, "uploads"))
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(keepRegistry{Entries: []keepEntry{{File: name, RegisteredAt: 1}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.ExpireKept(time.Now()); !errors.Is(err, errRegistryCorrupt) {
				t.Fatalf("unsafe registry accepted: %v", err)
			}
			if _, err := os.Stat(outside); err != nil {
				t.Fatalf("outside file removed: %v", err)
			}
			if _, err := s.SweepUnregistered(time.Now(), 0); !errors.Is(err, errRegistryCorrupt) {
				t.Fatalf("sweep should preserve files when registry is unsafe: %v", err)
			}
		})
	}
}
