# CLAUDE.md

## What this is

`lmctl` is a Go library for managing LM Studio model lifecycle: loading, unloading, and LRU eviction within a memory budget. It serializes operations across processes via a filesystem lock so concurrent tools don't race.

## Build

```bash
make test     # run tests
make lint     # golangci-lint
make check    # full: vet + lint + test + govulncheck
```

Never run `go build` or `go test` directly. Always use make.

## Package structure

Single package `lmctl` at the module root. No sub-packages. This is a library, not a binary.

- `lmctl.go` - Public API: `EnsureLoaded`, option funcs
- `lms.go` - `lms` CLI wrappers: `ListLoaded`, `BaseModelName`, model matching
- `evict.go` - LRU eviction within memory budget
- `lock.go` - Filesystem advisory lock for cross-process serialization

## Public API

```go
lmctl.EnsureLoaded(ctx, model,
    lmctl.WithContextLength(262144),
    lmctl.WithMaxMemoryGB(80),
)
```

## Consumers

- `goodkind.io/lm-review` - code review tool
- `github.com/fgrehm/clotilde` - Claude Code session wrapper

## Rules

- Use `slog` for logging. Never `fmt.Fprintf(os.Stderr)`.
- All model matching uses `BaseModelName` to strip publisher prefixes.
- The filesystem lock at `$XDG_CACHE_HOME/lmctl/load.lock` serializes load/evict across processes.
