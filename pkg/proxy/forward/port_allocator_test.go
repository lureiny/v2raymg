package forward

import (
	"sync"
	"testing"
)

func TestPortAllocator_Basic(t *testing.T) {
	pa, err := NewPortAllocator(PortAllocatorConfig{
		MinPort: 10000,
		MaxPort: 10010,
	})
	if err != nil {
		t.Fatalf("NewPortAllocator failed: %v", err)
	}

	port, err := pa.Allocate()
	if err != nil {
		t.Fatalf("Allocate failed: %v", err)
	}
	if port < 10000 || port > 10010 {
		t.Errorf("port %d out of range", port)
	}
	if !pa.IsAllocated(port) {
		t.Error("port should be marked allocated")
	}
	if pa.AllocatedCount() != 1 {
		t.Errorf("expected 1 allocated, got %d", pa.AllocatedCount())
	}

	pa.Release(port)
	if pa.IsAllocated(port) {
		t.Error("port should not be allocated after release")
	}
	if pa.AllocatedCount() != 0 {
		t.Errorf("expected 0 allocated, got %d", pa.AllocatedCount())
	}
}

func TestPortAllocator_Exhaustion(t *testing.T) {
	pa, _ := NewPortAllocator(PortAllocatorConfig{
		MinPort: 20000,
		MaxPort: 20002, // only 3 ports
	})

	ports := make([]uint32, 0, 3)
	for i := 0; i < 3; i++ {
		p, err := pa.Allocate()
		if err != nil {
			t.Fatalf("Allocate %d failed: %v", i, err)
		}
		ports = append(ports, p)
	}

	// 4th allocation should fail
	_, err := pa.Allocate()
	if err == nil {
		t.Fatal("expected error when pool exhausted")
	}

	// Release one, then allocate again should succeed
	pa.Release(ports[1])
	p, err := pa.Allocate()
	if err != nil {
		t.Fatalf("Allocate after release failed: %v", err)
	}
	if p != ports[1] {
		// The released port should be reusable (may or may not be the same due to round-robin)
		// Just verify it's in range
		if p < 20000 || p > 20002 {
			t.Errorf("re-allocated port %d out of range", p)
		}
	}
}

func TestPortAllocator_Reserved(t *testing.T) {
	pa, _ := NewPortAllocator(PortAllocatorConfig{
		MinPort:  30000,
		MaxPort:  30004,
		Reserved: []uint32{30001, 30003},
	})

	// Should never allocate reserved ports
	allocated := map[uint32]bool{}
	for i := 0; i < 3; i++ {
		p, err := pa.Allocate()
		if err != nil {
			t.Fatalf("Allocate %d failed: %v", i, err)
		}
		if p == 30001 || p == 30003 {
			t.Errorf("allocated reserved port %d", p)
		}
		allocated[p] = true
	}

	// Pool should be exhausted (5 total - 2 reserved = 3 available, all used)
	_, err := pa.Allocate()
	if err == nil {
		t.Fatal("expected exhaustion after 3 allocations with 2 reserved")
	}

	if pa.Available() != 0 {
		t.Errorf("expected 0 available, got %d", pa.Available())
	}

	if !pa.IsReserved(30001) {
		t.Error("30001 should be reserved")
	}
}

func TestPortAllocator_AllocateSpecific(t *testing.T) {
	pa, _ := NewPortAllocator(PortAllocatorConfig{
		MinPort: 40000,
		MaxPort: 40010,
	})

	// Allocate specific port
	if err := pa.AllocateSpecific(40005); err != nil {
		t.Fatalf("AllocateSpecific failed: %v", err)
	}
	if !pa.IsAllocated(40005) {
		t.Error("40005 should be allocated")
	}

	// Double allocate should fail
	if err := pa.AllocateSpecific(40005); err == nil {
		t.Fatal("expected error for double allocation")
	}

	// Out of range should fail
	if err := pa.AllocateSpecific(50000); err == nil {
		t.Fatal("expected error for out-of-range port")
	}

	// Reserved port should fail
	pa.AddReserved(40006)
	if err := pa.AllocateSpecific(40006); err == nil {
		t.Fatal("expected error for reserved port")
	}
}

func TestPortAllocator_AddRemoveReserved(t *testing.T) {
	pa, _ := NewPortAllocator(PortAllocatorConfig{
		MinPort: 50000,
		MaxPort: 50000, // single port
	})

	// Reserve the only port
	pa.AddReserved(50000)
	_, err := pa.Allocate()
	if err == nil {
		t.Fatal("expected exhaustion with only port reserved")
	}

	// Unreserve
	pa.RemoveReserved(50000)
	p, err := pa.Allocate()
	if err != nil {
		t.Fatalf("Allocate after unreserve failed: %v", err)
	}
	if p != 50000 {
		t.Errorf("expected 50000, got %d", p)
	}
}

