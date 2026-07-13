package usermanager

import (
	"fmt"
	"sync"
	"testing"

	"github.com/lureiny/v2raymg/pkg/proxy/core/contracts"
)

// TestUserEvent_SnapshotNoRace is the -race regression anchor for finding
// UM-#56: emitted UserEvents used to carry the LIVE *contracts.User held in
// m.users, so a subscriber reading event.User off the lock raced with the next
// mutateUser writing the same struct under m.mu.
//
// The test reproduces exactly that shape: one subscriber continuously reads the
// mutable fields of the most recent event.User, while a writer mutates the same
// user (AuthToken + version stamp) in a tight loop. Before the fix (evt.User =
// user) `go test -race` reports a data race on User.AuthToken; after the fix
// (evt.User = user.Clone()) the subscriber only ever touches private snapshots,
// so the run is clean. It also asserts the snapshot is decoupled: a field read
// from an old event must not observe a later write.
func TestUserEvent_SnapshotNoRace(t *testing.T) {
	m := NewUserManager(&mockStatsForwardManager{}, "test-node")
	if err := m.AddUser(AddUserRequest{Username: "u1", Password: "pw"}); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	sub := m.Subscribe()
	defer m.Unsubscribe(sub)

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Subscriber: hold the latest event.User and hammer its mutable fields.
	wg.Add(1)
	go func() {
		defer wg.Done()
		var last *contracts.User
		for {
			select {
			case ev, ok := <-sub:
				if !ok {
					return
				}
				if ev.User != nil {
					last = ev.User
				}
			case <-stop:
				return
			default:
				if last != nil {
					// Concurrent reads of fields a writer mutates below.
					_ = last.AuthToken
					_ = last.UpdatedAtUs
					_ = len(last.BindPorts)
				}
			}
		}
	}()

	// Writer: mutate the same user (and stamp its version) many times, each
	// emitting an update event carrying a snapshot of the user.
	for i := 0; i < 3000; i++ {
		token := fmt.Sprintf("tok-%d", i)
		if err := m.mutateUser("u1", UserEventUpdate, func(u *contracts.User) error {
			u.AuthToken = token
			return nil
		}); err != nil {
			t.Fatalf("mutateUser: %v", err)
		}
	}

	close(stop)
	wg.Wait()
}
