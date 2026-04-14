package sah

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type WorkerOptions struct {
	Agent           string
	Agents          []string
	RotateInstalled bool
	BinaryPaths     map[string]string
	Model           string
	Models          map[string]string
	Interval        time.Duration
	Timeout         time.Duration
	TaskType        string
	Once            bool
	Output          io.Writer
	ErrorOutput     io.Writer
}

type WorkerCycleResult struct {
	AgentHealthy bool
}

type workerClient interface {
	GetTask(ctx context.Context, taskType string) (*Assignment, error)
	SubmitAssignment(
		ctx context.Context,
		assignment Assignment,
		payload map[string]any,
	) (*SubmitContributionResponse, error)
	ReleaseOpenAssignment(ctx context.Context, assignment Assignment) error
}

var solveAssignment = SolveAssignment

const releaseAssignmentTimeout = 10 * time.Second

func RunWorker(ctx context.Context, config Config, options WorkerOptions) error {
	client := NewClient(config.BaseURL, config.APIKey)

	if strings.TrimSpace(config.APIKey) == "" {
		return fmt.Errorf("not authenticated; run `sah auth login` first")
	}

	picker, err := NewAgentPicker(config, options)
	if err != nil {
		return err
	}

	if options.Once {
		_, err := runWorkerCycle(ctx, client, picker.Next(), options)
		return normalizeContextCancel(err)
	}

	return normalizeContextCancel(runContinuousWorker(ctx, client, picker, options))
}

func runContinuousWorker(
	ctx context.Context,
	client *Client,
	picker *AgentPicker,
	options WorkerOptions,
) error {
	backoff := NewAgentBackoff(options.Interval)
	for {
		agent, err := waitForReadyAgent(ctx, backoff, picker, options)
		if err != nil {
			return err
		}

		result, err := runWorkerCycle(ctx, client, agent, options)
		if result.AgentHealthy {
			clearAgentCooldown(backoff, agent, options.Output)
		}

		if err := handleWorkerCycleError(backoff, err, options); err != nil {
			return err
		}

		logLine(options.Output, "sleeping for %s", options.Interval)
		if err := sleepWithContext(ctx, options.Interval); err != nil {
			return err
		}
	}
}

func waitForReadyAgent(
	ctx context.Context,
	backoff *AgentBackoff,
	picker *AgentPicker,
	options WorkerOptions,
) (AgentSpec, error) {
	for {
		agent, readyAt, ok := backoff.SelectReadyAgent(picker, time.Now())
		if ok {
			return agent, nil
		}

		wait := time.Until(readyAt)
		if wait <= 0 {
			continue
		}
		logLine(options.Output, "all configured agents are cooling down; next retry in %s", humanDuration(wait))
		if err := sleepWithContext(ctx, wait); err != nil {
			return AgentSpec{}, err
		}
	}
}

func clearAgentCooldown(backoff *AgentBackoff, agent AgentSpec, writer io.Writer) {
	if _, _, failures, blocked := backoff.State(agent); blocked && failures > 0 {
		logLine(writer, "%s recovered; cooldown cleared", agent.Name)
	}
	backoff.RecordSuccess(agent)
}

