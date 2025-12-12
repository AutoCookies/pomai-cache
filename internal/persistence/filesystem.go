package persistence

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/cache"
)

// FilePersister writes snapshots to a file atomically (temp + rename).
type FilePersister struct {
	Path     string
	Interval time.Duration
}

// NewFilePersister creates a FilePersister.
func NewFilePersister(path string, interval time.Duration) *FilePersister {
	return &FilePersister{
		Path:     path,
		Interval: interval,
	}
}

// Restore restores store from existing snapshot file (if present).
func (p *FilePersister) Restore(s *cache.Store) error {
	f, err := os.Open(p.Path)
	if err != nil {
		if os.IsNotExist(err) {
			// no snapshot, nothing to do
			return nil
		}
		return err
	}
	defer f.Close()

	return s.RestoreFrom(f)
}

// PeriodicSnapshot starts a goroutine that snapshots the store periodically until ctx is done.
// It returns immediately after starting the goroutine.
func (p *FilePersister) PeriodicSnapshot(ctx context.Context, s *cache.Store) {
	// if no interval configured, do nothing
	if p.Interval <= 0 {
		return
	}
	ticker := time.NewTicker(p.Interval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				// on shutdown, attempt final snapshot
				_ = p.snapshotToTempAndRename(s)
				return
			case <-ticker.C:
				_ = p.snapshotToTempAndRename(s)
			}
		}
	}()
}

// SnapshotNow performs a synchronous snapshot (temp file + atomic rename).
func (p *FilePersister) SnapshotNow(s *cache.Store) error {
	return p.snapshotToTempAndRename(s)
}

// snapshotToTempAndRename writes snapshot to a temp file and renames atomically.
func (p *FilePersister) snapshotToTempAndRename(s *cache.Store) error {
	dir := filepath.Dir(p.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", p.Path, time.Now().UnixNano())
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	// write snapshot
	if err := s.SnapshotTo(f); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	// ensure fsync
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// atomic rename
	return os.Rename(tmp, p.Path)
}