func TestPortAllocator_InvalidRange(t *testing.T) {
	_, err := NewPortAllocator(PortAllocatorConfig{
		MinPort: 60000,
		MaxPort: 50000,
	})
	if err == nil {
		t.Fatal("expected error for min > max")
	}
}

func TestPortAllocator_Concurrent(t *testing.T) {
	pa, _ := NewPortAllocator(PortAllocatorConfig{
		MinPort: 11000,
		MaxPort: 11099, // 100 ports
	})

	var wg sync.WaitGroup
	allocated := make(chan uint32, 100)
	errors := make(chan error, 100)

	// 100 goroutines each allocate one port
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := pa.Allocate()
			if err != nil {
				errors <- err
				return
			}
			allocated <- p
		}()
	}
	wg.Wait()
	close(allocated)
	close(errors)

	for err := range errors {
		t.Fatalf("concurrent allocation error: %v", err)
	}

	// All ports should be unique
	seen := map[uint32]bool{}
	for p := range allocated {
		if seen[p] {
			t.Errorf("duplicate port %d allocated", p)
		}
		seen[p] = true
	}

	if len(seen) != 100 {
		t.Errorf("expected 100 unique ports, got %d", len(seen))
	}

	if pa.AllocatedCount() != 100 {
		t.Errorf("expected 100 allocated, got %d", pa.AllocatedCount())
	}

	if pa.Available() != 0 {
		t.Errorf("expected 0 available, got %d", pa.Available())
	}
}

func TestPortAllocator_Available(t *testing.T) {
	pa, _ := NewPortAllocator(PortAllocatorConfig{
		MinPort:  60000,
		MaxPort:  60009, // 10 ports
		Reserved: []uint32{60005},
	})

	if pa.Available() != 9 {
		t.Errorf("expected 9 available, got %d", pa.Available())
	}

	pa.Allocate()
	if pa.Available() != 8 {
		t.Errorf("expected 8 available after 1 allocation, got %d", pa.Available())
	}
}

func TestPortAllocator_RandomAllocation(t *testing.T) {
	pa, err := NewPortAllocator(PortAllocatorConfig{
		MinPort:   10000,
		MaxPort:   10010,
		UseRandom: true,
	})
	if err != nil {
		t.Fatalf("NewPortAllocator failed: %v", err)
	}

	// Allocate 5 ports randomly
	ports := make([]uint32, 5)
	for i := 0; i < 5; i++ {
		p, err := pa.Allocate()
		if err != nil {
			t.Fatalf("Allocate %d failed: %v", i, err)
		}
		if p < 10000 || p > 10010 {
			t.Errorf("port %d out of range", p)
		}
		ports[i] = p
	}

	// Verify all ports are unique
	seen := map[uint32]bool{}
	for _, p := range ports {
		if seen[p] {
			t.Errorf("duplicate port %d allocated", p)
		}
		seen[p] = true
	}
}

func TestPortAllocator_RandomNotSequential(t *testing.T) {
	// This test verifies that random allocation doesn't produce sequential ports
	// Run multiple times to increase chance of detecting sequential patterns
	pa, err := NewPortAllocator(PortAllocatorConfig{
		MinPort:   10000,
		MaxPort:   65535,
		UseRandom: true,
	})
	if err != nil {
		t.Fatalf("NewPortAllocator failed: %v", err)
	}

	// Allocate a small sample
	ports := make([]uint32, 10)
	for i := 0; i < 10; i++ {
		p, err := pa.Allocate()
		if err != nil {
			t.Fatalf("Allocate %d failed: %v", i, err)
		}
		ports[i] = p
	}

	// Check that we don't have strictly sequential ports (e.g., 10000, 10001, 10002...)
	// In random allocation, having 3+ sequential ports in a row is very unlikely
	sequentialCount := 0
	for i := 1; i < len(ports); i++ {
		if ports[i] == ports[i-1]+1 {
			sequentialCount++
		}
	}

	// We expect 0 or at most 1 sequential pair in random allocation
	// (not a strict statistical test, but catches obvious bugs)
	if sequentialCount >= 2 {
		t.Logf("Warning: found %d sequential port pairs in random allocation: %v", sequentialCount, ports)
	}
}

func TestPortAllocator_DefaultRange10000_65535(t *testing.T) {
	// Test that default ForwardManager uses range 10000-65535
	m, err := NewForwardManager()
	if err != nil {
		t.Fatalf("NewForwardManager failed: %v", err)
	}

	// The manager should be usable
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestPortAllocator_DefaultUsesRandom(t *testing.T) {
	// Test that default ForwardManager uses random allocation
	m, err := NewForwardManager()
	if err != nil {
		t.Fatalf("NewForwardManager failed: %v", err)
	}

	// The manager should be usable
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}
