package lmctl

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"os"
)

// Set via ldflags at build time by consumers.
var (
	Commit  = "unknown"
	Version = "dev"
	Dirty   = "false"
)

// BuildHash computes the SHA-256 of the running binary, truncated to 12 hex chars.
func BuildHash() string {
	exe, err := os.Executable()
	if err != nil {
		return "unknown"
	}
	f, err := os.Open(exe)
	if err != nil {
		return "unknown"
	}
	defer f.Close()
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
