package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"miservice/internal/logx"
)

// The upload folder follows one rule:
//
//   - a file NOT registered in the keep registry is temporary: it is deleted
//     automatically (right after its request, and by an hourly sweep that
//     also catches leftovers from crashes);
//   - a file registered in the keep registry stays until the scheduled
//     cleanup (same schedule as outputs: every CleanupIntervalDays, files
//     older than RetentionDays) removes it.
const (
	// UploadGrace protects unregistered files that may still belong to a
	// request in flight (a 25 MB upload can take a while on a slow link).
	UploadGrace = 15 * time.Minute

	uploadSweepEvery = time.Hour
	keepName         = ".keep.json"
)

type keepEntry struct {
	File         string `json:"file"`            // base name inside the upload dir
	Owner        string `json:"owner,omitempty"` // who asked to keep it (module's choice)
	Name         string `json:"name,omitempty"`  // client's original file name
	RegisteredAt int64  `json:"registeredAt"`    // epoch ms; retention counts from here
}

type keepRegistry struct {
	Entries       []keepEntry `json:"entries"`
	LastScheduled int64       `json:"lastScheduled"` // last scheduled run for kept files
}

// UploadSweep counts what a sweep removed.
type UploadSweep struct{ Unregistered, Expired, Pruned int }

// UploadStore owns the upload directory and its keep registry
// (<dir>/.keep.json). A module registers a file with Keep when it must
// survive its request; everything else is removed.
type UploadStore struct {
	Dir       string
	Retention time.Duration // how long a kept file lives
	Interval  time.Duration // how often the scheduled cleanup of kept files runs

	mu   sync.Mutex // one process; serialises registry access and deletions
	path string
}

func NewUploadStore(dir string) (*UploadStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &UploadStore{
		Dir:       dir,
		Retention: RetentionDays * day,
		Interval:  CleanupIntervalDays * day,
		path:      filepath.Join(dir, keepName),
	}, nil
}

var errRegistryCorrupt = errors.New("keep registry is unreadable")

// read returns the registry. A missing file is an empty registry; a corrupt
// one is an error, because treating it as empty would make every kept file
// look unregistered and delete it.
func (s *UploadStore) read() (keepRegistry, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return keepRegistry{}, nil
	}
	if err != nil {
		return keepRegistry{}, err
	}
	var r keepRegistry
	if json.Unmarshal(b, &r) != nil {
		return keepRegistry{}, errRegistryCorrupt
	}
	return r, nil
}

