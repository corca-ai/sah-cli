package sah

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCodexStructuredOutput(t *testing.T) {
	raw := `{"type":"thread.started","thread_id":"abc"}
{"type":"item.completed","item":{"id":"1","type":"agent_message","text":"{\"ok\":true}"}}
{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":5}}`

	output, err := parseCodexStructuredOutput(raw)
	if err != nil {
		t.Fatalf("parseCodexStructuredOutput returned error: %v", err)
	}
	if output.Text != `{"ok":true}` {
		t.Fatalf("unexpected text: %q", output.Text)
	}
	if !output.Usage.Available || output.Usage.InputTokens != 100 || output.Usage.CachedTokens != 20 {
		t.Fatalf("unexpected usage: %#v", output.Usage)
	}
}

func TestParseGeminiStructuredOutput(t *testing.T) {
	raw := `{"type":"message","role":"assistant","content":"{\"ok\":true}","delta":true}
{"type":"result","status":"success","stats":{"total_tokens":50,"input_tokens":40,"output_tokens":10,"cached":5}}`

	output, err := parseGeminiStructuredOutput(raw)
	if err != nil {
		t.Fatalf("parseGeminiStructuredOutput returned error: %v", err)
	}
	if output.Text != `{"ok":true}` {
		t.Fatalf("unexpected text: %q", output.Text)
	}
	if output.Usage.TotalTokens != 50 || output.Usage.InputTokens != 40 || output.Usage.CachedTokens != 5 {
		t.Fatalf("unexpected usage: %#v", output.Usage)
	}
}

func TestParseClaudeStructuredOutput(t *testing.T) {
	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"{\"ok\":true}"}]}}
{"type":"result","is_error":false,"usage":{"input_tokens":12,"cache_read_input_tokens":3,"output_tokens":4}}`

	output, err := parseClaudeStructuredOutput(raw)
	if err != nil {
		t.Fatalf("parseClaudeStructuredOutput returned error: %v", err)
	}
	if output.Text != `{"ok":true}` {
		t.Fatalf("unexpected text: %q", output.Text)
	}
	if output.Usage.InputTokens != 12 || output.Usage.OutputTokens != 4 || output.Usage.CachedTokens != 3 {
		t.Fatalf("unexpected usage: %#v", output.Usage)
	}
}

func TestResolveAgentBinaryPathUsesConfiguredPath(t *testing.T) {
	t.Setenv("PATH", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	resolved, err := resolveAgentBinaryPath(SupportedAgents[0], map[string]string{"codex": path})
	if err != nil {
		t.Fatalf("resolveAgentBinaryPath returned error: %v", err)
	}
	if resolved != path {
		t.Fatalf("expected %q, got %q", path, resolved)
	}
}

func TestResolveAgentBinaryPathDoesNotFallbackFromConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	fallback := filepath.Join(dir, "codex")
	if err := os.WriteFile(fallback, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fallback executable: %v", err)
	}

	_, err := resolveAgentBinaryPath(SupportedAgents[0], map[string]string{"codex": filepath.Join(dir, "missing-codex")})
	if err == nil {
		t.Fatal("expected error for missing configured path")
	}
	if !strings.Contains(err.Error(), "re-run `sah daemon install`") {
		t.Fatalf("expected daemon reinstall guidance, got %v", err)
	}
}
