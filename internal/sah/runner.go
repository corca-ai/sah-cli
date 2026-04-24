package sah

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type AgentSpec struct {
	Name        string
	Binary      string
	Description string
}

type AgentStatus struct {
	AgentSpec
	Path      string
	Installed bool
}

type AgentRunOptions struct {
	Agent       string
	Model       string
	Models      map[string]string
	BinaryPaths map[string]string
	Timeout     time.Duration
}

type AgentResult struct {
	Agent     AgentSpec
	Model     string
	RawOutput string
	Payload   map[string]any
	Usage     TokenUsage
	Duration  time.Duration
}

type structuredAgentOutput struct {
	Text       string
	Usage      TokenUsage
	AgentError string
}

var errNoSupportedAgentCLI = errors.New("no supported agent CLI found")

var SupportedAgents = []AgentSpec{
	{Name: "codex", Binary: "codex", Description: "OpenAI Codex CLI"},
	{Name: "gemini", Binary: "gemini", Description: "Google Gemini CLI"},
	{Name: "claude", Binary: "claude", Description: "Anthropic Claude Code"},
	{Name: "qwen", Binary: "qwen", Description: "Qwen Code CLI"},
}

func InstalledAgents() []AgentStatus {
	return InstalledAgentsWithBinaryPaths(nil)
}

func InstalledAgentsWithBinaryPaths(binaryPaths map[string]string) []AgentStatus {
	statuses := make([]AgentStatus, 0, len(SupportedAgents))
	for _, agent := range SupportedAgents {
		path, err := resolveAgentBinaryPath(agent, binaryPaths)
		statuses = append(statuses, AgentStatus{
			AgentSpec: agent,
			Path:      path,
			Installed: err == nil,
		})
	}
	return statuses
}

func ResolveAgent(name string) (AgentSpec, error) {
	return ResolveAgentWithBinaryPaths(name, nil)
}

func ResolveAgentWithBinaryPaths(name string, binaryPaths map[string]string) (AgentSpec, error) {
	trimmed := normalizeAgentName(name)
	if trimmed == "" {
		for _, status := range InstalledAgentsWithBinaryPaths(binaryPaths) {
			if status.Installed {
				return status.AgentSpec, nil
			}
		}
		return AgentSpec{}, noSupportedAgentCLIError()
	}

	for _, agent := range SupportedAgents {
		if agent.Name == trimmed {
			if _, err := resolveAgentBinaryPath(agent, binaryPaths); err != nil {
				return AgentSpec{}, err
			}
			return agent, nil
		}
	}
	return AgentSpec{}, fmt.Errorf("unsupported agent %q", name)
}

func IsNoSupportedAgentCLI(err error) bool {
	return errors.Is(err, errNoSupportedAgentCLI)
}

func noSupportedAgentCLIError() error {
	return fmt.Errorf(
		"%w in configured paths or PATH; install one of: %s; run `sah agents` to inspect detection",
		errNoSupportedAgentCLI,
		supportedAgentNames(),
	)
}

func supportedAgentNames() string {
	names := make([]string, 0, len(SupportedAgents))
	for _, agent := range SupportedAgents {
		names = append(names, agent.Name)
	}
	return strings.Join(names, ", ")
}

func CaptureInstalledAgentBinaryPaths() map[string]string {
	paths := map[string]string{}
	for _, status := range InstalledAgents() {
		if status.Installed && strings.TrimSpace(status.Path) != "" {
			paths[status.Name] = status.Path
		}
	}
	return normalizeAgentBinaryPaths(paths)
}

