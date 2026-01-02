// File: internal/engine/tenants/manager.go
package tenants

import (
	"sync"
	"time"

	"github.com/AutoCookies/pomai-cache/internal/engine/core"
)

// Manager manages per-tenant stores
type Manager struct {
	mu                sync.RWMutex
	stores            map[string]*core.Store
	slots             map[string]chan struct{}
	shardCount        int
	perTenantCapacity int64
	maxConcurrent     int
}

// StoreInterface defines store operations
type StoreInterface interface {
	// Add methods as needed
}

// StoreFactory creates stores
type StoreFactory func(shardCount int, capacityBytes int64) StoreInterface

// NewManager creates tenant manager
func NewManager(shardCount int, perTenantCapacity int64) *Manager {
	return NewManagerWithLimit(shardCount, perTenantCapacity, 100)
}

// NewManagerWithLimit creates manager with concurrency limit
func NewManagerWithLimit(shardCount int, perTenantCapacity int64, maxConcurrent int) *Manager {
	if maxConcurrent <= 0 {
		maxConcurrent = 100
	}

	return &Manager{
		stores:            make(map[string]*core.Store),
		slots:             make(map[string]chan struct{}),
		shardCount:        shardCount,
		perTenantCapacity: perTenantCapacity,
		maxConcurrent:     maxConcurrent,
	}
}

// GetStore returns store for tenant (creates if needed)
func (tm *Manager) GetStore(tenantID string) *core.Store {
	tm.mu.RLock()
	store, ok := tm.stores[tenantID]
	tm.mu.RUnlock()

	if ok {
		return store
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	if store, ok = tm.stores[tenantID]; ok {
		return store
	}

	store = core.NewStoreWithOptions(tm.shardCount, tm.perTenantCapacity)
	store.SetTenantID(tenantID)
	tm.stores[tenantID] = store

	return store
}

// AcquireTenant acquires slot for tenant
func (tm *Manager) AcquireTenant(tenantID string, timeout time.Duration) bool {
	tm.mu.Lock()
	ch, ok := tm.slots[tenantID]
	if !ok {
		ch = make(chan struct{}, tm.maxConcurrent)
		tm.slots[tenantID] = ch
	}
	tm.mu.Unlock()

	if timeout <= 0 {
		ch <- struct{}{}
		return true
	}

	select {
	case ch <- struct{}{}:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ReleaseTenant releases tenant slot
func (tm *Manager) ReleaseTenant(tenantID string) {
	tm.mu.RLock()
	ch, ok := tm.slots[tenantID]
	tm.mu.RUnlock()

	if !ok {
		return
	}

	select {
	case <-ch:
	default:
	}
}

// ListTenants returns all tenant IDs
func (tm *Manager) ListTenants() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tenants := make([]string, 0, len(tm.stores))
	for id := range tm.stores {
		tenants = append(tenants, id)
	}
	return tenants
}

// RemoveTenant removes tenant store
func (tm *Manager) RemoveTenant(tenantID string) bool {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	_, ok := tm.stores[tenantID]
	if !ok {
		return false
	}

	delete(tm.stores, tenantID)
	delete(tm.slots, tenantID)
	return true
}
