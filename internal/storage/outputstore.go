package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"miservice/internal/logx"
)

const (
	day                 = 24 * time.Hour
	MaxOutputsPerOwner  = 2  // newest N outputs kept per owner (employee number)
	RetentionDays       = 14 // unused files older than this are deleted by cleanup
	CleanupIntervalDays = 14 // how often cleanup runs
	cleanupCheckEvery   = day
	manifestName        = ".manifest.json"
)

type manifestEntry struct {
	File      string `json:"file"`
	Owner     string `json:"employee"`  // JSON key kept: the Node service wrote "employee"
	CreatedAt int64  `json:"createdAt"` // epoch ms, same as the Node manifest
	LastUsed  int64  `json:"lastUsed"`
}

type manifest struct {
	Entries     []manifestEntry `json:"entries"`
	LastCleanup int64           `json:"lastCleanup"`
}

// OutputStore keeps the files the service generates, indexed in a manifest,
// with "newest N per owner" and age-based retention. An owner is whatever
// groups outputs for a module (the BOM module uses the employee number).
// The on-disk manifest format is the one the Node service wrote, so an
// existing outputs/ folder carries over.
type OutputStore struct {
	OutputDir       string
	MaxPerOwner     int
	Retention       time.Duration
	CleanupInterval time.Duration

	mu           sync.Mutex // serialises all manifest access (single process)
	manifestPath string
}

func NewOutputStore(outputDir string) (*OutputStore, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, err
	}
	return &OutputStore{
		OutputDir:       outputDir,
		MaxPerOwner:     MaxOutputsPerOwner,
		Retention:       RetentionDays * day,
		CleanupInterval: CleanupIntervalDays * day,
		manifestPath:    filepath.Join(outputDir, manifestName),
	}, nil
}

func (s *OutputStore) readManifest() manifest {
	b, err := os.ReadFile(s.manifestPath)
	if err != nil {
		return manifest{}
	}
	var m manifest
	if json.Unmarshal(b, &m) != nil {
		return manifest{}
	}
	return m
}

func (s *OutputStore) writeManifest(m manifest) error {
	if m.Entries == nil {
		m.Entries = []manifestEntry{}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	var rb [8]byte
	_, _ = rand.Read(rb[:])
	tmp := fmt.Sprintf("%s.%s.tmp", s.manifestPath, hex.EncodeToString(rb[:]))
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.manifestPath)
}

// NewTempPath is where the pipeline should write; Commit moves it into place
// atomically so a half-written file is never visible or downloadable.
func (s *OutputStore) NewTempPath() string {
	var rb [16]byte
	_, _ = rand.Read(rb[:])
	return filepath.Join(s.OutputDir, ".tmp-"+hex.EncodeToString(rb[:])+".xlsx")
}

// Discard removes an uncommitted temp file.
func (s *OutputStore) Discard(tempPath string) { _ = os.Remove(tempPath) }

// Commit installs tempPath as fileName (overwriting) and enforces the
// "newest N per owner" limit.
func (s *OutputStore) Commit(tempPath, fileName, owner string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	finalPath := filepath.Join(s.OutputDir, fileName)
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", err
	}
	m := s.readManifest()
	now := time.Now().UnixMilli()
	entries := []manifestEntry{{File: fileName, Owner: owner, CreatedAt: now, LastUsed: now}}
	for _, e := range m.Entries {
		if e.File != fileName {
			entries = append(entries, e)
		}
	}
	var mine []manifestEntry
	for _, e := range entries {
		if e.Owner == owner {
			mine = append(mine, e)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool { return mine[i].LastUsed > mine[j].LastUsed })
	evict := map[manifestEntry]bool{}
	if len(mine) > s.MaxPerOwner {
		for _, e := range mine[s.MaxPerOwner:] {
			_ = os.Remove(filepath.Join(s.OutputDir, e.File))
			evict[e] = true
		}
	}
	m.Entries = m.Entries[:0:0]
	for _, e := range entries {
		if !evict[e] {
			m.Entries = append(m.Entries, e)
		}
	}
	return finalPath, s.writeManifest(m)
}

// CleanupSummary counts what Cleanup removed.
type CleanupSummary struct{ Outputs, Orphans int }

// Cleanup deletes outputs unused beyond the retention window and orphaned
// old files in the output dir. Dotfiles other than stale .tmp-* files
// (.gitkeep, the manifest) are never touched. Uploads are UploadStore's job.
func (s *OutputStore) Cleanup(now time.Time) (CleanupSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cleanupLocked(now)
}

func (s *OutputStore) cleanupLocked(now time.Time) (CleanupSummary, error) {
	var sum CleanupSummary
	cutoff := now.Add(-s.Retention)
	m := s.readManifest()
	var keep []manifestEntry
	for _, e := range m.Entries {
		if e.LastUsed < cutoff.UnixMilli() {
			_ = os.Remove(filepath.Join(s.OutputDir, e.File))
			sum.Outputs++
		} else {
			keep = append(keep, e)
		}
	}
	m.Entries = keep
	indexed := map[string]bool{}
	for _, e := range keep {
		indexed[e.File] = true
	}
	sweep := func(dir string, candidate func(string) bool, counter *int) {
		if dir == "" {
			return
		}
		des, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, de := range des {
			if !candidate(de.Name()) {
				continue
			}
			full := filepath.Join(dir, de.Name())
			st, err := os.Stat(full)
			if err != nil || !st.Mode().IsRegular() {
				continue
			}
			if st.ModTime().Before(cutoff) && os.Remove(full) == nil {
				*counter++
			}
		}
	}
	sweep(s.OutputDir, func(n string) bool {
		return !indexed[n] && (!strings.HasPrefix(n, ".") || strings.HasPrefix(n, ".tmp-"))
	}, &sum.Orphans)

	m.LastCleanup = now.UnixMilli()
	return sum, s.writeManifest(m)
}

// RunCleanupIfDue runs Cleanup only if the last run was >= CleanupInterval
// ago (the last-run time is persisted, so restarts cannot postpone it).
func (s *OutputStore) RunCleanupIfDue(now time.Time, log logx.Logger) (*CleanupSummary, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.readManifest()
	if now.UnixMilli()-m.LastCleanup < s.CleanupInterval.Milliseconds() {
		return nil, false
	}
	sum, err := s.cleanupLocked(now)
	if err != nil {
		log.Printf("Cleanup failed (will retry at next check): %s", err.Error())
		return nil, false
	}
	log.Printf("Cleanup: removed %d expired output(s), %d orphaned output file(s).", sum.Outputs, sum.Orphans)
	return &sum, true
}

// StartCleanupScheduler checks once now and then daily until stop is closed.
func (s *OutputStore) StartCleanupScheduler(log logx.Logger, stop <-chan struct{}) {
	s.RunCleanupIfDue(time.Now(), log)
	go func() {
		t := time.NewTicker(cleanupCheckEvery)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.RunCleanupIfDue(time.Now(), log)
			case <-stop:
				return
			}
		}
	}()
}
