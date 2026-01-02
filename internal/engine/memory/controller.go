// File: internal/engine/memory/controller.go
package memory

import (
	"log"
	"sync/atomic"
	"time"
)

// Controller manages global memory budget
// Implements core.MemoryController interface
type Controller struct {
	capacity int64
	used     int64
	tenants  TenantManagerInterface
}

// TenantManagerInterface defines tenant operations
type TenantManagerInterface interface {
	ListTenants() []string
	GetStore(tenantID string) StoreInterface
}

// StoreInterface defines store eviction operations
type StoreInterface interface {
	ForceEvictBytes(target int64) int64
}

// NewController creates memory controller
func NewController(tenants TenantManagerInterface, capacityBytes int64) *Controller {
	return &Controller{
		capacity: capacityBytes,
		used:     0,
		tenants:  tenants,
	}
}

// Used returns current used bytes
func (mc *Controller) Used() int64 {
	return atomic.LoadInt64(&mc.used)
}

// Capacity returns capacity
func (mc *Controller) Capacity() int64 {
	return mc.capacity
}

// UsagePercent returns usage percentage
func (mc *Controller) UsagePercent() float64 {
	if mc.capacity == 0 {
		return 0
	}
	return float64(mc.Used()) / float64(mc.capacity) * 100
}

// Reserve tries to reserve n bytes
func (mc *Controller) Reserve(n int64) bool {
	if n <= 0 {
		return true
	}

	// Unlimited capacity
	if mc.capacity == 0 {
		atomic.AddInt64(&mc.used, n)
		return true
	}

	// Try fast path
	newUsed := atomic.AddInt64(&mc.used, n)
	if newUsed <= mc.capacity {
		return true
	}

	// Exceeded - rollback and try eviction
	atomic.AddInt64(&mc.used, -n)
	mc.forceEvictAcrossStores(n)

	// Retry once
	newUsed = atomic.AddInt64(&mc.used, n)
	if newUsed <= mc.capacity {
		return true
	}

	// Failed
	atomic.AddInt64(&mc.used, -n)
	return false
}

// Release releases previously reserved bytes
func (mc *Controller) Release(n int64) {
	if n <= 0 {
		return
	}

	atomic.AddInt64(&mc.used, -n)

	// Prevent underflow
	if atomic.LoadInt64(&mc.used) < 0 {
		atomic.StoreInt64(&mc.used, 0)
	}
}

// forceEvictAcrossStores attempts cross-tenant eviction
func (mc *Controller) forceEvictAcrossStores(target int64) {
	if target <= 0 || mc.tenants == nil {
		return
	}

	tenantIDs := mc.tenants.ListTenants()
	if len(tenantIDs) == 0 {
		return
	}

	// Calculate per-store target
	perStoreTarget := target / int64(len(tenantIDs))
	if perStoreTarget <= 0 {
		perStoreTarget = target
	}

	log.Printf("[MEMORY] Force eviction:  target=%d bytes across %d tenants", target, len(tenantIDs))

	// First pass: try evicting from each store
	for _, tenantID := range tenantIDs {
		store := mc.tenants.GetStore(tenantID)
		if store == nil {
			continue
		}

		freed := store.ForceEvictBytes(perStoreTarget)
		if freed > 0 {
			log.Printf("[MEMORY] Evicted %d bytes from tenant %s", freed, tenantID)
			target -= freed
		}

		if target <= 0 {
			return
		}

		// Small pause to let locks settle
		time.Sleep(5 * time.Millisecond)
	}

	// Second pass: smaller chunks
	if target > 0 {
		for _, tenantID := range tenantIDs {
			store := mc.tenants.GetStore(tenantID)
			if store == nil {
				continue
			}

			freed := store.ForceEvictBytes(32 * 1024) // 32KB chunks
			if freed > 0 {
				target -= freed
			}

			if target <= 0 {
				return
			}
		}
	}

	if target > 0 {
		log.Printf("[MEMORY] Force eviction incomplete: %d bytes still needed", target)
	}
}
