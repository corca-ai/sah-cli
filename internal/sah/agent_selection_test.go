package sah

import "testing"

func TestParseAgentList(t *testing.T) {
	list := ParseAgentList("codex, gemini ,claude,codex")
	if len(list) != 3 || list[0] != "codex" || list[1] != "gemini" || list[2] != "claude" {
		t.Fatalf("unexpected list: %#v", list)
	}
}

func TestParseAgentModels(t *testing.T) {
	models, err := ParseAgentModels("codex=gpt-5.4-mini, gemini=gemini-3-flash-base, claude=sonnet")
	if err != nil {
		t.Fatalf("ParseAgentModels returned error: %v", err)
	}
	if models["codex"] != "gpt-5.4-mini" || models["gemini"] != "gemini-3-flash-base" || models["claude"] != "sonnet" {
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
