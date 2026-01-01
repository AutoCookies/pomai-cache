package engine

import (
	"sync/atomic"
	"time"
)

// MemoryController manages a global memory budget across all tenant stores.
type MemoryController struct {
	capacity int64 // 0 = unlimited
	used     int64
	tm       *TenantManager
}

// NewMemoryController creates controller. capacityBytes = 0 => unlimited.
func NewMemoryController(tm *TenantManager, capacityBytes int64) *MemoryController {
	return &MemoryController{
		capacity: capacityBytes,
		used:     0,
		tm:       tm,
	}
}

// Used returns current used bytes
func (mc *MemoryController) Used() int64 {
	return atomic.LoadInt64(&mc.used)
}

// Capacity returns capacity
func (mc *MemoryController) Capacity() int64 {
	return mc.capacity
}

// Reserve tries to reserve n bytes. Returns true if reserved.
// If capacity == 0 (unlimited) always succeeds.
// On transient shortage, it will attempt force-eviction across stores once, then fail.
func (mc *MemoryController) Reserve(n int64) bool {
	if n <= 0 {
		return true
	}
	// unlimited
	if mc.capacity == 0 {
		atomic.AddInt64(&mc.used, n)
		return true
	}

	// Fast path: try to add
	new := atomic.AddInt64(&mc.used, n)
	if new <= mc.capacity {
		return true
	}

	// Exceeded -> roll back and try eviction
	atomic.AddInt64(&mc.used, -n)

	// Trigger eviction across tenants to free up at most n bytes (best-effort).
	// Use short time window to avoid long GC or stalls.
	mc.forceEvictAcrossStores(n)

	// Retry once
	new = atomic.AddInt64(&mc.used, n)
	if new <= mc.capacity {
		return true
	}
	atomic.AddInt64(&mc.used, -n)
	return false
}

// Release releases previously reserved bytes.
func (mc *MemoryController) Release(n int64) {
	if n <= 0 {
		return
	}
	if mc.capacity == 0 {
		// no accounting needed for unlimited
		atomic.AddInt64(&mc.used, -n)
		return
	}
	atomic.AddInt64(&mc.used, -n)
	if atomic.LoadInt64(&mc.used) < 0 {
		atomic.StoreInt64(&mc.used, 0)
	}
}

// forceEvictAcrossStores tries to free target bytes by asking each tenant store to force-evict.
// This is best-effort and returns immediately after a short sweep.
func (mc *MemoryController) forceEvictAcrossStores(target int64) {
	if target <= 0 || mc.tm == nil {
		return
	}

	// iterate tenants and ask them to evict some bytes.
	// We cap per-store eviction attempt to avoid starving a single tenant.
	perStoreTarget := target / int64(len(mc.tm.stores)+1)
	if perStoreTarget <= 0 {
		perStoreTarget = target // try full target on first store
	}

	// Simple round-robin sweep with short sleeps to allow locks to settle.
	for _, id := range mc.tm.ListTenants() {
		s := mc.tm.GetStore(id)
		if s == nil {
			continue
		}
		// best-effort: ask store to evict perStoreTarget
		freed := s.ForceEvictBytes(perStoreTarget)
		if freed >= target {
			return
		}
		target -= freed
		// small pause to avoid tight loop
		time.Sleep(5 * time.Millisecond)
		if target <= 0 {
			return
		}
	}

	// If still need more, do another light pass with smaller per-store target
	for _, id := range mc.tm.ListTenants() {
		s := mc.tm.GetStore(id)
		if s == nil {
			continue
		}
		freed := s.ForceEvictBytes(int64(1024 * 32)) // try 32 KB chunks
		if freed <= 0 {
			continue
		}
		if freed >= target {
			return
		}
		target -= freed
	}
}
