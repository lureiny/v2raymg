package util

import (
	"fmt"
	"os"
	"sync"
)

type MutexFunc func() error

// OpWithRlock ...
func OpWithRlock(mutex interface{}, op MutexFunc) error {
	if m, ok := mutex.(*sync.RWMutex); ok {
		(*m).RLock()
		defer (*m).RUnlock()
		return op()
	}
	return fmt.Errorf("mutex type is not sync.RWMutex")
}

// OpWithWlock ...
func OpWithWlock(mutex interface{}, op MutexFunc) error {
	if m, ok := mutex.(*sync.RWMutex); ok {
		(*m).Lock()
		defer (*m).Unlock()
		return op()
	} else if m, ok := mutex.(*sync.Mutex); ok {
		(*m).Lock()
		defer (*m).Unlock()
		return op()
	}
	return fmt.Errorf("mutex type is not sync.RWMutex or sync.Mutex")
}

// SwitchExec ...
func SwitchExec(src, dst string) error {
	os.Chmod(src, 0755)
	return os.Rename(src, dst)
}
