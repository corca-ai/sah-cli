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
	Model           string
	Models          map[string]string
	Interval        time.Duration
	Timeout         time.Duration
	TaskType        string
	Once            bool
	Output          io.Writer
	ErrorOutput     io.Writer
}

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
		return runWorkerCycle(ctx, client, picker.Next(), options)
	}

	ticker := time.NewTicker(options.Interval)
	defer ticker.Stop()

	for {
		if err := runWorkerCycle(ctx, client, picker.Next(), options); err != nil {
			if IsStatus(err, http.StatusUnauthorized) || IsStatus(err, http.StatusForbidden) {
				return fmt.Errorf("api key rejected; run `sah auth login` again")
			}
			logLine(options.ErrorOutput, "worker cycle failed: %v", err)
		}

		logLine(options.Output, "sleeping for %s", options.Interval)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func runWorkerCycle(
	ctx context.Context,
	client *Client,
	agent AgentSpec,
	options WorkerOptions,
) error {
	assignment, err := client.GetTask(ctx, options.TaskType)
	switch {
	case IsStatus(err, http.StatusNoContent):
		logLine(options.Output, "no task available")
		return nil
	case IsStatus(err, http.StatusTooManyRequests):
		logLine(options.Output, "too many pending assignments; waiting for reviews")
		return nil
	case err != nil:
		return err
	}

	logLine(
		options.Output,
		"picked assignment %d (%s / %s)",
		assignment.AssignmentID,
		assignment.TaskType,
		assignment.TaskKey,
	)

	result, err := SolveAssignment(ctx, *assignment, AgentRunOptions{
		Agent:   agent.Name,
		Model:   options.Model,
		Models:  options.Models,
		Timeout: options.Timeout,
	})
	if err != nil {
		var abortErr *AbortError
		if errors.As(err, &abortErr) {
			logLine(options.Output, "agent skipped assignment %d: %s", assignment.AssignmentID, abortErr.Reason)
			return nil
		}
		return fmt.Errorf("solve assignment %d: %w", assignment.AssignmentID, err)
	}

	response, err := client.SubmitContribution(ctx, SubmitContributionRequest{
		AssignmentID: assignment.AssignmentID,
		TaskType:     assignment.TaskType,
		Payload:      result.Payload,
	})
	if err != nil {
		return fmt.Errorf("submit assignment %d: %w", assignment.AssignmentID, err)
	}

	PrintCycleSummary(options.Output, *assignment, result, response)
	return nil
}

func logLine(writer io.Writer, format string, args ...any) {
	if writer == nil {
		return
	}
	fmt.Fprintf(writer, "[%s] %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}
