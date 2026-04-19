package sah

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAgentList(t *testing.T) {
	list := ParseAgentList("codex, gemini ,claude,qwen,codex")
	if len(list) != 4 || list[0] != "codex" || list[1] != "gemini" || list[2] != "claude" || list[3] != "qwen" {
		t.Fatalf("unexpected list: %#v", list)
	}
}

func TestParseAgentModels(t *testing.T) {
	models, err := ParseAgentModels("codex=gpt-5.4-mini, gemini=gemini-3-flash-base, claude=sonnet, qwen=coder-model")
	if err != nil {
		t.Fatalf("ParseAgentModels returned error: %v", err)
	}
	if models["codex"] != "gpt-5.4-mini" || models["gemini"] != "gemini-3-flash-base" || models["claude"] != "sonnet" || models["qwen"] != "coder-model" {
		t.Fatalf("unexpected models: %#v", models)
	}
}

func TestModelForAgentPrefersOverride(t *testing.T) {
	model := ModelForAgent("codex", "default-model", map[string]string{"codex": "gpt-5.4-mini"})
	if model != "gpt-5.4-mini" {
		t.Fatalf("unexpected model: %q", model)
	}
}

func TestModelForAgentFallsBackToGlobal(t *testing.T) {
	model := ModelForAgent("gemini", "some-global", nil)
	if model != "some-global" {
		t.Fatalf("expected global fallback, got %q", model)
	}
}

func TestModelForAgentUsesBuiltinDefaults(t *testing.T) {
	cases := map[string]string{
		"codex":  "gpt-5.4-mini",
		"gemini": "gemini-3-flash-base",
		"claude": "sonnet",
	}
	for agent, want := range cases {
		if got := ModelForAgent(agent, "", nil); got != want {
			t.Fatalf("%s: expected %q, got %q", agent, want, got)
		}
	}
}

func TestModelForAgentLeavesQwenUnsetWithoutOverrides(t *testing.T) {
	model := ModelForAgent("qwen", "", nil)
	if model != "" {
		t.Fatalf("expected qwen to use upstream default model, got %q", model)
	}
}

func TestResolveAgentPoolRotateInstalledReturnsFriendlyErrorWhenNothingDetected(t *testing.T) {
	t.Setenv("PATH", "")

	_, err := ResolveAgentPool(DefaultConfig(), WorkerOptions{RotateInstalled: true})
	if err == nil {
		t.Fatal("expected missing-agent error")
	}
	if !IsNoSupportedAgentCLI(err) {
		t.Fatalf("expected IsNoSupportedAgentCLI to match, got %v", err)
	}
	if !strings.Contains(err.Error(), "sah agents") {
		t.Fatalf("expected detection guidance in error, got %v", err)
	}
}

func TestResolveAgentPoolPrefersExplicitAgentOverConfiguredPool(t *testing.T) {
	binaryPaths := testSelectionBinaryPaths(t, "codex", "claude", "gemini")
	config := DefaultConfig()
	config.AgentPool = []string{"claude", "gemini"}

	pool, err := ResolveAgentPool(config, WorkerOptions{
		Agent:       "codex",
		BinaryPaths: binaryPaths,
	})
	if err != nil {
		t.Fatalf("ResolveAgentPool returned error: %v", err)
	}
	if got := joinSelectionAgentNames(pool); got != "codex" {
		t.Fatalf("expected explicit agent to override configured pool, got %q", got)
	}
}

func TestResolveAgentPoolPrefersExplicitAgentOverConfiguredRotateInstalled(t *testing.T) {
	binaryPaths := testSelectionBinaryPaths(t, "codex", "claude", "gemini")
	config := DefaultConfig()
	config.RotateInstalled = true

	pool, err := ResolveAgentPool(config, WorkerOptions{
		Agent:       "codex",
		BinaryPaths: binaryPaths,
	})
	if err != nil {
		t.Fatalf("ResolveAgentPool returned error: %v", err)
	}
	if got := joinSelectionAgentNames(pool); got != "codex" {
		t.Fatalf("expected explicit agent to override rotate-installed config, got %q", got)
	}
}

func testSelectionBinaryPaths(t *testing.T, agents ...string) map[string]string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("PATH", dir)

	binaryPaths := make(map[string]string, len(agents))
	for _, agent := range agents {
		binaryPaths[agent] = writeSelectionExecutable(t, dir, agent)
	}
	return binaryPaths
}

func writeSelectionExecutable(t *testing.T, dir string, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", name, err)
	}
	return path
}

func joinSelectionAgentNames(pool []AgentSpec) string {
	names := make([]string, 0, len(pool))
	for _, agent := range pool {
		names = append(names, agent.Name)
	}
	return strings.Join(names, ", ")
}
