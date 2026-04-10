package sah

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	Agent   string
	Model   string
	Timeout time.Duration
}

type AgentResult struct {
	RawOutput string
	Payload   map[string]any
}

var SupportedAgents = []AgentSpec{
	{Name: "codex", Binary: "codex", Description: "OpenAI Codex CLI"},
	{Name: "gemini", Binary: "gemini", Description: "Google Gemini CLI"},
	{Name: "claude", Binary: "claude", Description: "Anthropic Claude Code"},
}

func InstalledAgents() []AgentStatus {
	statuses := make([]AgentStatus, 0, len(SupportedAgents))
	for _, agent := range SupportedAgents {
		path, err := exec.LookPath(agent.Binary)
		statuses = append(statuses, AgentStatus{
			AgentSpec: agent,
			Path:      path,
			Installed: err == nil,
		})
	}
	return statuses
}

func ResolveAgent(name string) (AgentSpec, error) {
	trimmed := strings.TrimSpace(strings.ToLower(name))
	if trimmed == "" {
		for _, status := range InstalledAgents() {
			if status.Installed {
				return status.AgentSpec, nil
			}
		}
		return AgentSpec{}, fmt.Errorf("no supported agent CLI found in PATH")
	}

	for _, agent := range SupportedAgents {
		if agent.Name == trimmed {
			if _, err := exec.LookPath(agent.Binary); err != nil {
				return AgentSpec{}, fmt.Errorf("%s is not installed or not on PATH", agent.Binary)
			}
			return agent, nil
		}
	}
	return AgentSpec{}, fmt.Errorf("unsupported agent %q", name)
}

func SolveAssignment(
	ctx context.Context,
	assignment Assignment,
	options AgentRunOptions,
) (*AgentResult, error) {
	agent, err := ResolveAgent(options.Agent)
	if err != nil {
		return nil, err
	}

	prompt, err := BuildAgentPrompt(assignment)
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
	defer os.RemoveAll(workdir)

	command, err := buildAgentCommand(runCtx, agent, options.Model, workdir)
	if err != nil {
		return nil, err
	}
	command.Stdin = strings.NewReader(prompt)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		if stderrText := strings.TrimSpace(stderr.String()); stderrText != "" {
			return nil, fmt.Errorf("%s failed: %w: %s", agent.Name, err, stderrText)
		}
		return nil, fmt.Errorf("%s failed: %w", agent.Name, err)
	}

	payload, err := ParseAgentPayload(stdout.String())
	if err != nil {
		return nil, err
	}

	return &AgentResult{
		RawOutput: stdout.String(),
		Payload:   payload,
	}, nil
}

func buildAgentCommand(
	ctx context.Context,
	agent AgentSpec,
	model string,
	workdir string,
) (*exec.Cmd, error) {
	args := []string{}

	switch agent.Name {
	case "codex":
		args = []string{
			"exec",
			"--skip-git-repo-check",
			"--sandbox", "read-only",
			"--color", "never",
			"--ephemeral",
			"--cd", workdir,
			"-",
		}
	case "gemini":
		args = []string{
			"--prompt", "",
			"--sandbox", "true",
			"--approval-mode", "plan",
			"--output-format", "text",
		}
	case "claude":
		args = []string{
			"-p",
			"--output-format", "text",
			"--permission-mode", "plan",
			"--tools", "",
			"--no-session-persistence",
			"--bare",
		}
	default:
		return nil, fmt.Errorf("unsupported agent %q", agent.Name)
	}

	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", model)
	}

	command := exec.CommandContext(ctx, agent.Binary, args...)
	command.Dir = workdir
	return command, nil
}
