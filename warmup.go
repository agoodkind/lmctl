package lmctl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// chatMessage is one message in a chat-completion request.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// warmupRequest is the request body sent to /v1/chat/completions to
// force model warmup on the LM Studio server.
type warmupRequest struct {
	Model     string        `json:"model"`
	Messages  []chatMessage `json:"messages"`
	MaxTokens int           `json:"max_tokens"`
}

// warmupModel sends a throwaway 1-token completion to force the LM Studio
// runtime to compile Metal shaders, allocate the KV cache, and set up the
// compute graph. After this call returns, the next real request is fast.
func warmupModel(ctx context.Context, apiURL, token, model string) error {
	log := slog.Default().With("component", "lmctl", "model", model)
	url := strings.TrimRight(apiURL, "/") + "/v1/chat/completions"

	body, err := json.Marshal(warmupRequest{
		Model:     model,
		Messages:  []chatMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 1,
	})
	if err != nil {
		log.ErrorContext(ctx, "failed to marshal warmup request", "err", err)
		return fmt.Errorf("marshal warmup request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.ErrorContext(ctx, "failed to create warmup request", "err", err, "url", url)
		return fmt.Errorf("create warmup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	log.InfoContext(ctx, "sending warmup request", "url", url)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.ErrorContext(ctx, "warmup request transport error", "err", err)
		return fmt.Errorf("warmup request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		statusErr := fmt.Errorf("warmup returned %d", resp.StatusCode)
		log.ErrorContext(ctx, "warmup request returned non-2xx",
			"status", resp.StatusCode, "err", statusErr)
		return statusErr
	}
	return nil
}
