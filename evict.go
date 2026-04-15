package lmctl

import (
	"context"
	"log/slog"
	"os/exec"
	"sort"
)

// evictForBudget unloads idle models (LRU first) until there's room for
// newSize within maxMem. Never evicts the model being loaded or any model
// with status "generating".
func evictForBudget(ctx context.Context, log *slog.Logger, lms string, loaded []LoadedModel, keepBase string, newSize, maxMem int64) {
	var totalLoaded int64
	for _, m := range loaded {
		totalLoaded += m.SizeBytes
	}

	needed := totalLoaded + newSize - maxMem
	if needed <= 0 {
		return
	}

	log.Info("memory budget exceeded, evicting idle models",
		"total_loaded_gb", totalLoaded/(1024*1024*1024),
		"new_size_gb", newSize/(1024*1024*1024),
		"budget_gb", maxMem/(1024*1024*1024),
		"need_to_free_gb", needed/(1024*1024*1024),
	)

	candidates := make([]LoadedModel, 0, len(loaded))
	for _, m := range loaded {
		if matchesModel(m, keepBase) {
			continue
		}
		if m.Status == "generating" {
			continue
		}
		candidates = append(candidates, m)
	}

	sort.Slice(candidates, func(i, j int) bool {
		ti, tj := int64(0), int64(0)
		if candidates[i].LastUsedTime != nil {
			ti = *candidates[i].LastUsedTime
		}
		if candidates[j].LastUsedTime != nil {
			tj = *candidates[j].LastUsedTime
		}
		return ti < tj // oldest first
	})

	for _, m := range candidates {
		if needed <= 0 {
			break
		}
		log.Info("evicting model", "model", m.Identifier, "size_gb", m.SizeBytes/(1024*1024*1024))
		_ = exec.CommandContext(ctx, lms, "unload", m.Identifier).Run()
		needed -= m.SizeBytes
	}
}
