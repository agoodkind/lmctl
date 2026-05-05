package lmctl

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
)

// Build metadata stamped via ldflags by consumers.
var (
	// Commit is the git commit hash, set via ldflags.
	Commit = "unknown"
	// Version is the release version, set via ldflags.
	Version = "dev"
	// Dirty is "true" when the working tree was dirty at build time.
	Dirty = "false"
)

// BuildHash computes the SHA-256 of the running binary, truncated to
// 12 hex chars. Returns "unknown" if [os.Executable] or the read fail.
func BuildHash() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	f, err := os.Open(exe)
	if err != nil {
		return "unknown"
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	_, _ = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// BuildAttrs returns the standard slog attributes for build metadata.
// Consumers should attach these to their logger at init time.
func BuildAttrs() []slog.Attr {
	return []slog.Attr{
		slog.String("commit", Commit),
		slog.String("version", Version),
		slog.String("buildHash", BuildHash()),
		slog.String("dirty", Dirty),
	}
}