func (s *UploadStore) write(r keepRegistry) error {
	if r.Entries == nil {
		r.Entries = []keepEntry{}
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	var rb [8]byte
	_, _ = rand.Read(rb[:])
	tmp := fmt.Sprintf("%s.%s.tmp", s.path, hex.EncodeToString(rb[:]))
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// local returns the base name of path if it is a file directly inside Dir.
func (s *UploadStore) local(path string) (string, bool) {
	if filepath.Clean(filepath.Dir(path)) != filepath.Clean(s.Dir) {
		return "", false
	}
	n := filepath.Base(path)
	return n, n != "" && !strings.HasPrefix(n, ".")
}

// Keep registers a stored upload so it is not deleted with its request. It
// then lives until the scheduled cleanup. owner and clientName are recorded
// for whoever inspects the registry.
func (s *UploadStore) Keep(path, owner, clientName string) error {
	name, ok := s.local(path)
	if !ok {
		return fmt.Errorf("storage: %q is not a file in the upload directory", path)
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.read()
	if err != nil {
		return err
	}
	entries := []keepEntry{{File: name, Owner: owner, Name: clientName, RegisteredAt: time.Now().UnixMilli()}}
	for _, e := range r.Entries {
		if e.File != name {
			entries = append(entries, e)
		}
	}
	r.Entries = entries
	return s.write(r)
}

// IsKept reports whether path is registered.
func (s *UploadStore) IsKept(path string) bool {
	name, ok := s.local(path)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.read()
	if err != nil {
		return true // unreadable registry: assume kept, never delete on doubt
	}
	for _, e := range r.Entries {
		if e.File == name {
			return true
		}
	}
	return false
}

// Discard is the end-of-request cleanup: it deletes every given upload that
// is not registered in the keep registry and leaves registered ones alone.
func (s *UploadStore) Discard(paths ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.read()
	if err != nil {
		return // cannot tell what is kept; the sweep will retry once it can
	}
	kept := map[string]bool{}
	for _, e := range r.Entries {
		kept[e.File] = true
	}
	for _, p := range paths {
		if n, ok := s.local(p); ok && !kept[n] {
			_ = os.Remove(p)
		}
	}
}

// SweepUnregistered deletes every regular file in the upload directory that
// is not registered and is older than grace (use 0 at startup, when no
// request can be in flight). Dotfiles (.gitkeep, the registry) are never
// touched, except stale registry temp files.
func (s *UploadStore) SweepUnregistered(now time.Time, grace time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sweepUnregisteredLocked(now, grace)
}

func (s *UploadStore) sweepUnregisteredLocked(now time.Time, grace time.Duration) (int, error) {
	r, err := s.read()
	if err != nil {
		return 0, err
	}
	kept := map[string]bool{}
	for _, e := range r.Entries {
		kept[e.File] = true
	}
	des, err := os.ReadDir(s.Dir)
	if err != nil {
		return 0, err
	}
	removed := 0
	cutoff := now.Add(-grace)
	for _, de := range des {
		n := de.Name()
		staleTmp := strings.HasPrefix(n, keepName+".") && strings.HasSuffix(n, ".tmp")
		if kept[n] || (strings.HasPrefix(n, ".") && !staleTmp) {
			continue
		}
		full := filepath.Join(s.Dir, n)
		st, err := os.Stat(full)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if !st.ModTime().After(cutoff) && os.Remove(full) == nil {
			removed++
		}
	}
	return removed, nil
}

// ExpireKept is the scheduled cleanup: it deletes registered files whose
// registration is older than Retention, drops their entries, and prunes
// entries whose file has vanished.
func (s *UploadStore) ExpireKept(now time.Time) (expired, pruned int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.expireKeptLocked(now)
}

func (s *UploadStore) expireKeptLocked(now time.Time) (expired, pruned int, err error) {
	r, err := s.read()
	if err != nil {
		return 0, 0, err
	}
	cutoff := now.Add(-s.Retention).UnixMilli()
	var live []keepEntry
	for _, e := range r.Entries {
		full := filepath.Join(s.Dir, e.File)
		switch _, serr := os.Stat(full); {
		case serr != nil:
			pruned++
		case e.RegisteredAt < cutoff:
			if rerr := os.Remove(full); rerr != nil {
				live = append(live, e) // could not delete: try again next run
			} else {
				expired++
			}
		default:
			live = append(live, e)
		}
	}
	r.Entries = live
	r.LastScheduled = now.UnixMilli()
	return expired, pruned, s.write(r)
}

// RunDue is one scheduler tick: unregistered files are always swept (past
// the grace period); kept files are only expired when the scheduled run is
// due (the last run time is persisted, so restarts cannot postpone it).
func (s *UploadStore) RunDue(now time.Time, grace time.Duration, log logx.Logger) UploadSweep {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sum UploadSweep
	n, err := s.sweepUnregisteredLocked(now, grace)
	if err != nil {
		log.Printf("Upload sweep skipped (nothing deleted): %s", err.Error())
		return sum
	}
	sum.Unregistered = n
	r, _ := s.read()
	if now.UnixMilli()-r.LastScheduled >= s.Interval.Milliseconds() {
		sum.Expired, sum.Pruned, err = s.expireKeptLocked(now)
		if err != nil {
			log.Printf("Scheduled upload cleanup failed (will retry at next check): %s", err.Error())
		}
	}
	if sum != (UploadSweep{}) {
		log.Printf("Uploads: removed %d unregistered file(s), %d expired kept file(s), pruned %d stale registry entr(ies).", sum.Unregistered, sum.Expired, sum.Pruned)
	}
	return sum
}

// StartScheduler cleans once at startup (no grace: nothing can be in flight
// yet) and then hourly until stop is closed.
func (s *UploadStore) StartScheduler(log logx.Logger, stop <-chan struct{}) {
	s.RunDue(time.Now(), 0, log)
	go func() {
		t := time.NewTicker(uploadSweepEvery)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.RunDue(time.Now(), UploadGrace, log)
			case <-stop:
				return
			}
		}
	}()
}