func handleWorkerCycleError(backoff *AgentBackoff, err error, options WorkerOptions) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if IsStatus(err, http.StatusUnauthorized) || IsStatus(err, http.StatusForbidden) {
		return fmt.Errorf("api key rejected; run `sah auth login` again")
	}
	if IsWorkerContractError(err) {
		return err
	}

	var agentFailure *AgentFailure
	if errors.As(err, &agentFailure) {
		delay := backoff.RecordFailure(agentFailure.Agent, agentFailure.Err, time.Now())
		_, lastErr, failures, _ := backoff.State(agentFailure.Agent)
		logLine(
			options.ErrorOutput,
			"%s cooling down for %s after failure #%d: %s",
			agentFailure.Agent.Name,
			humanDuration(delay),
			failures,
			lastErr,
		)
		return nil
	}

	logLine(options.ErrorOutput, "worker cycle failed: %v", err)
	return nil
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func normalizeContextCancel(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func runWorkerCycle(
	ctx context.Context,
	client workerClient,
	agent AgentSpec,
	options WorkerOptions,
) (WorkerCycleResult, error) {
	assignment, err := client.GetTask(ctx, options.TaskType)
	switch {
	case IsStatus(err, http.StatusNoContent):
		logLine(options.Output, "no task available")
		return WorkerCycleResult{}, nil
	case IsStatus(err, http.StatusTooManyRequests):
		logLine(options.Output, "%s", openAssignmentLimitMessage(err))
		return WorkerCycleResult{}, nil
	case err != nil:
		return WorkerCycleResult{}, err
	}
	if assignment == nil {
		return WorkerCycleResult{}, fmt.Errorf("task fetch returned no assignment payload")
	}
	if assignment.AssignmentID <= 0 || strings.TrimSpace(assignment.TaskType) == "" {
		return WorkerCycleResult{}, fmt.Errorf(
			"task fetch returned invalid assignment payload: id=%d task_type=%q",
			assignment.AssignmentID,
			assignment.TaskType,
		)
	}
	if err := ValidateAssignmentContract(*assignment); err != nil {
		releaseAssignmentOnFailure(client, *assignment, options)
		return WorkerCycleResult{}, fmt.Errorf("assignment %d: %w", assignment.AssignmentID, err)
	}

	logLine(
		options.Output,
		"picked assignment %d (%s / %s) with %s",
		assignment.AssignmentID,
		assignment.TaskType,
		assignment.TaskKey,
		agent.Name,
	)

	result, solveErr := solveAssignment(ctx, *assignment, AgentRunOptions{
		Agent:       agent.Name,
		Model:       options.Model,
		Models:      options.Models,
		BinaryPaths: options.BinaryPaths,
		Timeout:     options.Timeout,
	})
	if solveErr != nil {
		releaseAssignmentOnFailure(client, *assignment, options)
		var abortErr *AbortError
		if errors.As(solveErr, &abortErr) {
			logLine(options.Output, "agent skipped assignment %d: %s", assignment.AssignmentID, abortErr.Reason)
			logLine(options.Output, "released assignment %d without submission", assignment.AssignmentID)
			return WorkerCycleResult{AgentHealthy: true}, nil
		}
		logLine(options.Output, "released assignment %d after local failure", assignment.AssignmentID)
		return WorkerCycleResult{}, &AgentFailure{
			Agent: agent,
			Err:   fmt.Errorf("solve assignment %d: %w", assignment.AssignmentID, solveErr),
		}
	}

	response, err := client.SubmitAssignment(ctx, *assignment, result.Payload)
	if err != nil {
		releaseAssignmentOnFailure(client, *assignment, options)
		return WorkerCycleResult{AgentHealthy: true}, fmt.Errorf("submit assignment %d: %w", assignment.AssignmentID, err)
	}

	PrintCycleSummary(options.Output, *assignment, result, response)
	return WorkerCycleResult{AgentHealthy: true}, nil
}

func releaseAssignmentOnFailure(client workerClient, assignment Assignment, options WorkerOptions) {
	if client == nil || assignment.AssignmentID == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), releaseAssignmentTimeout)
	defer cancel()

	err := client.ReleaseOpenAssignment(ctx, assignment)
	switch {
	case err == nil:
		return
	case IsStatus(err, http.StatusNotFound), IsStatus(err, http.StatusConflict):
		return
	default:
		logLine(options.ErrorOutput, "failed to release assignment %d: %v", assignment.AssignmentID, err)
	}
}

func openAssignmentLimitMessage(err error) string {
	if message := statusMessage(err); message != "" {
		return message
	}
	return "Too many open assignments. Submit completed work or wait for older assignments to expire."
}

func logLine(writer io.Writer, format string, args ...any) {
	if writer == nil {
		return
	}
	_, _ = fmt.Fprintf(writer, "[%s] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}
