package lmctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// warmupModel sends a throwaway 1-token completion to force the LM Studio
// runtime to compile Metal shaders, allocate the KV cache, and set up the
// compute graph. After this call returns, the next real request is fast.
func warmupModel(ctx context.Context, apiURL, token, model string) error {
	url := strings.TrimRight(apiURL, "/") + "/v1/chat/completions"

	body, err := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
		"max_tokens": 1,
	})
	if err != nil {
		return fmt.Errorf("marshal warmup request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create warmup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("warmup request: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("warmup returned %d", resp.StatusCode)
	}
	return nil
}
