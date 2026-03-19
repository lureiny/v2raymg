package forward

import (
	"sync"
	"testing"
)

func TestTrafficCounter_Basic(t *testing.T) {
	tc := NewTrafficCounter()

	tc.AddUpload(100)
	tc.AddUpload(200)
	tc.AddDownload(500)

	if tc.Upload() != 300 {
		t.Errorf("expected upload 300, got %d", tc.Upload())
	}
	if tc.Download() != 500 {
		t.Errorf("expected download 500, got %d", tc.Download())
	}
}

func TestTrafficCounter_ActiveConns(t *testing.T) {
	tc := NewTrafficCounter()

	tc.IncrConns()
	tc.IncrConns()
	if tc.ActiveConns() != 2 {
		t.Errorf("expected 2 active, got %d", tc.ActiveConns())
	}

	tc.DecrConns()
	if tc.ActiveConns() != 1 {
		t.Errorf("expected 1 active, got %d", tc.ActiveConns())
	}
}

func TestTrafficCounter_Snapshot(t *testing.T) {
	tc := NewTrafficCounter()
	tc.AddUpload(1000)
	tc.AddDownload(2000)
	tc.IncrConns()

	// Snapshot without reset
	up, down, active := tc.Snapshot(false)
	if up != 1000 || down != 2000 || active != 1 {
		t.Errorf("snapshot: up=%d down=%d active=%d", up, down, active)
	}
	// Values should still be there
	if tc.Upload() != 1000 {
		t.Error("upload should not be reset")
	}

	// Snapshot with reset
	up, down, _ = tc.Snapshot(true)
	if up != 1000 || down != 2000 {
		t.Errorf("snapshot-reset: up=%d down=%d", up, down)
	}
	// Values should be 0 now
	if tc.Upload() != 0 {
		t.Errorf("upload should be 0 after reset, got %d", tc.Upload())
	}
	if tc.Download() != 0 {
		t.Errorf("download should be 0 after reset, got %d", tc.Download())
	}
}

func TestTrafficCounter_Concurrent(t *testing.T) {
	tc := NewTrafficCounter()
	var wg sync.WaitGroup

	// 100 goroutines each add 1000 bytes
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				tc.AddUpload(1)
				tc.AddDownload(1)
			}
		}()
	}
	wg.Wait()

	if tc.Upload() != 100000 {
		t.Errorf("expected upload 100000, got %d", tc.Upload())
	}
	if tc.Download() != 100000 {
		t.Errorf("expected download 100000, got %d", tc.Download())
	}
}

func TestTrafficRegistry_GetOrCreate(t *testing.T) {
	reg := NewTrafficRegistry()

	tc1 := reg.GetOrCreate("rule-1")
	tc1.AddUpload(100)

	tc2 := reg.GetOrCreate("rule-1") // should return same instance
	if tc2.Upload() != 100 {
		t.Error("GetOrCreate should return existing counter")
	}

	tc3 := reg.GetOrCreate("rule-2") // different key
	if tc3.Upload() != 0 {
		t.Error("new counter should start at 0")
	}
}

func TestTrafficRegistry_Get(t *testing.T) {
	reg := NewTrafficRegistry()

	if reg.Get("nonexistent") != nil {
		t.Error("Get should return nil for nonexistent key")
	}

	reg.GetOrCreate("exists")
	if reg.Get("exists") == nil {
		t.Error("Get should return counter for existing key")
	}
}

func TestTrafficRegistry_Remove(t *testing.T) {
	reg := NewTrafficRegistry()
	reg.GetOrCreate("to-remove")
	reg.Remove("to-remove")

	if reg.Get("to-remove") != nil {
		t.Error("counter should be removed")
	}
}

func TestTrafficRegistry_SnapshotAll(t *testing.T) {
	reg := NewTrafficRegistry()

	tc1 := reg.GetOrCreate("rule-a")
	tc1.AddUpload(100)
	tc1.AddDownload(200)

	tc2 := reg.GetOrCreate("rule-b")
	tc2.AddUpload(300)
	tc2.AddDownload(400)

	// Snapshot without reset
	snap := reg.SnapshotAll(false)
	if len(snap) != 2 {
		t.Errorf("expected 2 snapshots, got %d", len(snap))
	}
	if snap["rule-a"].Upload != 100 {
		t.Errorf("rule-a upload: expected 100, got %d", snap["rule-a"].Upload)
	}
	if snap["rule-b"].Download != 400 {
		t.Errorf("rule-b download: expected 400, got %d", snap["rule-b"].Download)
	}

	// Snapshot with reset
	snap2 := reg.SnapshotAll(true)
	if snap2["rule-a"].Upload != 100 {
		t.Errorf("rule-a upload before reset: expected 100, got %d", snap2["rule-a"].Upload)
	}

	// After reset, should be 0
	snap3 := reg.SnapshotAll(false)
	if snap3["rule-a"].Upload != 0 {
		t.Errorf("rule-a upload after reset: expected 0, got %d", snap3["rule-a"].Upload)
	}
}
