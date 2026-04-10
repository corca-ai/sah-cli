package sah

import (
	"errors"
	"testing"
	"time"
)

func TestAgentBackoffDoublesDelay(t *testing.T) {
	backoff := NewAgentBackoff(time.Minute)
	agent := AgentSpec{Name: "codex"}
	now := time.Date(2026, time.April, 10, 10, 0, 0, 0, time.UTC)

	first := backoff.RecordFailure(agent, errors.New("timeout"), now)
	second := backoff.RecordFailure(agent, errors.New("timeout"), now)

	if first != time.Minute {
		t.Fatalf("unexpected first delay: %s", first)
	}
	if second != 2*time.Minute {
		t.Fatalf("unexpected second delay: %s", second)
	}

	readyAt, lastErr, failures, blocked := backoff.State(agent)
	if !blocked {
		t.Fatal("expected blocked state")
	}
	if failures != 2 {
		t.Fatalf("unexpected failure count: %d", failures)
	}
	if lastErr != "timeout" {
		t.Fatalf("unexpected last error: %q", lastErr)
	}
	if !readyAt.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("unexpected readyAt: %s", readyAt)
	}
}

func TestAgentBackoffSelectReadyAgentSkipsCoolingAgent(t *testing.T) {
	backoff := NewAgentBackoff(time.Minute)
	now := time.Date(2026, time.April, 10, 10, 0, 0, 0, time.UTC)
	picker := &AgentPicker{
		pool: []AgentSpec{
			{Name: "codex"},
			{Name: "gemini"},
		},
	}

	backoff.RecordFailure(picker.pool[0], errors.New("timeout"), now)

	agent, _, ok := backoff.SelectReadyAgent(picker, now)
	if !ok {
		t.Fatal("expected a ready agent")
	}
	if agent.Name != "gemini" {
		t.Fatalf("unexpected agent: %s", agent.Name)
	}
}

func TestAgentBackoffReturnsEarliestReadyTimeWhenAllAgentsBlocked(t *testing.T) {
	backoff := NewAgentBackoff(time.Minute)
	now := time.Date(2026, time.April, 10, 10, 0, 0, 0, time.UTC)
	picker := &AgentPicker{
		pool: []AgentSpec{
			{Name: "codex"},
			{Name: "gemini"},
		},
	}

	backoff.RecordFailure(picker.pool[0], errors.New("timeout"), now)
	backoff.RecordFailure(picker.pool[1], errors.New("rate limited"), now.Add(30*time.Second))

	agent, readyAt, ok := backoff.SelectReadyAgent(picker, now)
	if ok {
		t.Fatalf("expected no ready agent, got %s", agent.Name)
	}
	if !readyAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("unexpected earliest ready time: %s", readyAt)
	}
}
