package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"miservice/internal/logx"
)

func TestOutputStore(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewOutputStore(filepath.Join(dir, "outputs"))
	commit := func(name, owner string) string {
		tmp := s.NewTempPath()
		os.WriteFile(tmp, []byte(name), 0o644)
		p, err := s.Commit(tmp, name, owner)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
		return p
	}
	p1 := commit("a_1.xlsx", "1")
	commit("b_1.xlsx", "1")
	commit("c_1.xlsx", "1")
	other := commit("x_2.xlsx", "2")
	if _, err := os.Stat(p1); err == nil {
		t.Error("oldest output for owner 1 should have been evicted")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("other owner untouched")
	}
	// old orphan swept; recent kept; manifest survives
	old := time.Now().Add(-30 * 24 * time.Hour)
	orphan := filepath.Join(s.OutputDir, "orphan.xlsx")
	os.WriteFile(orphan, nil, 0o644)
	os.Chtimes(orphan, old, old)
	sum, _ := s.Cleanup(time.Now())
	if sum.Orphans != 1 || sum.Outputs != 0 {
		t.Errorf("%+v", sum)
	}
	if _, due := s.RunCleanupIfDue(time.Now(), logx.Nop); due {
		t.Error("not due right after a cleanup")
	}
	// corrupt manifest tolerated
	os.WriteFile(s.manifestPath, []byte("{nope"), 0o644)
	commit("d_3.xlsx", "3")
}
