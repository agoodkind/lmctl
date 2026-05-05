// Package lmctl manages LM Studio model lifecycle: loading, unloading,
// and LRU eviction within a memory budget. It serializes operations
// across processes via a filesystem lock so concurrent tools (lm-review,
// clotilde, etc.) don't race.
package lmctl

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

// Option configures EnsureLoaded behavior.
type Option func(*cfg)

// WithContextLength sets the minimum context length (tokens) the model
// must be loaded with. Pass 0 to skip the context check.
func WithContextLength(n int) Option { return func(c *cfg) { c.contextLen = n } }

// WithMaxMemoryBytes sets the memory budget in bytes. Models are evicted
// LRU-first when loading would exceed this budget. 0 disables eviction.
func WithMaxMemoryBytes(b int64) Option { return func(c *cfg) { c.maxMemBytes = b } }

// WithMaxMemoryGB is a convenience wrapper around WithMaxMemoryBytes.
func WithMaxMemoryGB(gb int) Option {
	return func(c *cfg) { c.maxMemBytes = int64(gb) * 1024 * 1024 * 1024 }
}

// WithLoadTimeout sets the maximum time to wait for `lms load` to complete.
// Default is 120s. Pass 0 to disable the timeout.
func WithLoadTimeout(d time.Duration) Option { return func(c *cfg) { c.loadTimeout = d } }

// WithLogger sets the logger for model lifecycle events.
// Defaults to slog.Default().
func WithLogger(l *slog.Logger) Option { return func(c *cfg) { c.log = l } }

// WithLockPath overrides the filesystem lock location.
// Default is $XDG_CACHE_HOME/lmctl/load.lock.
func WithLockPath(p string) Option { return func(c *cfg) { c.lockPath = p } }

// WithTTL sets the idle timeout in seconds. After this many seconds of
// inactivity, LM Studio unloads the model. The minimum is 1 second.
// By default lmctl does not pass --ttl, which means models loaded via
// `lms load` have no TTL and stay resident until explicitly unloaded.
func WithTTL(seconds int) Option { return func(c *cfg) { c.ttlSeconds = seconds } }

// WithWarmup fires a throwaway 1-token completion after loading to force
// Metal shader compilation, KV cache allocation, and graph setup. The
// first real request will be fast instead of paying the cold-start cost.
// Requires the API base URL and token so lmctl can hit the /v1 endpoint.
func WithWarmup(apiURL, token string) Option {
	return func(c *cfg) {
		c.warmup = true
		c.warmupURL = apiURL
		c.warmupToken = token
	}
}

type cfg struct {
	contextLen  int
	maxMemBytes int64
	loadTimeout time.Duration
	log         *slog.Logger
	lockPath    string
	ttlSeconds  int // 0 = no TTL flag (models stay resident)
	warmup      bool
	warmupURL   string
	warmupToken string
}

func defaults() *cfg {
	return &cfg{
		loadTimeout: 120 * time.Second,
		log:         slog.Default(),
		lockPath:    defaultLockPath(),
		// ttlSeconds 0 = don't pass --ttl = models stay resident (no idle eviction)
	}
}

// EnsureLoaded loads a model via `lms load` if it is not already loaded
// with sufficient context. Evicts idle models LRU-first when the memory
// budget would be exceeded. The entire check-evict-load sequence is
// serialized via a filesystem lock.
//
// After loading, if WithWarmup was set, a 1-token throwaway request is
// sent to force runtime warmup (shader compilation, KV cache alloc).
//
// Returns nil if lms is not on PATH (graceful no-op).
func EnsureLoaded(ctx context.Context, model string, opts ...Option) error {
	c := defaults()
	for _, o := range opts {
		o(c)
	}

	log := c.log.With("component", "lmctl")

	lms, err := exec.LookPath("lms")
	if err != nil {
		return nil
	}

	unlock, err := acquireLock(ctx, c.lockPath)
	if err != nil {
		log.Warn("failed to acquire lmctl lock, proceeding unlocked", "err", err)
	} else {
		defer unlock()
	}

	loaded, err := listLoaded(ctx, lms)
	if err != nil {
		loaded = nil
	}

	base := BaseModelName(model)

	// Already loaded with sufficient context? Done.
	for _, m := range loaded {
		if matchesModel(m, base) && (c.contextLen == 0 || m.ContextLen >= c.contextLen) {
			log.Info("model already loaded", "model", model, "context", m.ContextLen)
			return nil
		}
	}

	// Unload if loaded with insufficient context.
	for _, m := range loaded {
		if matchesModel(m, base) {
			log.Info("unloading model for context upgrade",
				"model", m.Identifier, "had", m.ContextLen, "need", c.contextLen)
			_ = exec.CommandContext(ctx, lms, "unload", m.Identifier).Run()
		}
	}

	newSize := estimateModelSize(ctx, lms, model)

	// Evict idle models if loading would exceed budget.
	if c.maxMemBytes > 0 && newSize > 0 {
		loaded, _ = listLoaded(ctx, lms) // refresh after potential unload
		evictForBudget(ctx, log, lms, loaded, base, newSize, c.maxMemBytes)
	}

	// Build load command.
	args := []string{"load", model}
	if c.contextLen > 0 {
		args = append(args, "-c", fmt.Sprintf("%d", c.contextLen))
	}
	if c.ttlSeconds > 0 {
		args = append(args, "--ttl", fmt.Sprintf("%d", c.ttlSeconds))
	}
	args = append(args, "-y")

	log.Info("loading model",
		"model", model,
		"context_length", c.contextLen,
		"estimated_size_gb", newSize/(1024*1024*1024),
		"ttl_seconds", c.ttlSeconds,
	)

	loadCtx := ctx
	var loadCancel context.CancelFunc
	if c.loadTimeout > 0 {
		loadCtx, loadCancel = context.WithTimeout(ctx, c.loadTimeout)
		defer loadCancel()
	}

	loadStart := time.Now()
	cmd := exec.CommandContext(loadCtx, lms, args...)
	if output, loadErr := cmd.CombinedOutput(); loadErr != nil {
		return fmt.Errorf("lms load %s: %w\n%s", model, loadErr, output)
	}

	log.Info("model loaded",
		"model", model,
		"load_duration", time.Since(loadStart).Round(time.Millisecond),
	)

	// Warmup: fire a throwaway 1-token request to force shader/KV cache setup.
	if c.warmup && c.warmupURL != "" {
		warmupStart := time.Now()
		if warmupErr := warmupModel(ctx, c.warmupURL, c.warmupToken, model); warmupErr != nil {
			log.Warn("warmup request failed", "model", model, "err", warmupErr)
		} else {
			log.Info("model warmed up",
				"model", model,
				"warmup_duration", time.Since(warmupStart).Round(time.Millisecond),
			)
		}
	}

	return nil
}
