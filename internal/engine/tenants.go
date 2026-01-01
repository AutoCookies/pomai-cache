package engine

import (
	"sync"
	"time"
)

// TenantManager manages per-tenant stores.
type TenantManager struct {
	mu                sync.RWMutex
	stores            map[string]*Store
	slots             map[string]chan struct{} // semaphore per tenant
	shardCount        int
	perTenantCapacity int64
	maxConcurrent     int // maximum concurrent requests per tenant
}

// NewTenantManager creates a manager that will create per-tenant stores on demand.
// shardCount is forwarded to each per-tenant store. perTenantCapacity is bytes quota per tenant (0 = unlimited).
// This constructor keeps the old signature and uses a sensible default for max concurrent per tenant.
func NewTenantManager(shardCount int, perTenantCapacity int64) *TenantManager {
	// default max concurrent per tenant
	return NewTenantManagerWithLimit(shardCount, perTenantCapacity, 100)
}

// NewTenantManagerWithLimit allows specifying maximum concurrent requests per tenant.
func NewTenantManagerWithLimit(shardCount int, perTenantCapacity int64, maxConcurrentPerTenant int) *TenantManager {
	if maxConcurrentPerTenant <= 0 {
		maxConcurrentPerTenant = 100
	}
	return &TenantManager{
		stores:            make(map[string]*Store),
		slots:             make(map[string]chan struct{}),
		shardCount:        shardCount,
		perTenantCapacity: perTenantCapacity,
		maxConcurrent:     maxConcurrentPerTenant,
	}
}

// GetStore returns the Store for the given tenantID, creating it if needed.
func (tm *TenantManager) GetStore(tenantID string) *Store {
	// fast path read lock
	tm.mu.RLock()
	s, ok := tm.stores[tenantID]
	tm.mu.RUnlock()
	if ok {
		return s
	}

	// create under write lock
	tm.mu.Lock()
	defer tm.mu.Unlock()
	// double-check
	if s, ok = tm.stores[tenantID]; ok {
		return s
	}
	s = NewStoreWithOptions(tm.shardCount, tm.perTenantCapacity)
	tm.stores[tenantID] = s

	// Create semaphore (channel) for tenant concurrency control
	if _, ok := tm.slots[tenantID]; !ok {
		tm.slots[tenantID] = make(chan struct{}, tm.maxConcurrent)
	}

	return s
}

// AcquireTenant attempts to acquire a slot for tenantID. If timeout <= 0 it blocks until acquired.
func (tm *TenantManager) AcquireTenant(tenantID string, timeout time.Duration) bool {
	// Ensure slot exists
	tm.mu.Lock()
	ch, ok := tm.slots[tenantID]
	if !ok {
		ch = make(chan struct{}, tm.maxConcurrent)
		tm.slots[tenantID] = ch
	}
	tm.mu.Unlock()

	// Try to acquire
	if timeout <= 0 {
		// blocking acquire
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

// ReleaseTenant releases a previously acquired slot. It is safe to call even if the tenant slot map entry was removed.
func (tm *TenantManager) ReleaseTenant(tenantID string) {
	tm.mu.RLock()
	ch, ok := tm.slots[tenantID]
	tm.mu.RUnlock()
	if !ok {
		// Nothing to release (shouldn't happen in normal flow)
		return
	}
	select {
	case <-ch:
	default:
		// nothing to release (avoid blocking)
	}
}

// StatsForTenant returns Stats for a specific tenant store; returns false if tenant not found.
func (tm *TenantManager) StatsForTenant(tenantID string) (Stats, bool) {
	tm.mu.RLock()
	s, ok := tm.stores[tenantID]
	tm.mu.RUnlock()
	if !ok {
		return Stats{}, false
	}
	return s.Stats(), true
}

// ListTenants returns a slice of tenant IDs currently known.
func (tm *TenantManager) ListTenants() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	out := make([]string, 0, len(tm.stores))
	for id := range tm.stores {
		out = append(out, id)
	}
	return out
}

// StatsAll returns a map tenantID -> Stats snapshot for all known tenants.
func (tm *TenantManager) StatsAll() map[string]Stats {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	out := make(map[string]Stats, len(tm.stores))
	for id, s := range tm.stores {
		out[id] = s.Stats()
	}
	return out
}
