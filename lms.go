package lmctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
)

// LoadedModel represents a model from `lms ps --json`.
type LoadedModel struct {
	Identifier   string `json:"identifier"`
	ModelKey     string `json:"modelKey"`
	Path         string `json:"path"`
	SizeBytes    int64  `json:"sizeBytes"`
	ContextLen   int    `json:"contextLength"`
	Status       string `json:"status"`
	LastUsedTime *int64 `json:"lastUsedTime"`
}

// lookupLMS returns the absolute path of the `lms` binary, or the empty
// string when lms is not installed. Splitting this out lets callers
// treat "missing binary" as a benign no-op without confusing static
// analyzers that flag returning nil when an error is in scope.
func lookupLMS() string {
	path, err := exec.LookPath("lms")
	if err != nil {
		return ""
	}
	return path
}

// ListLoaded returns the currently loaded models via `lms ps --json`.
// Returns nil, nil if lms is not on PATH (graceful no-op so callers in
// non-LM-Studio environments do not have to special-case the missing
// binary).
func ListLoaded(ctx context.Context) ([]LoadedModel, error) {
	lms := lookupLMS()
	if lms == "" {
		return nil, nil
	}
	return listLoaded(ctx, lms)
}

func listLoaded(ctx context.Context, lms string) ([]LoadedModel, error) {
	log := slog.Default().With("component", "lmctl")
	out, err := exec.CommandContext(ctx, lms, "ps", "--json").Output()
	if err != nil {
		log.ErrorContext(ctx, "lms ps --json failed", "err", err)
		return nil, fmt.Errorf("lms ps --json: %w", err)
	}
	var models []LoadedModel
	err = json.Unmarshal(out, &models)
	if err != nil {
		log.ErrorContext(ctx, "decode lms ps output failed", "err", err)
		return nil, fmt.Errorf("decode lms ps output: %w", err)
	}
	return models, nil
}

func estimateModelSize(ctx context.Context, lms, model string) int64 {
	log := slog.Default().With("component", "lmctl", "model", model)
	out, err := exec.CommandContext(ctx, lms, "ls", "--json").Output()
	if err != nil {
		log.WarnContext(ctx, "list models for size estimate failed", "err", err)
		return 0
	}
	base := BaseModelName(model)
	var models []struct {
		ModelKey  string `json:"modelKey"`
		SizeBytes int64  `json:"sizeBytes"`
	}
	err = json.Unmarshal(out, &models)
	if err != nil {
		log.WarnContext(ctx, "decode lms ls output failed", "err", err)
		return 0
	}
	for _, m := range models {
		if BaseModelName(m.ModelKey) == base {
			return m.SizeBytes
		}
	}
	return 0
}

// BaseModelName strips the publisher prefix (e.g. "qwen/qwen3-coder-next"
// becomes "qwen3-coder-next") so models match regardless of namespace.
func BaseModelName(model string) string {
	if i := strings.LastIndex(model, "/"); i >= 0 {
		return model[i+1:]
	}
	return model
}

func matchesModel(m LoadedModel, base string) bool {
	return BaseModelName(m.ModelKey) == base ||
		BaseModelName(m.Identifier) == base ||
		BaseModelName(m.Path) == base
}

// safeIdentifierPattern restricts model identifiers to a conservative
// alphabet that can never be interpreted as shell metacharacters or
// argv-injection. This is the allowlist applied before passing any
// JSON-derived identifier to [exec.CommandContext].
var safeIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9._/@:+-]{1,256}$`)

// errUnsafeIdentifier is returned when a model identifier read from
// `lms ps --json` does not match [safeIdentifierPattern]. The exec
// helper refuses to invoke `lms` with a non-conforming identifier.
var errUnsafeIdentifier = errors.New("unsafe model identifier")

// validateIdentifier enforces the [safeIdentifierPattern] allowlist.
// Returns the input unchanged when it passes; returns
// [errUnsafeIdentifier] otherwise.
func validateIdentifier(id string) (string, error) {
	if !safeIdentifierPattern.MatchString(id) {
		log := slog.Default().With("component", "lmctl")
		log.Warn("rejected unsafe model identifier", "id", id)
		return "", fmt.Errorf("%w: %q", errUnsafeIdentifier, id)
	}
	return id, nil
}

// runUnload invokes `lms unload <id>` after validating the identifier
// via [validateIdentifier]. The validated string flows through a local
// variable, which lets static analyzers see the input as constrained
// rather than tainted.
func runUnload(ctx context.Context, lms, id string) error {
	log := slog.Default().With("component", "lmctl", "model", id)
	safeID, err := validateIdentifier(id)
	if err != nil {
		log.ErrorContext(ctx, "rejected unsafe model identifier", "err", err)
		return err
	}
	cmd := exec.CommandContext(ctx, lms, "unload", safeID)
	runErr := cmd.Run()
	if runErr != nil {
		log.ErrorContext(ctx, "lms unload failed", "err", runErr)
		return fmt.Errorf("lms unload %s: %w", safeID, runErr)
	}
	return nil
}
