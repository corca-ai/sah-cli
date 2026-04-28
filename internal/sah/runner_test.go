package sah

import (
	"context"
	"encoding/json"
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

func TestParseClaudeStructuredOutputUsesStructuredOutputFallback(t *testing.T) {
	raw := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"StructuredOutput","input":{"ok":true}}]}}
{"type":"result","is_error":false,"result":"","structured_output":{"ok":true},"usage":{"input_tokens":3,"output_tokens":8}}`

	output, err := parseClaudeStructuredOutput(raw)
	if err != nil {
		t.Fatalf("parseClaudeStructuredOutput returned error: %v", err)
	}
	if output.Text != `{"ok":true}` {
		t.Fatalf("unexpected text: %q", output.Text)
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
		&AssignmentAgentRequest{Prompt: `{"ok":true}`},
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
	if !strings.Contains(args, "--exclude-dynamic-system-prompt-sections") {
		t.Fatalf("expected claude args to exclude dynamic system prompt sections, got %q", args)
	}
	if !strings.Contains(args, "--setting-sources local") {
		t.Fatalf("expected claude args to ignore user/project settings, got %q", args)
	}
	env := strings.Join(command.Env, "\n")
	for _, entry := range []string{
		"CLAUDE_CODE_DISABLE_GIT_INSTRUCTIONS=1",
		"CLAUDE_CODE_DISABLE_CLAUDE_MDS=1",
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
		"CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1",
	} {
		if !strings.Contains(env, entry) {
			t.Fatalf("expected claude env to include %s, got %q", entry, env)
		}
	}
}

func TestBuildAgentCommandForGeminiDisablesExtensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gemini")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	command, _, err := buildAgentCommand(
		context.Background(),
		AgentSpec{Name: "gemini", Binary: "gemini"},
		"gemini-3-flash-base",
		t.TempDir(),
		&AssignmentAgentRequest{Prompt: `{"ok":true}`},
		map[string]string{"gemini": path},
	)
	if err != nil {
		t.Fatalf("buildAgentCommand returned error: %v", err)
	}

	args := strings.Join(command.Args[1:], " ")
	if !strings.Contains(args, "-e none") {
		t.Fatalf("expected gemini args to disable extensions, got %q", args)
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
		&AssignmentAgentRequest{Prompt: `{"ok":true}`},
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

func TestExecuteAgentReturnsParseErrorWhenFailedAgentPrintsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	script := "#!/bin/sh\nprintf 'not-json\\n'\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	_, rawOutput, err := executeAgent(
		context.Background(),
		AgentSpec{Name: "codex", Binary: "codex"},
		"",
		t.TempDir(),
		&AssignmentAgentRequest{Prompt: `{"ok":true}`},
		map[string]string{"codex": path},
	)
	if err == nil {
		t.Fatal("expected executeAgent to return an error")
	}
	if rawOutput != "not-json\n" {
		t.Fatalf("unexpected raw output: %q", rawOutput)
	}
	if !strings.Contains(err.Error(), "decode json line") {
		t.Fatalf("expected parse error in message, got %v", err)
	}
}

func TestResolveAssignmentAgentRequestPrefersServerOwnedRequest(t *testing.T) {
	request, err := ResolveAssignmentAgentRequest(Assignment{
		TaskType: "verification",
		AgentRequest: &AssignmentAgentRequest{
			Title:          "Verification assignment",
			Description:    "Return one JSON object.",
			Prompt:         "server-owned prompt",
			ResponseSchema: map[string]any{"type": "object"},
		},
		Instructions: AssignmentInstructions{
			Summary: "legacy summary",
		},
	})
	if err != nil {
		t.Fatalf("ResolveAssignmentAgentRequest returned error: %v", err)
	}
	if request.Prompt != "server-owned prompt" {
		t.Fatalf("expected server-owned prompt, got %q", request.Prompt)
	}
}

func TestResolveAssignmentAgentRequestFallsBackToLegacyInstructions(t *testing.T) {
	request, err := ResolveAssignmentAgentRequest(Assignment{
		AssignmentID:       41,
		TaskType:           "verification",
		TaskKey:            "verification",
		InstructionVersion: "2026-04-12.2",
		SchemaVersion:      "2026-04-11",
		Payload:            map[string]any{"title": "A"},
		Instructions: AssignmentInstructions{
			Summary:          "Approve only",
			Rules:            []string{"rule 1"},
			BadPatterns:      []string{"pattern 1"},
			StopConditions:   []string{"stop 1"},
			GoodExamples:     []any{map[string]any{"verdict": "approve"}},
			SubmissionSchema: map[string]any{"type": "object"},
		},
	})
	if err != nil {
		t.Fatalf("ResolveAssignmentAgentRequest returned error: %v", err)
	}
	if !strings.Contains(request.Prompt, "assignment_id: 41") {
		t.Fatalf("expected legacy prompt to include assignment metadata, got %q", request.Prompt)
	}
	if !strings.Contains(request.Prompt, "Submission schema:") {
		t.Fatalf("expected legacy prompt to include submission schema, got %q", request.Prompt)
	}
}

func TestBuildAgentCommandForCodexIncludesOutputSchemaFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	workdir := t.TempDir()

	command, useStdin, err := buildAgentCommand(
		context.Background(),
		AgentSpec{Name: "codex", Binary: "codex"},
		"",
		workdir,
		&AssignmentAgentRequest{
			Prompt:         `{"ok":true}`,
			ResponseSchema: map[string]any{"type": "object", "required": []string{"answer"}},
		},
		map[string]string{"codex": path},
	)
	if err != nil {
		t.Fatalf("buildAgentCommand returned error: %v", err)
	}
	if !useStdin {
		t.Fatal("expected codex prompt to be passed over stdin")
	}

	args := command.Args[1:]
	index := indexOf(args, "--output-schema")
	if index < 0 || index+1 >= len(args) {
		t.Fatalf("expected codex args to include --output-schema, got %q", strings.Join(args, " "))
	}
	schemaPath := args[index+1]
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema file: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("decode schema file: %v", err)
	}
	if schema["type"] != "object" {
		t.Fatalf("unexpected schema: %#v", schema)
	}
}

func TestBuildAgentCommandForClaudeIncludesJSONSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	command, _, err := buildAgentCommand(
		context.Background(),
		AgentSpec{Name: "claude", Binary: "claude"},
		"",
		t.TempDir(),
		&AssignmentAgentRequest{
			Prompt:         `{"ok":true}`,
			ResponseSchema: map[string]any{"type": "object", "required": []string{"answer"}},
		},
		map[string]string{"claude": path},
	)
	if err != nil {
		t.Fatalf("buildAgentCommand returned error: %v", err)
	}

	args := command.Args[1:]
	index := indexOf(args, "--json-schema")
	if index < 0 || index+1 >= len(args) {
		t.Fatalf("expected claude args to include --json-schema, got %q", strings.Join(args, " "))
	}
	if !strings.Contains(args[index+1], `"type":"object"`) {
		t.Fatalf("unexpected inline schema: %q", args[index+1])
	}
}

func indexOf(values []string, target string) int {
	for index, value := range values {
		if value == target {
			return index
		}
	}
	return -1
}