func SolveAssignment(
	ctx context.Context,
	assignment Assignment,
	options AgentRunOptions,
) (*AgentResult, error) {
	agent, err := ResolveAgentWithBinaryPaths(options.Agent, options.BinaryPaths)
	if err != nil {
		return nil, err
	}

	request, err := ResolveAssignmentAgentRequest(assignment)
	if err != nil {
		return nil, err
	}

	timeout := options.Timeout
	if timeout <= 0 {
		timeout = DefaultAgentTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workdir, err := os.MkdirTemp("", "sah-agent-*")
	if err != nil {
		return nil, fmt.Errorf("create temp workspace: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(workdir)
	}()

	model := ModelForAgent(agent.Name, options.Model, options.Models)
	startedAt := time.Now()
	output, rawOutput, err := executeAgent(
		runCtx,
		agent,
		model,
		workdir,
		request,
		options.BinaryPaths,
	)
	if err != nil {
		return nil, err
	}

	payload, err := ParseAgentPayload(output.Text)
	if err != nil {
		return nil, err
	}

	return &AgentResult{
		Agent:     agent,
		Model:     model,
		RawOutput: rawOutput,
		Payload:   payload,
		Usage:     output.Usage,
		Duration:  time.Since(startedAt),
	}, nil
}

func executeAgent(
	ctx context.Context,
	agent AgentSpec,
	model string,
	workdir string,
	request *AssignmentAgentRequest,
	binaryPaths map[string]string,
) (*structuredAgentOutput, string, error) {
	command, useStdin, err := buildAgentCommand(ctx, agent, model, workdir, request, binaryPaths)
	if err != nil {
		return nil, "", err
	}
	if useStdin {
		command.Stdin = strings.NewReader(request.Prompt)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	runErr := command.Run()
	rawOutput := stdout.String()
	output, parseErr := parseStructuredOutput(agent.Name, rawOutput)

	if runErr != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" && output.AgentError != "" {
			message = output.AgentError
		}
		if message == "" && parseErr != nil {
			message = parseErr.Error()
		}
		if message == "" {
			message = strings.TrimSpace(rawOutput)
		}
		if message != "" {
			return nil, rawOutput, fmt.Errorf("%s failed: %w: %s", agent.Name, runErr, message)
		}
		return nil, rawOutput, fmt.Errorf("%s failed: %w", agent.Name, runErr)
	}

	if parseErr != nil {
		return nil, rawOutput, fmt.Errorf("%s returned unreadable structured output: %w", agent.Name, parseErr)
	}
	if output.AgentError != "" && strings.TrimSpace(output.Text) == "" {
		return nil, rawOutput, fmt.Errorf("%s failed: %s", agent.Name, output.AgentError)
	}

	return output, rawOutput, nil
}

func buildAgentCommand(
	ctx context.Context,
	agent AgentSpec,
	model string,
	workdir string,
	request *AssignmentAgentRequest,
	binaryPaths map[string]string,
) (*exec.Cmd, bool, error) {
	args, useStdin, err := agentCommandArgs(agent, request, workdir)
	if err != nil {
		return nil, false, err
	}
	args = appendAgentModelArg(args, model)
	commandPath, err := resolveAgentBinaryPath(agent, binaryPaths)
	if err != nil {
		return nil, false, err
	}

	command := exec.CommandContext(ctx, commandPath, args...)
	command.Dir = workdir
	command.Env = agentCommandEnv(agent)
	return command, useStdin, nil
}

func agentCommandEnv(agent AgentSpec) []string {
	if agent.Name != "claude" {
		return nil
	}
	return append(
		os.Environ(),
		"CLAUDE_CODE_DISABLE_GIT_INSTRUCTIONS=1",
		"CLAUDE_CODE_DISABLE_CLAUDE_MDS=1",
		"CLAUDE_CODE_DISABLE_AUTO_MEMORY=1",
		"CLAUDE_AGENT_SDK_DISABLE_BUILTIN_AGENTS=1",
	)
}

func agentCommandArgs(
	agent AgentSpec,
	request *AssignmentAgentRequest,
	workdir string,
) ([]string, bool, error) {
	prompt := ""
	if request != nil {
		prompt = request.Prompt
	}

	switch agent.Name {
	case "codex":
		return buildCodexCommandArgs(request, workdir)
	case "gemini":
		return []string{
			"--prompt", prompt,
			"--sandbox", "true",
			"--approval-mode", "plan",
			"--output-format", "stream-json",
			"-e", "none",
		}, false, nil
	case "claude":
		return buildClaudeCommandArgs(request, prompt)
	case "qwen":
		return []string{
			"--prompt", prompt,
			"--sandbox",
			"--approval-mode", "plan",
			"--output-format", "stream-json",
		}, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported agent %q", agent.Name)
	}
}

func buildCodexCommandArgs(request *AssignmentAgentRequest, workdir string) ([]string, bool, error) {
	args := []string{
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--color", "never",
		"--ephemeral",
		"--cd", workdir,
	}
	if request != nil && request.ResponseSchema != nil {
		schemaPath, err := writeOutputSchemaFile(workdir, request.ResponseSchema)
		if err != nil {
			return nil, false, err
		}
		args = append(args, "--output-schema", schemaPath)
	}
	args = append(args, "-")
	return args, true, nil
}

func buildClaudeCommandArgs(request *AssignmentAgentRequest, prompt string) ([]string, bool, error) {
	args := []string{
		"-p",
		"--verbose",
		"--output-format", "stream-json",
		"--permission-mode", "plan",
		"--tools", "",
		"--strict-mcp-config",
		"--disable-slash-commands",
		"--no-session-persistence",
		"--exclude-dynamic-system-prompt-sections",
		"--setting-sources", "local",
	}
	if request != nil && request.ResponseSchema != nil {
		schema, err := encodeInlineJSON(request.ResponseSchema)
		if err != nil {
			return nil, false, err
		}
		args = append(args, "--json-schema", schema)
	}
	args = append(args, prompt)
	return args, false, nil
}

func writeOutputSchemaFile(workdir string, schema any) (string, error) {
	data, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("encode response schema: %w", err)
	}
	path := filepath.Join(workdir, ".sah-output-schema.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write response schema: %w", err)
	}
	return path, nil
}

func encodeInlineJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode response schema: %w", err)
	}
	return string(data), nil
}

func appendAgentModelArg(args []string, model string) []string {
	if strings.TrimSpace(model) == "" {
		return args
	}
	return append(args, "--model", model)
}

func resolveAgentBinaryPath(agent AgentSpec, binaryPaths map[string]string) (string, error) {
	configuredPaths := normalizeAgentBinaryPaths(binaryPaths)
	if configuredPath, ok := configuredPaths[agent.Name]; ok {
		if isExecutablePath(configuredPath) {
			return configuredPath, nil
		}
		return "", fmt.Errorf("%s is configured at %s but unavailable; re-run `sah daemon install`", agent.Binary, configuredPath)
	}

	path, err := exec.LookPath(agent.Binary)
	if err == nil {
		return path, nil
	}

	return "", fmt.Errorf("%s is not installed or not on PATH", agent.Binary)
}

func isExecutablePath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func parseStructuredOutput(agentName string, raw string) (*structuredAgentOutput, error) {
	switch agentName {
	case "codex":
		return parseCodexStructuredOutput(raw)
	case "gemini":
		return parseGeminiStructuredOutput(raw)
	case "claude":
		return parseClaudeStructuredOutput(raw)
	case "qwen":
		return parseQwenStructuredOutput(raw)
	default:
		return nil, fmt.Errorf("unsupported agent %q", agentName)
	}
}

func parseCodexStructuredOutput(raw string) (*structuredAgentOutput, error) {
	output := &structuredAgentOutput{}

	if err := parseJSONLines(raw, func(event map[string]any) {
		switch stringValue(event["type"]) {
		case "item.completed":
			item := mapValue(event["item"])
			if stringValue(item["type"]) == "agent_message" {
				output.Text = strings.TrimSpace(stringValue(item["text"]))
			}
			if output.AgentError == "" && stringValue(item["type"]) == "error" && output.Text == "" {
				output.AgentError = strings.TrimSpace(stringValue(item["message"]))
			}
		case "turn.completed":
			output.Usage = parseCodexUsage(mapValue(event["usage"]))
		}
	}); err != nil {
		return nil, err
	}
	return output, nil
}

func parseGeminiStructuredOutput(raw string) (*structuredAgentOutput, error) {
	output := &structuredAgentOutput{}
	var assistantText strings.Builder

	if err := parseJSONLines(raw, func(event map[string]any) {
		switch stringValue(event["type"]) {
		case "message":
			if stringValue(event["role"]) == "assistant" {
				content := strings.TrimSpace(stringValue(event["content"]))
				if content != "" {
					if boolValue(event["delta"]) {
						assistantText.WriteString(content)
					} else {
						if assistantText.Len() > 0 {
							assistantText.WriteString("\n")
						}
						assistantText.WriteString(content)
					}
				}
			}
		case "result":
			if stringValue(event["status"]) != "success" && output.AgentError == "" {
				output.AgentError = strings.TrimSpace(stringValue(event["error"]))
			}
			output.Usage = parseGeminiUsage(mapValue(event["stats"]))
		}
	}); err != nil {
		return nil, err
	}

	output.Text = strings.TrimSpace(assistantText.String())
	return output, nil
}

func parseClaudeStructuredOutput(raw string) (*structuredAgentOutput, error) {
	return parseAssistantResultStructuredOutput(raw, parseClaudeUsage)
}

func parseQwenStructuredOutput(raw string) (*structuredAgentOutput, error) {
	return parseAssistantResultStructuredOutput(raw, parseQwenUsage)
}

func parseAssistantResultStructuredOutput(
	raw string,
	parseUsage func(map[string]any) TokenUsage,
) (*structuredAgentOutput, error) {
	output := &structuredAgentOutput{}
	var assistantText strings.Builder
	structuredText := ""

	if err := parseJSONLines(raw, func(event map[string]any) {
		switch stringValue(event["type"]) {
		case "assistant":
			appendAssistantText(&assistantText, event)
		case "result":
			structuredText = applyAssistantResultEvent(output, event, structuredText, parseUsage)
		}
	}); err != nil {
		return nil, err
	}

	output.Text = strings.TrimSpace(assistantText.String())
	if output.Text == "" {
		output.Text = structuredText
	}
	return output, nil
}

