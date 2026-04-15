package lmctl

import (
	"testing"
)

func TestBaseModelName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"qwen/qwen3-coder-next", "qwen3-coder-next"},
		{"openai/gpt-oss-120b", "gpt-oss-120b"},
		{"qwen3.5-122b-a10b-text-qx85-mlx", "qwen3.5-122b-a10b-text-qx85-mlx"},
		{"a/b/c", "c"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := BaseModelName(tt.input); got != tt.want {
			t.Errorf("BaseModelName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMatchesModel(t *testing.T) {
	m := LoadedModel{
		Identifier: "qwen3-coder-next",
		ModelKey:   "qwen/qwen3-coder-next",
		Path:       "/models/qwen/qwen3-coder-next",
	}

	if !matchesModel(m, "qwen3-coder-next") {
		t.Error("expected match on base name")
	}
	if matchesModel(m, "gpt-oss-120b") {
		t.Error("expected no match on different model")
	}
}

func TestDefaults(t *testing.T) {
	c := defaults()
	if c.loadTimeout != 120_000_000_000 { // 120s in nanoseconds
		t.Errorf("default loadTimeout = %v, want 120s", c.loadTimeout)
	}
	if c.contextLen != 0 {
		t.Errorf("default contextLen = %d, want 0", c.contextLen)
	}
	if c.maxMemBytes != 0 {
		t.Errorf("default maxMemBytes = %d, want 0", c.maxMemBytes)
	}
	if c.ttlSeconds != 0 {
		t.Errorf("default ttlSeconds = %d, want 0 (no TTL)", c.ttlSeconds)
	}
}

func TestWithOptions(t *testing.T) {
	c := defaults()
	WithContextLength(262144)(c)
	WithMaxMemoryGB(80)(c)

	if c.contextLen != 262144 {
		t.Errorf("contextLen = %d, want 262144", c.contextLen)
	}
	wantBytes := int64(80) * 1024 * 1024 * 1024
	if c.maxMemBytes != wantBytes {
		t.Errorf("maxMemBytes = %d, want %d", c.maxMemBytes, wantBytes)
	}
}

func TestWithMaxMemoryBytes(t *testing.T) {
	c := defaults()
	WithMaxMemoryBytes(42)(c)
	if c.maxMemBytes != 42 {
		t.Errorf("maxMemBytes = %d, want 42", c.maxMemBytes)
	}
}

func TestWithWarmup(t *testing.T) {
	c := defaults()
	WithWarmup("http://localhost:1234", "sk-test")(c)
	if !c.warmup {
		t.Error("warmup should be true")
	}
	if c.warmupURL != "http://localhost:1234" {
		t.Errorf("warmupURL = %q, want http://localhost:1234", c.warmupURL)
	}
	if c.warmupToken != "sk-test" {
		t.Errorf("warmupToken = %q, want sk-test", c.warmupToken)
	}
}

func TestWithTTL(t *testing.T) {
	c := defaults()
	WithTTL(3600)(c)
	if c.ttlSeconds != 3600 {
		t.Errorf("ttlSeconds = %d, want 3600", c.ttlSeconds)
	}
}

func TestBuildAttrs(t *testing.T) {
	attrs := BuildAttrs()
	if len(attrs) != 4 {
		t.Errorf("BuildAttrs() returned %d attrs, want 4", len(attrs))
	}
	keys := map[string]bool{}
	for _, a := range attrs {
		keys[a.Key] = true
	}
	for _, k := range []string{"commit", "version", "buildHash", "dirty"} {
		if !keys[k] {
			t.Errorf("BuildAttrs() missing key %q", k)
		}
	}
}
