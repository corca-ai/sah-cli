package sah

import (
	"fmt"
	"time"
)

const maxAgentBackoff = 6 * time.Hour

type AgentFailure struct {
	Agent AgentSpec
	Err   error
}

func (err *AgentFailure) Error() string {
	return fmt.Sprintf("%s failed: %v", err.Agent.Name, err.Err)
}

func (err *AgentFailure) Unwrap() error {
	return err.Err
}

type agentCooldown struct {
	failures int
	readyAt  time.Time
	lastErr  string
}

type AgentBackoff struct {
	base   time.Duration
	states map[string]*agentCooldown
}

func NewAgentBackoff(base time.Duration) *AgentBackoff {
	if base <= 0 {
		base = DefaultPollInterval
	}
	return &AgentBackoff{
		base:   base,
		states: map[string]*agentCooldown{},
	}
}

func (backoff *AgentBackoff) RecordFailure(agent AgentSpec, err error, now time.Time) time.Duration {
	state, ok := backoff.states[agent.Name]
	if !ok {
		state = &agentCooldown{}
		backoff.states[agent.Name] = state
	}

	state.failures++
	delay := backoff.base
	for i := 1; i < state.failures; i++ {
		delay *= 2
		if delay >= maxAgentBackoff {
			delay = maxAgentBackoff
			break
		}
	}
	state.readyAt = now.Add(delay)
	if err != nil {
		state.lastErr = err.Error()
	}
	return delay
}

func (backoff *AgentBackoff) RecordSuccess(agent AgentSpec) {
	delete(backoff.states, agent.Name)
}

func (backoff *AgentBackoff) State(agent AgentSpec) (readyAt time.Time, lastErr string, failures int, blocked bool) {
	state, ok := backoff.states[agent.Name]
	if !ok {
		return time.Time{}, "", 0, false
	}
	return state.readyAt, state.lastErr, state.failures, true
}

func (backoff *AgentBackoff) SelectReadyAgent(
	picker *AgentPicker,
	now time.Time,
) (AgentSpec, time.Time, bool) {
	var earliest time.Time
	poolSize := len(picker.pool)
	for i := 0; i < poolSize; i++ {
		agent := picker.Next()
		readyAt, _, _, blocked := backoff.State(agent)
		if !blocked || !readyAt.After(now) {
			return agent, time.Time{}, true
		}
		if earliest.IsZero() || readyAt.Before(earliest) {
			earliest = readyAt
		}
	}
	return AgentSpec{}, earliest, false
}