func appendAssistantText(builder *strings.Builder, event map[string]any) {
	message := mapValue(event["message"])
	content := arrayValue(message["content"])
	for _, item := range content {
		block := mapValue(item)
		if stringValue(block["type"]) != "text" {
			continue
		}
		text := strings.TrimSpace(stringValue(block["text"]))
		if text == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString(text)
	}
}

func applyAssistantResultEvent(
	output *structuredAgentOutput,
	event map[string]any,
	structuredText string,
	parseUsage func(map[string]any) TokenUsage,
) string {
	output.Usage = parseUsage(mapValue(event["usage"]))
	if structuredText == "" {
		structuredText = compactJSONText(event["structured_output"])
	}
	if boolValue(event["is_error"]) && output.AgentError == "" {
		output.AgentError = strings.TrimSpace(stringValue(event["result"]))
		if output.AgentError == "" {
			output.AgentError = strings.TrimSpace(stringValue(event["subtype"]))
		}
	}
	return structuredText
}

func compactJSONText(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func parseJSONLines(raw string, visit func(map[string]any)) error {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return fmt.Errorf("decode json line: %w", err)
		}
		visit(event)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan json lines: %w", err)
	}
	return nil
}

func parseCodexUsage(usage map[string]any) TokenUsage {
	input := int64Value(usage["input_tokens"])
	output := int64Value(usage["output_tokens"])
	total := int64Value(usage["total_tokens"])
	if total == 0 {
		total = input + output
	}

	return TokenUsage{
		Available:    len(usage) > 0,
		InputTokens:  input,
		OutputTokens: output,
		CachedTokens: int64Value(usage["cached_input_tokens"]),
		TotalTokens:  total,
	}
}

func parseGeminiUsage(stats map[string]any) TokenUsage {
	input := int64Value(stats["input_tokens"])
	output := int64Value(stats["output_tokens"])
	total := int64Value(stats["total_tokens"])
	if total == 0 {
		total = input + output
	}

	return TokenUsage{
		Available:      len(stats) > 0,
		InputTokens:    input,
		OutputTokens:   output,
		CachedTokens:   int64Value(stats["cached"]),
		InternalTokens: positiveDelta(total, input+output),
		TotalTokens:    total,
	}
}

func parseClaudeUsage(usage map[string]any) TokenUsage {
	input := int64Value(usage["input_tokens"])
	output := int64Value(usage["output_tokens"])
	cacheRead := int64Value(usage["cache_read_input_tokens"])
	cacheWrite := int64Value(usage["cache_creation_input_tokens"])
	total := int64Value(usage["total_tokens"])
	if total == 0 {
		total = input + output + cacheRead + cacheWrite
	}

	return TokenUsage{
		Available:        len(usage) > 0,
		InputTokens:      input,
		OutputTokens:     output,
		CachedTokens:     cacheRead,
		CacheWriteTokens: cacheWrite,
		InternalTokens:   positiveDelta(total, input+output+cacheRead+cacheWrite),
		TotalTokens:      total,
	}
}

func parseQwenUsage(usage map[string]any) TokenUsage {
	input := int64Value(usage["input_tokens"])
	output := int64Value(usage["output_tokens"])
	cacheWrite := int64Value(usage["cache_creation_input_tokens"])
	total := int64Value(usage["total_tokens"])
	if total == 0 {
		total = input + output + cacheWrite
	}
	return TokenUsage{
		Available:        len(usage) > 0,
		InputTokens:      input,
		OutputTokens:     output,
		CachedTokens:     int64Value(usage["cache_read_input_tokens"]),
		CacheWriteTokens: cacheWrite,
		InternalTokens:   positiveDelta(total, input+output+cacheWrite),
		TotalTokens:      total,
	}
}

func positiveDelta(total int64, known int64) int64 {
	if total <= known {
		return 0
	}
	return total - known
}

func mapValue(value any) map[string]any {
	if mapped, ok := value.(map[string]any); ok {
		return mapped
	}
	return map[string]any{}
}

func arrayValue(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}
	return nil
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed
		}
		floatParsed, err := typed.Float64()
		if err == nil {
			return int64(floatParsed)
		}
		return 0
	default:
		return 0
	}
}

func boolValue(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}
