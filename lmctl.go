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
	"strconv"
	"time"
)

// Option configures [EnsureLoaded] behavior.
type Option func(*cfg)

// WithContextLength sets the minimum context length (tokens) the model
// must be loaded with. Pass 0 to skip the context check.
func WithContextLength(n int) Option { return func(c *cfg) { c.contextLen = n } }

// WithMaxMemoryBytes sets the memory budget in bytes. Models are evicted
// LRU-first when loading would exceed this budget. 0 disables eviction.
func WithMaxMemoryBytes(b int64) Option { return func(c *cfg) { c.maxMemBytes = b } }

// WithMaxMemoryGB is a convenience wrapper around [WithMaxMemoryBytes].
func WithMaxMemoryGB(gb int) Option {
	return func(c *cfg) { c.maxMemBytes = int64(gb) * 1024 * 1024 * 1024 }
}

// WithLoadTimeout sets the maximum time to wait for `lms load` to
// complete. Default is 120s. Pass 0 to disable the timeout.
func WithLoadTimeout(d time.Duration) Option { return func(c *cfg) { c.loadTimeout = d } }

// WithLogger sets the logger for model lifecycle events.
// Defaults to [slog.Default].
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
		contextLen:  0,
		maxMemBytes: 0,
		loadTimeout: 120 * time.Second,
		log:         slog.Default(),
		lockPath:    defaultLockPath(),
		ttlSeconds:  0,
		warmup:      false,
		warmupURL:   "",
		warmupToken: "",
	}
}

// EnsureLoaded loads a model via `lms load` if it is not already loaded
// with sufficient context. Evicts idle models LRU-first when the memory
// budget would be exceeded. The entire check-evict-load sequence is
// serialized via a filesystem lock.
//
// After loading, if [WithWarmup] was set, a 1-token throwaway request is
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
		log.InfoContext(ctx, "lms not on PATH, skipping ensure-loaded", "model", model)
		return nil
	}

	unlock := acquireLockOrLog(ctx, log, c.lockPath)
	defer unlock()

	loaded, err := listLoaded(ctx, lms)
	if err != nil {
		log.WarnContext(ctx, "failed to list loaded models, continuing", "err", err)
		loaded = nil
	}

	base := BaseModelName(model)

	if hasSufficientContext(ctx, log, loaded, base, model, c.contextLen) {
		return nil
	}

	unloadInsufficientContext(ctx, log, lms, loaded, base, c.contextLen)

	newSize := estimateModelSize(ctx, lms, model)

	// Evict idle models if loading would exceed budget.
	if c.maxMemBytes > 0 && newSize > 0 {
		// refresh after potential unload
		refreshed, refreshErr := listLoaded(ctx, lms)
		if refreshErr != nil {
			log.WarnContext(ctx, "failed to refresh loaded models for eviction",
				"err", refreshErr)
			refreshed = loaded
		}
		evictForBudget(ctx, log, lms, refreshed, base, newSize, c.maxMemBytes)
	}

	if loadErr := runLoad(ctx, log, lms, model, c, newSize); loadErr != nil {
		return loadErr
	}

	if c.warmup && c.warmupURL != "" {
		runWarmup(ctx, log, c, model)
	}

	return nil
}

// acquireLockOrLog wraps [acquireLock] and falls back to a no-op unlock
// if locking fails. The fallback path is logged at warn level.
func acquireLockOrLog(ctx context.Context, log *slog.Logger, path string) func() {
	unlock, err := acquireLock(ctx, path)
	if err != nil {
		log.WarnContext(ctx, "failed to acquire lmctl lock, proceeding unlocked",
			"err", err)
		return func() {}
	}
	return unlock
}

// hasSufficientContext returns true if [base] is already loaded with at
// least [wantCtx] tokens of context.
func hasSufficientContext(
	ctx context.Context, log *slog.Logger,
	loaded []LoadedModel, base, model string, wantCtx int,
) bool {
	matched, foundCtx := findLoadedContext(loaded, base, wantCtx)
	if !matched {
		return false
	}
	log.InfoContext(ctx, "model already loaded",
		"model", model, "context", foundCtx)
	return true
}

// findLoadedContext scans [loaded] for a model matching [base] that
// satisfies [wantCtx]. Returns (true, ctxLen) on the first hit and
// (false, 0) when no entry qualifies. Splitting the search out of the
// caller keeps logging out of the loop body.
func findLoadedContext(loaded []LoadedModel, base string, wantCtx int) (bool, int) {
	for _, m := range loaded {
		if !matchesModel(m, base) {
			continue
		}
		if wantCtx == 0 || m.ContextLen >= wantCtx {
			return true, m.ContextLen
		}
	}
	return false, 0
}

// unloadInsufficientContext unloads any matching model whose context
// length is below the requested size, so the load step can re-load with
// a larger context window. Emits a single summary log line for the batch.
func unloadInsufficientContext(
	ctx context.Context, log *slog.Logger,
	lms string, loaded []LoadedModel, base string, wantCtx int,
) {
	var (
		ids      []string
		failures int
	)
	for _, m := range loaded {
		if !matchesModel(m, base) {
			continue
		}
		err := runUnload(ctx, lms, m.Identifier)
		if err != nil {
			failures++
			continue
		}
		ids = append(ids, m.Identifier)
	}
	if len(ids) > 0 || failures > 0 {
		log.InfoContext(ctx, "unloaded models for context upgrade",
			"models", ids, "need", wantCtx, "failures", failures)
	}
}

// runLoad assembles the `lms load` argv and executes it under any
// configured load-timeout context. It logs the boundary events and
// wraps any error from the external process.
func runLoad(
	ctx context.Context, log *slog.Logger,
	lms, model string, c *cfg, newSize int64,
) error {
	args := []string{"load", model}
	if c.contextLen > 0 {
		args = append(args, "-c", strconv.Itoa(c.contextLen))
	}
	if c.ttlSeconds > 0 {
		args = append(args, "--ttl", strconv.Itoa(c.ttlSeconds))
	}
	args = append(args, "-y")

	log.InfoContext(ctx, "loading model",
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

	loadStart := now()
	cmd := exec.CommandContext(loadCtx, lms, args...)
	output, loadErr := cmd.CombinedOutput()
	if loadErr != nil {
		log.ErrorContext(ctx, "lms load failed",
			"model", model, "err", loadErr, "output", string(output))
		return fmt.Errorf("lms load %s: %w\n%s", model, loadErr, output)
	}

	log.InfoContext(ctx, "model loaded",
		"model", model,
		"load_duration", since(loadStart).Round(time.Millisecond),
	)
	return nil
}

// runWarmup fires the post-load warmup request and logs the result. A
// failed warmup is non-fatal and only produces a warning.
func runWarmup(ctx context.Context, log *slog.Logger, c *cfg, model string) {
	warmupStart := now()
	warmupErr := warmupModel(ctx, c.warmupURL, c.warmupToken, model)
	if warmupErr != nil {
		log.WarnContext(ctx, "warmup request failed",
			"model", model, "err", warmupErr)
		return
	}
	log.InfoContext(ctx, "model warmed up",
		"model", model,
		"warmup_duration", since(warmupStart).Round(time.Millisecond),
	)
}
