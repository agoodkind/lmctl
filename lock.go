package lmctl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
)

// errLockNotAcquired indicates that the advisory lock was already held
// by another process at the moment we attempted to acquire it.
var errLockNotAcquired = errors.New("lock not acquired")

func defaultLockPath() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "lmctl", "load.lock")
}

// acquireLock takes an advisory file lock, serializing model load/evict
// across processes. Returns an unlock function. Errors are logged at
// the boundary so failures are visible without callers needing to
// re-emit the same context.
func acquireLock(ctx context.Context, path string) (func(), error) {
	log := slog.Default().With("component", "lmctl", "lock_path", path)

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		log.ErrorContext(ctx, "failed to create lock dir", "err", err)
		return nil, fmt.Errorf("create lock dir: %w", err)
	}

	fl := flock.New(path)
	ok, err := fl.TryLockContext(ctx, 0)
	if err != nil {
		log.ErrorContext(ctx, "failed to acquire lock", "err", err)
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !ok {
		log.WarnContext(ctx, "lock already held by another process")
		return nil, errLockNotAcquired
	}
	return func() { _ = fl.Unlock() }, nil
}
