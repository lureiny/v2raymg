package forward

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
)

// PortAllocator manages a pool of ports within a specified range.
// It is goroutine-safe.
type PortAllocator struct {
	mu            sync.Mutex
	minPort       uint32
	maxPort       uint32
	allocated     map[uint32]bool // ports currently allocated
	reserved      map[uint32]bool // ports reserved (e.g. used by containers)
	nextHint      uint32          // next port to try (round-robin hint)
	useRandom     bool            // use random allocation instead of round-robin
}

// PortAllocatorConfig configures a PortAllocator.
type PortAllocatorConfig struct {
	// MinPort is the lower bound of the allocatable range (inclusive).
	MinPort uint32 `yaml:"min_port" json:"min_port"`
	// MaxPort is the upper bound of the allocatable range (inclusive).
	MaxPort uint32 `yaml:"max_port" json:"max_port"`
	// Reserved is a set of ports that must never be allocated.
	Reserved []uint32 `yaml:"reserved" json:"reserved"`
	// UseRandom enables random port allocation instead of round-robin.
	// Random allocation is recommended to avoid predictable port patterns.
	UseRandom bool `yaml:"use_random" json:"use_random"`
	// ListenStack is the default IP stack for wildcard forward listeners:
	// "dual" (default, IPv4+IPv6), "ipv4", or "ipv6". This is carried in the
	// forward config section and consumed by the ForwardManager; the port
	// allocator itself ignores it. Individual rules override via
	// ForwardRule.ListenStack.
	ListenStack string `yaml:"listen_stack" json:"listen_stack"`
}

// NewPortAllocator creates a new PortAllocator.
func NewPortAllocator(cfg PortAllocatorConfig) (*PortAllocator, error) {
	if cfg.MinPort < 100 {
		cfg.MinPort = 100
	}
	if cfg.MaxPort > 65535 {
		cfg.MaxPort = 65535
	}
	if cfg.MinPort > cfg.MaxPort {
		return nil, fmt.Errorf("port_allocator: min_port(%d) > max_port(%d)", cfg.MinPort, cfg.MaxPort)
	}

	reserved := make(map[uint32]bool, len(cfg.Reserved))
	for _, p := range cfg.Reserved {
		reserved[p] = true
	}

	return &PortAllocator{
		minPort:   cfg.MinPort,
		maxPort:   cfg.MaxPort,
		allocated: make(map[uint32]bool),
		reserved:  reserved,
		nextHint:  cfg.MinPort,
		useRandom: cfg.UseRandom,
	}, nil
}

// Allocate assigns an available port from the pool.
// Returns the allocated port or an error if the pool is exhausted.
func (pa *PortAllocator) Allocate() (uint32, error) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	rangeSize := pa.maxPort - pa.minPort + 1

	// Build list of available ports
	var available []uint32
	for p := pa.minPort; p <= pa.maxPort; p++ {
		if !pa.allocated[p] && !pa.reserved[p] {
			available = append(available, p)
		}
	}

	if len(available) == 0 {
		return 0, fmt.Errorf("port_allocator: no available ports in range [%d, %d]", pa.minPort, pa.maxPort)
	}

	var selected uint32
	if pa.useRandom {
		// Random selection
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(available))))
		if err != nil {
			return 0, fmt.Errorf("port_allocator: failed to generate random index: %w", err)
		}
		selected = available[idx.Int64()]
	} else {
		// Round-robin selection (original behavior)
		for i := uint32(0); i < rangeSize; i++ {
			candidate := pa.minPort + ((pa.nextHint - pa.minPort + i) % rangeSize)
			if !pa.allocated[candidate] && !pa.reserved[candidate] {
				selected = candidate
				pa.nextHint = candidate + 1
				if pa.nextHint > pa.maxPort {
					pa.nextHint = pa.minPort
				}
				break
			}
		}
	}

	pa.allocated[selected] = true
	return selected, nil
}

// AllocateSpecific allocates a specific port. Returns error if already in use or reserved.
func (pa *PortAllocator) AllocateSpecific(port uint32) error {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	if port < pa.minPort || port > pa.maxPort {
		return fmt.Errorf("port_allocator: port %d out of range [%d, %d]", port, pa.minPort, pa.maxPort)
	}
	if pa.reserved[port] {
		return fmt.Errorf("port_allocator: port %d is reserved", port)
	}
	if pa.allocated[port] {
		return fmt.Errorf("port_allocator: port %d already allocated", port)
	}

	pa.allocated[port] = true
	return nil
}

// Release returns a port to the pool.
func (pa *PortAllocator) Release(port uint32) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	delete(pa.allocated, port)
}

// IsAllocated returns whether a port is currently allocated.
func (pa *PortAllocator) IsAllocated(port uint32) bool {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	return pa.allocated[port]
}

// IsReserved returns whether a port is reserved.
func (pa *PortAllocator) IsReserved(port uint32) bool {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	return pa.reserved[port]
}

// AddReserved adds a port to the reserved set.
// If the port is currently allocated, it remains allocated (caller should release first).
func (pa *PortAllocator) AddReserved(port uint32) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	pa.reserved[port] = true
}

// RemoveReserved removes a port from the reserved set.
func (pa *PortAllocator) RemoveReserved(port uint32) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	delete(pa.reserved, port)
}

// AllocatedCount returns the number of currently allocated ports.
func (pa *PortAllocator) AllocatedCount() int {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	return len(pa.allocated)
}

// Available returns the number of available (unallocated, unreserved) ports.
func (pa *PortAllocator) Available() int {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	total := int(pa.maxPort - pa.minPort + 1)
	unavailable := 0
	for p := pa.minPort; p <= pa.maxPort; p++ {
		if pa.allocated[p] || pa.reserved[p] {
			unavailable++
		}
	}
	return total - unavailable
}
