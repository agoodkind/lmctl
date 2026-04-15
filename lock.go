package lmctl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

func defaultLockPath() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "lmctl", "load.lock")
}

// acquireLock takes an advisory file lock, serializing model load/evict
// across processes. Returns an unlock function.
func acquireLock(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}

	fl := flock.New(path)
	ok, err := fl.TryLockContext(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("lock not acquired")
	}
	return func() { _ = fl.Unlock() }, nil
}
