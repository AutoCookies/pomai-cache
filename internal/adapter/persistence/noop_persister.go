// File: internal/adapter/persistence/noop_persister.go
package persistence

import (
	// [SỬA] Import đường dẫn mới
	"github.com/AutoCookies/pomai-cache/internal/core/ports"
	"github.com/AutoCookies/pomai-cache/internal/engine"
)

// NoOpPersister does nothing (for testing or when persistence is disabled)
type NoOpPersister struct{}

func NewNoOpPersister() *NoOpPersister {
	return &NoOpPersister{}
}

func (n *NoOpPersister) Persist(key string, value []byte) error {
	return nil
}

// [SỬA] Tham số là []ports.WriteOp
func (n *NoOpPersister) PersistBatch(ops []ports.WriteOp) error {
	return nil
}

func (n *NoOpPersister) Load(key string) ([]byte, error) {
	return nil, nil
}

// [SỬA] Tham số là *engine.Store
func (n *NoOpPersister) Snapshot(s *engine.Store) error {
	return nil
}

// [SỬA] Tham số là *engine.Store
func (n *NoOpPersister) Restore(s *engine.Store) error {
	return nil
}

func (n *NoOpPersister) Close() error {
	return nil
}
