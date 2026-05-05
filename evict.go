package lmctl

import (
	"context"
	"log/slog"
	"sort"
)

// evictForBudget unloads idle models (LRU first) until there's room for
// newSize within maxMem. Never evicts the model being loaded or any model
// with status "generating".
func evictForBudget(
	ctx context.Context, log *slog.Logger,
	lms string, loaded []LoadedModel,
	keepBase string, newSize, maxMem int64,
) {
	var totalLoaded int64
	for _, m := range loaded {
		totalLoaded += m.SizeBytes
	}

	needed := totalLoaded + newSize - maxMem
	if needed <= 0 {
		return
	}

	log.InfoContext(ctx, "memory budget exceeded, evicting idle models",
		"total_loaded_gb", totalLoaded/(1024*1024*1024),
		"new_size_gb", newSize/(1024*1024*1024),
		"budget_gb", maxMem/(1024*1024*1024),
		"need_to_free_gb", needed/(1024*1024*1024),
	)

	candidates := pickEvictionCandidates(loaded, keepBase)
	evicted, freed, failures := unloadCandidates(ctx, lms, candidates, needed)

	log.InfoContext(ctx, "eviction summary",
		"evicted_models", evicted,
		"freed_gb", freed/(1024*1024*1024),
		"remaining_to_free_gb", (needed-freed)/(1024*1024*1024),
		"unload_failures", failures,
	)
}

// pickEvictionCandidates returns loaded models eligible for eviction
// (not the one being loaded, not currently generating), sorted oldest
// last-used first.
func pickEvictionCandidates(loaded []LoadedModel, keepBase string) []LoadedModel {
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
		return lastUsed(candidates[i]) < lastUsed(candidates[j])
	})
	return candidates
}

// lastUsed returns the model's last-used timestamp in epoch units, or 0
// if the field is unset (treat as oldest).
func lastUsed(m LoadedModel) int64 {
	if m.LastUsedTime == nil {
		return 0
	}
	return *m.LastUsedTime
}

// unloadCandidates iterates the candidates oldest-first and unloads
// each via `lms unload`, stopping once at least [needed] bytes have
// been freed. Returns the identifiers of the models actually unloaded,
// the number of bytes freed, and the count of unload failures.
func unloadCandidates(
	ctx context.Context, lms string,
	candidates []LoadedModel, needed int64,
) ([]string, int64, int) {
	var (
		evicted  []string
		freed    int64
		failures int
	)
	for _, m := range candidates {
		if freed >= needed {
			break
		}
		err := runUnload(ctx, lms, m.Identifier)
		if err != nil {
			failures++
			continue
		}
		evicted = append(evicted, m.Identifier)
		freed += m.SizeBytes
	}
	return evicted, freed, failures
}
