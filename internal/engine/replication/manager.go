// File: internal/engine/replication/manager.go
package replication

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Manager handles replication coordination
type Manager struct {
	nodeID       string
	mode         ReplicationMode
	tenants      TenantManagerInterface
	peers        map[string]*Peer
	peersMu      sync.RWMutex
	isLeader     atomic.Bool
	opLog        *OpLog
	ctx          context.Context
	cancel       context.CancelFunc
	writeQuorum  int
	maxLagMillis int64
	stats        struct {
		TotalOps         atomic.Uint64
		ReplicatedOps    atomic.Uint64
		FailedReplicas   atomic.Uint64
		AverageLatencyMs atomic.Int64
		CurrentLag       atomic.Int64
	}
}

// NewManager creates replication manager
func NewManager(nodeID string, mode ReplicationMode, tenants TenantManagerInterface) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	rm := &Manager{
		nodeID:       nodeID,
		mode:         mode,
		tenants:      tenants,
		peers:        make(map[string]*Peer),
		opLog:        NewOpLog(100000),
		ctx:          ctx,
		cancel:       cancel,
		writeQuorum:  1,
		maxLagMillis: 5000,
	}

	rm.isLeader.Store(true)

	// Start background workers
	go rm.healthCheckLoop()
	go rm.metricsLoop()

	log.Printf("[REPLICATION] Manager started:  node=%s, mode=%s", nodeID, mode)

	return rm
}

// Replicate replicates operation to peers
func (rm *Manager) Replicate(op ReplicaOp) error {
	if !rm.isLeader.Load() {
		return ErrNotLeader
	}

	op.Timestamp = time.Now().UnixNano()
	seqNum := rm.opLog.Append(op)
	op.SeqNum = seqNum

	rm.stats.TotalOps.Add(1)

	switch rm.mode {
	case ModeAsync:
		go rm.replicateAsync(op)
		return nil

	case ModeSync:
		return rm.replicateSync(op)

	case ModeSemiSync:
		return rm.replicateSemiSync(op)

	default:
		return fmt.Errorf("unknown replication mode: %d", rm.mode)
	}
}

// replicateAsync sends operation to all peers without waiting
func (rm *Manager) replicateAsync(op ReplicaOp) {
	peers := rm.getHealthyPeers()

	for _, peer := range peers {
		go rm.sendOpToPeer(peer, op)
	}
}

// replicateSync waits for all peers to acknowledge
func (rm *Manager) replicateSync(op ReplicaOp) error {
	peers := rm.getHealthyPeers()

	if len(peers) == 0 {
		return ErrNoHealthyPeers
	}

	errCh := make(chan error, len(peers))
	for _, peer := range peers {
		go func(p *Peer) {
			errCh <- rm.sendOpToPeer(p, op)
		}(peer)
	}

	successCount := 0
	for i := 0; i < len(peers); i++ {
		if err := <-errCh; err == nil {
			successCount++
		}
	}

	if successCount == len(peers) {
		rm.stats.ReplicatedOps.Add(1)
		return nil
	}

	rm.stats.FailedReplicas.Add(1)
	return fmt.Errorf("sync replication failed: %d/%d peers succeeded", successCount, len(peers))
}

// replicateSemiSync waits for quorum
func (rm *Manager) replicateSemiSync(op ReplicaOp) error {
	peers := rm.getHealthyPeers()

	if len(peers) == 0 {
		return ErrNoHealthyPeers
	}

	quorum := rm.calculateQuorum(len(peers))

	errCh := make(chan error, len(peers))
	for _, peer := range peers {
		go func(p *Peer) {
			errCh <- rm.sendOpToPeer(p, op)
		}(peer)
	}

	successCount := 0
	timeout := time.After(100 * time.Millisecond)

	for i := 0; i < len(peers); i++ {
		select {
		case err := <-errCh:
			if err == nil {
				successCount++
				if successCount >= quorum {
					rm.stats.ReplicatedOps.Add(1)
					// Drain remaining responses
					go func() {
						for j := i + 1; j < len(peers); j++ {
							<-errCh
						}
					}()
					return nil
				}
			}
		case <-timeout:
			if successCount >= quorum {
				rm.stats.ReplicatedOps.Add(1)
				return nil
			}
			rm.stats.FailedReplicas.Add(1)
			return fmt.Errorf("semi-sync timeout:  %d/%d quorum not met", successCount, quorum)
		}
	}

	if successCount >= quorum {
		rm.stats.ReplicatedOps.Add(1)
		return nil
	}

	rm.stats.FailedReplicas.Add(1)
	return fmt.Errorf("semi-sync failed: %d/%d quorum not met", successCount, quorum)
}

// calculateQuorum computes required quorum size
func (rm *Manager) calculateQuorum(peerCount int) int {
	if rm.writeQuorum > 0 {
		return rm.writeQuorum
	}
	return (peerCount / 2) + 1
}

