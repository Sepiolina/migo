package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"miservice/internal/logx"
)

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestUploadStore(t *testing.T) {
	dir := t.TempDir()
	s, err := NewUploadStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(name string, age time.Duration) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(name), 0o644)
		old := time.Now().Add(-age)
		os.Chtimes(p, old, old)
		return p
	}
	os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644)

	// request end: unregistered deleted, registered survives
	tmp, kept := mk("tmp1", 0), mk("kept1", 0)
	if err := s.Keep(kept, "113280", "BOM.xlsx"); err != nil {
		t.Fatal(err)
	}
	if !s.IsKept(kept) || s.IsKept(tmp) {
		t.Fatal("IsKept")
	}
	s.Discard(tmp, kept)
	if exists(tmp) || !exists(kept) {
		t.Fatal("Discard must delete only unregistered files")
	}
	if err := s.Keep(filepath.Join(t.TempDir(), "x"), "", ""); err == nil {
		t.Fatal("Keep must reject files outside the upload dir")
	}

	// hourly sweep: leftover unregistered older than grace goes, recent stays
	oldTmp, newTmp := mk("leftover", time.Hour), mk("inflight", time.Minute)
	n, _ := s.SweepUnregistered(time.Now(), UploadGrace)
	if n != 1 || exists(oldTmp) || !exists(newTmp) || !exists(kept) {
		t.Fatalf("sweep n=%d", n)
	}
	// startup sweep (grace 0) removes the in-flight-looking one too
	if n, _ := s.SweepUnregistered(time.Now(), 0); n != 1 || exists(newTmp) {
		t.Fatalf("startup sweep n=%d", n)
	}
	if !exists(filepath.Join(dir, ".gitkeep")) || !exists(filepath.Join(dir, keepName)) {
		t.Fatal("dotfiles must survive")
	}

	// scheduled cleanup: kept file lives until retention passes
	if e, _, _ := s.ExpireKept(time.Now().Add(13 * day)); e != 0 || !exists(kept) {
		t.Fatal("kept file expired too early")
	}
	if e, _, _ := s.ExpireKept(time.Now().Add(15 * day)); e != 1 || exists(kept) {
		t.Fatal("kept file should expire after retention")
	}
	if s.IsKept(kept) {
		t.Fatal("entry should be gone")
	}

	// RunDue: unregistered every tick, kept files only when the schedule is due
	k2 := mk("kept2", 0)
	s.Keep(k2, "", "")
	mk("stray", time.Hour)
	sum := s.RunDue(time.Now(), UploadGrace, logx.Nop) // schedule ran moments ago above
	if sum.Unregistered != 1 || sum.Expired != 0 || !exists(k2) {
		t.Fatalf("%+v", sum)
	}
	sum = s.RunDue(time.Now().Add(30*day), UploadGrace, logx.Nop)
	if sum.Expired != 1 || exists(k2) {
		t.Fatalf("scheduled run: %+v", sum)
	}

	// vanished file is pruned; corrupt registry deletes nothing
	k3 := mk("kept3", 0)
	s.Keep(k3, "", "")
	os.Remove(k3)
	if _, p, _ := s.ExpireKept(time.Now()); p != 1 {
		t.Fatal("prune")
	}
	k4, stray := mk("kept4", time.Hour), mk("stray2", time.Hour)
	s.Keep(k4, "", "")
	os.WriteFile(s.path, []byte("{nope"), 0o644)
	if _, err := s.SweepUnregistered(time.Now(), UploadGrace); err == nil || !exists(k4) || !exists(stray) {
		t.Fatal("corrupt registry must delete nothing")
	}
	s.Discard(k4, stray)
	if !exists(k4) || !exists(stray) {
		t.Fatal("Discard must also do nothing on a corrupt registry")
	}
}
