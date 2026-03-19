package forward

import (
	"sync"
	"sync/atomic"
)

// TrafficCounter tracks upload/download bytes and active connections
// for a single forwarding rule. All operations are goroutine-safe.
type TrafficCounter struct {
	upload      int64 // atomic
	download    int64 // atomic
	activeConns int64 // atomic
}

// NewTrafficCounter creates a new TrafficCounter.
func NewTrafficCounter() *TrafficCounter {
	return &TrafficCounter{}
}

// AddUpload adds n bytes to the upload counter.
func (tc *TrafficCounter) AddUpload(n int64) {
	atomic.AddInt64(&tc.upload, n)
}

// AddDownload adds n bytes to the download counter.
func (tc *TrafficCounter) AddDownload(n int64) {
	atomic.AddInt64(&tc.download, n)
}

// Upload returns the current upload byte count.
func (tc *TrafficCounter) Upload() int64 {
	return atomic.LoadInt64(&tc.upload)
}

// Download returns the current download byte count.
func (tc *TrafficCounter) Download() int64 {
	return atomic.LoadInt64(&tc.download)
}

// IncrConns increments the active connection count.
func (tc *TrafficCounter) IncrConns() {
	atomic.AddInt64(&tc.activeConns, 1)
}

// DecrConns decrements the active connection count.
func (tc *TrafficCounter) DecrConns() {
	atomic.AddInt64(&tc.activeConns, -1)
}

// ActiveConns returns the current number of active connections.
func (tc *TrafficCounter) ActiveConns() int64 {
	return atomic.LoadInt64(&tc.activeConns)
}

// Snapshot returns the current traffic values.
// If reset is true, upload and download are atomically swapped to 0.
func (tc *TrafficCounter) Snapshot(reset bool) (upload, download, activeConns int64) {
	if reset {
		upload = atomic.SwapInt64(&tc.upload, 0)
		download = atomic.SwapInt64(&tc.download, 0)
	} else {
		upload = atomic.LoadInt64(&tc.upload)
		download = atomic.LoadInt64(&tc.download)
	}
	activeConns = atomic.LoadInt64(&tc.activeConns)
	return
}

// TrafficRegistry manages TrafficCounters for multiple rules.
// It is goroutine-safe.
type TrafficRegistry struct {
	mu       sync.RWMutex
	counters map[string]*TrafficCounter // key = ruleKey
}

// NewTrafficRegistry creates a new TrafficRegistry.
func NewTrafficRegistry() *TrafficRegistry {
	return &TrafficRegistry{
		counters: make(map[string]*TrafficCounter),
	}
}

// GetOrCreate returns the counter for the given key, creating one if needed.
func (tr *TrafficRegistry) GetOrCreate(ruleKey string) *TrafficCounter {
	tr.mu.RLock()
	if tc, ok := tr.counters[ruleKey]; ok {
		tr.mu.RUnlock()
		return tc
	}
	tr.mu.RUnlock()

	tr.mu.Lock()
	defer tr.mu.Unlock()
	// Double-check after write lock
	if tc, ok := tr.counters[ruleKey]; ok {
		return tc
	}
	tc := NewTrafficCounter()
	tr.counters[ruleKey] = tc
	return tc
}

// Get returns the counter for the given key, or nil if not found.
func (tr *TrafficRegistry) Get(ruleKey string) *TrafficCounter {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	return tr.counters[ruleKey]
}

// Remove removes the counter for the given key.
func (tr *TrafficRegistry) Remove(ruleKey string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	delete(tr.counters, ruleKey)
}

// SnapshotAll returns traffic snapshots for all rules.
// If reset is true, counters are reset after reading.
func (tr *TrafficRegistry) SnapshotAll(reset bool) map[string]TrafficSnapshot {
	tr.mu.RLock()
	defer tr.mu.RUnlock()

	result := make(map[string]TrafficSnapshot, len(tr.counters))
	for key, tc := range tr.counters {
		upload, download, active := tc.Snapshot(reset)
		result[key] = TrafficSnapshot{
			Upload:            upload,
			Download:          download,
			ActiveConnections: active,
		}
	}
	return result
}
