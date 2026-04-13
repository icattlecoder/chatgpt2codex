package tool

import "sync"

var fileLocks sync.Map

func withFileLock(path string, fn func() error) error {
	value, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	return fn()
}
