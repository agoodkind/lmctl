package lmctl

import (
	"context"
	"encoding/json"
	"os/exec"
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

// ListLoaded returns the currently loaded models via `lms ps --json`.
// Returns nil, nil if lms is not on PATH.
func ListLoaded(ctx context.Context) ([]LoadedModel, error) {
	lms, err := exec.LookPath("lms")
	if err != nil {
		return nil, nil
	}
	return listLoaded(ctx, lms)
}

func listLoaded(ctx context.Context, lms string) ([]LoadedModel, error) {
	out, err := exec.CommandContext(ctx, lms, "ps", "--json").Output()
	if err != nil {
		return nil, err
	}
	var models []LoadedModel
	if err := json.Unmarshal(out, &models); err != nil {
		return nil, err
	}
	return models, nil
}

func estimateModelSize(ctx context.Context, lms string, model string) int64 {
	out, err := exec.CommandContext(ctx, lms, "ls", "--json").Output()
	if err != nil {
		return 0
	}
	base := BaseModelName(model)
	var models []struct {
		ModelKey  string `json:"modelKey"`
		SizeBytes int64  `json:"sizeBytes"`
	}
	if err := json.Unmarshal(out, &models); err != nil {
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
