package sah

import (
	"context"
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
	if !output.Usage.Available || output.Usage.InputTokens != 100 || output.Usage.CachedTokens != 20 || output.Usage.TotalTokens != 105 {
		t.Fatalf("unexpected usage: %#v", output.Usage)
	}
}

func TestParseGeminiStructuredOutput(t *testing.T) {
	raw := `{"type":"message","role":"assistant","content":"{\"ok\":true}","delta":true}
{"type":"result","status":"success","stats":{"total_tokens":73516,"input_tokens":9916,"output_tokens":690,"cached":5}}`

	output, err := parseGeminiStructuredOutput(raw)
	if err != nil {
		t.Fatalf("parseGeminiStructuredOutput returned error: %v", err)
	}
	if output.Text != `{"ok":true}` {
		t.Fatalf("unexpected text: %q", output.Text)
	}
	if output.Usage.TotalTokens != 73516 || output.Usage.InputTokens != 9916 || output.Usage.OutputTokens != 690 || output.Usage.CachedTokens != 5 || output.Usage.InternalTokens != 62910 {
		t.Fatalf("unexpected usage: %#v", output.Usage)
	}
}

func TestParseClaudeStructuredOutput(t *testing.T) {
	raw := `{"type":"assistant","message":{"content":[{"type":"text","text":"{\"ok\":true}"}]}}
{"type":"result","is_error":false,"usage":{"input_tokens":3,"cache_creation_input_tokens":4824,"cache_read_input_tokens":12074,"output_tokens":8}}`

	output, err := parseClaudeStructuredOutput(raw)
	if err != nil {
		t.Fatalf("parseClaudeStructuredOutput returned error: %v", err)
	}
	if output.Text != `{"ok":true}` {
		t.Fatalf("unexpected text: %q", output.Text)
	}
	if output.Usage.InputTokens != 3 || output.Usage.OutputTokens != 8 || output.Usage.CachedTokens != 12074 || output.Usage.CacheWriteTokens != 4824 || output.Usage.TotalTokens != 16909 {
		t.Fatalf("unexpected usage: %#v", output.Usage)
	}
}

func TestParseQwenStructuredOutput(t *testing.T) {
	raw := `{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"internal"},{"type":"text","text":"{\"ok\":true}"}]}}
{"type":"result","subtype":"success","is_error":false,"usage":{"input_tokens":21,"cache_read_input_tokens":4,"output_tokens":6,"total_tokens":27}}`

	output, err := parseQwenStructuredOutput(raw)
	if err != nil {
		t.Fatalf("parseQwenStructuredOutput returned error: %v", err)
	}
	if output.Text != `{"ok":true}` {
		t.Fatalf("unexpected text: %q", output.Text)
	}
	if output.Usage.InputTokens != 21 || output.Usage.OutputTokens != 6 || output.Usage.CachedTokens != 4 || output.Usage.TotalTokens != 27 || output.Usage.InternalTokens != 0 {
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

func TestBuildAgentCommandForClaudeAvoidsBareAuthMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	command, _, err := buildAgentCommand(
		context.Background(),
		AgentSpec{Name: "claude", Binary: "claude"},
		"sonnet",
		t.TempDir(),
		`{"ok":true}`,
		map[string]string{"claude": path},
	)
	if err != nil {
		t.Fatalf("buildAgentCommand returned error: %v", err)
	}

	args := strings.Join(command.Args[1:], " ")
	if strings.Contains(args, "--bare") {
		t.Fatalf("expected claude args to omit --bare, got %q", args)
	}
	if !strings.Contains(args, "--strict-mcp-config") {
		t.Fatalf("expected claude args to include --strict-mcp-config, got %q", args)
	}
	if !strings.Contains(args, "--disable-slash-commands") {
		t.Fatalf("expected claude args to include --disable-slash-commands, got %q", args)
	}
}

func TestBuildAgentCommandForQwenUsesHeadlessPlanMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qwen")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	command, useStdin, err := buildAgentCommand(
		context.Background(),
		AgentSpec{Name: "qwen", Binary: "qwen"},
		"",
		t.TempDir(),
		`{"ok":true}`,
		map[string]string{"qwen": path},
	)
	if err != nil {
		t.Fatalf("buildAgentCommand returned error: %v", err)
	}
	if useStdin {
		t.Fatal("expected qwen prompt to be passed as an argument")
	}

	args := strings.Join(command.Args[1:], " ")
	if !strings.Contains(args, "--output-format stream-json") {
		t.Fatalf("expected qwen args to include stream-json output, got %q", args)
	}
	if !strings.Contains(args, "--approval-mode plan") {
		t.Fatalf("expected qwen args to include plan mode, got %q", args)
	}
	if !strings.Contains(args, "--sandbox") {
		t.Fatalf("expected qwen args to enable sandboxing, got %q", args)
	}
}
