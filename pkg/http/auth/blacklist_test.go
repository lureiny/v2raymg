package auth

import (
	"fmt"
	"sync"
	"testing"
)

func TestBlacklist_AddAndContains(t *testing.T) {
	jti := "test-blacklist-add-" + t.Name()
	BlacklistAdd(jti)
	if !BlacklistContains(jti) {
		t.Fatalf("expected blacklist to contain %q after Add", jti)
	}
}

func TestBlacklist_NotAdded(t *testing.T) {
	jti := "test-blacklist-notadded-" + t.Name()
	if BlacklistContains(jti) {
		t.Fatalf("expected blacklist not to contain %q before Add", jti)
	}
}

func TestBlacklist_Concurrent(t *testing.T) {
	const n = 50
	var wg sync.WaitGroup
	jtis := make([]string, n)
	for i := 0; i < n; i++ {
		jtis[i] = fmt.Sprintf("concurrent-blacklist-%s-%d", t.Name(), i)
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(jti string) {
			defer wg.Done()
			BlacklistAdd(jti)
			BlacklistContains(jti)
		}(jtis[i])
	}
	wg.Wait()
	// All added JTIs must be present
	for _, jti := range jtis {
		if !BlacklistContains(jti) {
			t.Errorf("expected blacklist to contain %q after concurrent Add", jti)
		}
	}
}