// getHealthyPeers returns list of healthy peers
func (rm *Manager) getHealthyPeers() []*Peer {
	rm.peersMu.RLock()
	defer rm.peersMu.RUnlock()

	peers := make([]*Peer, 0, len(rm.peers))
	for _, peer := range rm.peers {
		if peer.GetStatus() == PeerHealthy {
			peers = append(peers, peer)
		}
	}
	return peers
}

// AddPeer adds replication peer
func (rm *Manager) AddPeer(peerID, addr string) error {
	return rm.addPeerInternal(peerID, addr, false)
}

// addPeerInternal adds peer (internal, supports mock)
func (rm *Manager) addPeerInternal(peerID, addr string, isMock bool) error {
	conn, err := connectToPeer(addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to peer %s: %w", peerID, err)
	}

	peer := newPeer(peerID, addr, conn, isMock)

	rm.peersMu.Lock()
	rm.peers[peerID] = peer
	rm.peersMu.Unlock()

	if !isMock {
		go rm.handlePeer(peer)
	}

	log.Printf("[REPLICATION] Peer added: %s (%s)", peerID, addr)
	return nil
}

// RemovePeer removes peer
func (rm *Manager) RemovePeer(peerID string) {
	rm.peersMu.Lock()
	peer, ok := rm.peers[peerID]
	if ok {
		peer.conn.Close()
		delete(rm.peers, peerID)
	}
	rm.peersMu.Unlock()

	log.Printf("[REPLICATION] Peer removed: %s", peerID)
}

// SyncFromPeer syncs operations from leader
func (rm *Manager) SyncFromPeer(peerID string, fromSeq uint64) error {
	rm.peersMu.RLock()
	peer, ok := rm.peers[peerID]
	rm.peersMu.RUnlock()

	if !ok {
		return fmt.Errorf("peer not found: %s", peerID)
	}

	ops := rm.opLog.GetSince(fromSeq)

	for _, op := range ops {
		if err := rm.sendOpToPeer(peer, op); err != nil {
			return fmt.Errorf("sync failed at seqNum %d: %w", op.SeqNum, err)
		}
	}

	log.Printf("[REPLICATION] Synced %d ops to peer %s", len(ops), peerID)
	return nil
}

// SetMode changes replication mode
func (rm *Manager) SetMode(mode ReplicationMode) {
	rm.mode = mode
	log.Printf("[REPLICATION] Mode changed to:  %s", mode)
}

// SetWriteQuorum sets write quorum
func (rm *Manager) SetWriteQuorum(quorum int) {
	rm.writeQuorum = quorum
	log.Printf("[REPLICATION] Write quorum set to: %d", quorum)
}

// SetLeader sets leader status
func (rm *Manager) SetLeader(isLeader bool) {
	rm.isLeader.Store(isLeader)
	log.Printf("[REPLICATION] Leader status:  %v", isLeader)
}

// GetStats returns replication statistics
func (rm *Manager) GetStats() ReplicationStats {
	rm.peersMu.RLock()
	activeCount := len(rm.peers)
	healthyCount := 0
	degradedCount := 0
	downCount := 0

	for _, peer := range rm.peers {
		switch peer.GetStatus() {
		case PeerHealthy:
			healthyCount++
		case PeerDegraded:
			degradedCount++
		case PeerDown:
			downCount++
		}
	}
	rm.peersMu.RUnlock()

	return ReplicationStats{
		TotalOps:         rm.stats.TotalOps.Load(),
		ReplicatedOps:    rm.stats.ReplicatedOps.Load(),
		FailedReplicas:   rm.stats.FailedReplicas.Load(),
		AverageLatencyMs: rm.stats.AverageLatencyMs.Load(),
		CurrentLag:       rm.stats.CurrentLag.Load(),
		ActivePeers:      activeCount,
		HealthyPeers:     healthyCount,
		DegradedPeers:    degradedCount,
		DownPeers:        downCount,
	}
}

// Shutdown gracefully stops replication
func (rm *Manager) Shutdown() {
	log.Println("[REPLICATION] Shutting down...")
	rm.cancel()

	rm.peersMu.Lock()
	for _, peer := range rm.peers {
		peer.conn.Close()
	}
	rm.peersMu.Unlock()
}

// Background loops

func (rm *Manager) healthCheckLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			rm.checkPeerHealth()
		}
	}
}

func (rm *Manager) metricsLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rm.ctx.Done():
			return
		case <-ticker.C:
			rm.logMetrics()
		}
	}
}

func (rm *Manager) logMetrics() {
	stats := rm.GetStats()
	log.Printf("[REPLICATION] Stats: TotalOps=%d, Replicated=%d, Failed=%d, AvgLatency=%dms, Peers=%d/%d/%d",
		stats.TotalOps, stats.ReplicatedOps, stats.FailedReplicas, stats.AverageLatencyMs,
		stats.HealthyPeers, stats.DegradedPeers, stats.DownPeers)
}
