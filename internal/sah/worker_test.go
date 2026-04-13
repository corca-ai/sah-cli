package sah

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeWorkerClient struct {
	getTaskFunc             func(context.Context, string) (*Assignment, error)
	submitAssignmentFunc    func(context.Context, Assignment, map[string]any) (*SubmitContributionResponse, error)
	releaseOpenAssignmentFn func(context.Context, Assignment) error
}

func (client fakeWorkerClient) GetTask(ctx context.Context, taskType string) (*Assignment, error) {
	return client.getTaskFunc(ctx, taskType)
}

func (client fakeWorkerClient) SubmitAssignment(
	ctx context.Context,
	assignment Assignment,
	payload map[string]any,
) (*SubmitContributionResponse, error) {
	return client.submitAssignmentFunc(ctx, assignment, payload)
}

func (client fakeWorkerClient) ReleaseOpenAssignment(ctx context.Context, assignment Assignment) error {
	if client.releaseOpenAssignmentFn == nil {
		return nil
	}
	return client.releaseOpenAssignmentFn(ctx, assignment)
}

func TestHandleWorkerCycleErrorPropagatesContextCanceledWithoutLogging(t *testing.T) {
	var stderr bytes.Buffer

	err := handleWorkerCycleError(NewAgentBackoff(time.Minute), context.Canceled, WorkerOptions{
		ErrorOutput: &stderr,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no stderr output, got %q", stderr.String())
	}
}

func TestNormalizeContextCancelSuppressesContextCanceled(t *testing.T) {
	if err := normalizeContextCancel(context.Canceled); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if err := normalizeContextCancel(errors.New("boom")); err == nil {
		t.Fatal("expected non-nil error to pass through")
	}
}

func TestRunWorkerCycleReleasesAssignmentAfterAbort(t *testing.T) {
	previous := solveAssignment
	t.Cleanup(func() {
		solveAssignment = previous
	})

	solveAssignment = func(
		ctx context.Context,
		assignment Assignment,
		options AgentRunOptions,
	) (*AgentResult, error) {
		return nil, &AbortError{Reason: "bad task input"}
	}

	var stdout bytes.Buffer
	var released []int64
	client := fakeWorkerClient{
		getTaskFunc: func(context.Context, string) (*Assignment, error) {
			return &Assignment{
				AssignmentID: 42,
				TaskType:     "extraction",
				TaskKey:      "paper/42",
				Payload:      map[string]any{"corpus_id": 42},
			}, nil
		},
		submitAssignmentFunc: func(context.Context, Assignment, map[string]any) (*SubmitContributionResponse, error) {
			t.Fatal("SubmitAssignment should not be called after abort")
			return nil, nil
		},
		releaseOpenAssignmentFn: func(_ context.Context, assignment Assignment) error {
			released = append(released, assignment.AssignmentID)
			return nil
		},
	}

	result, err := runWorkerCycle(context.Background(), client, AgentSpec{Name: "codex"}, WorkerOptions{
		Output:      &stdout,
		ErrorOutput: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !result.AgentHealthy {
		t.Fatal("expected abort path to keep agent healthy")
	}
	if len(released) != 1 || released[0] != 42 {
		t.Fatalf("expected assignment 42 to be released, got %v", released)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("released assignment 42 without submission")) {
		t.Fatalf("expected release log, got %q", stdout.String())
	}
}

func TestRunWorkerCycleReleasesAssignmentAfterLocalFailure(t *testing.T) {
	previous := solveAssignment
	t.Cleanup(func() {
		solveAssignment = previous
	})

	solveAssignment = func(
		ctx context.Context,
		assignment Assignment,
		options AgentRunOptions,
	) (*AgentResult, error) {
		return nil, errors.New("agent exploded")
	}

	var released []int64
	client := fakeWorkerClient{
		getTaskFunc: func(context.Context, string) (*Assignment, error) {
			return &Assignment{
				AssignmentID: 77,
				TaskType:     "verification",
				TaskKey:      "verification",
				Payload:      map[string]any{"title": "Paper"},
			}, nil
		},
		submitAssignmentFunc: func(context.Context, Assignment, map[string]any) (*SubmitContributionResponse, error) {
			t.Fatal("SubmitAssignment should not be called after local failure")
			return nil, nil
		},
		releaseOpenAssignmentFn: func(_ context.Context, assignment Assignment) error {
			released = append(released, assignment.AssignmentID)
			return nil
		},
	}

	_, err := runWorkerCycle(context.Background(), client, AgentSpec{Name: "gemini"}, WorkerOptions{
		Output:      &bytes.Buffer{},
		ErrorOutput: &bytes.Buffer{},
	})
	var agentFailure *AgentFailure
	if !errors.As(err, &agentFailure) {
		t.Fatalf("expected AgentFailure, got %v", err)
	}
	if len(released) != 1 || released[0] != 77 {
		t.Fatalf("expected assignment 77 to be released, got %v", released)
	}
}

func TestRunWorkerCycleUsesClearOpenAssignmentMessage(t *testing.T) {
	var stdout bytes.Buffer
	client := fakeWorkerClient{
		getTaskFunc: func(context.Context, string) (*Assignment, error) {
			return nil, &StatusError{
				StatusCode: 429,
				Message:    "Too many open assignments. Submit completed work or wait for older assignments to expire.",
			}
		},
		submitAssignmentFunc: func(context.Context, Assignment, map[string]any) (*SubmitContributionResponse, error) {
			t.Fatal("SubmitAssignment should not be called when task fetch is rate-limited")
			return nil, nil
		},
	}

	_, err := runWorkerCycle(context.Background(), client, AgentSpec{Name: "codex"}, WorkerOptions{
		Output:      &stdout,
		ErrorOutput: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("Too many open assignments")) {
		t.Fatalf("expected clear cap message, got %q", stdout.String())
	}
}

func TestRunWorkerCycleLogsNoTaskAvailableOnNoContent(t *testing.T) {
	var stdout bytes.Buffer
	client := fakeWorkerClient{
		getTaskFunc: func(context.Context, string) (*Assignment, error) {
			return nil, &StatusError{StatusCode: http.StatusNoContent, Message: "No Content"}
		},
		submitAssignmentFunc: func(context.Context, Assignment, map[string]any) (*SubmitContributionResponse, error) {
			t.Fatal("SubmitAssignment should not be called when no task is available")
			return nil, nil
		},
	}

	result, err := runWorkerCycle(context.Background(), client, AgentSpec{Name: "claude"}, WorkerOptions{
		Output:      &stdout,
		ErrorOutput: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result.AgentHealthy {
		t.Fatal("expected no-task cycle to be neutral, not an agent-health signal")
	}
	if !bytes.Contains(stdout.Bytes(), []byte("no task available")) {
		t.Fatalf("expected no-task log, got %q", stdout.String())
	}
}

func TestRunWorkerCycleRejectsInvalidAssignmentPayload(t *testing.T) {
	client := fakeWorkerClient{
		getTaskFunc: func(context.Context, string) (*Assignment, error) {
			return &Assignment{}, nil
		},
		submitAssignmentFunc: func(context.Context, Assignment, map[string]any) (*SubmitContributionResponse, error) {
			t.Fatal("SubmitAssignment should not be called for invalid assignments")
			return nil, nil
		},
	}

	_, err := runWorkerCycle(context.Background(), client, AgentSpec{Name: "claude"}, WorkerOptions{
		Output:      &bytes.Buffer{},
		ErrorOutput: &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid assignment payload") {
		t.Fatalf("expected invalid assignment error, got %v", err)
	}
}

func TestRunWorkerCycleReleasesAssignmentAfterSubmitFailure(t *testing.T) {
	previous := solveAssignment
	t.Cleanup(func() {
		solveAssignment = previous
	})

	solveAssignment = func(
		ctx context.Context,
		assignment Assignment,
		options AgentRunOptions,
	) (*AgentResult, error) {
		return &AgentResult{
			Payload: map[string]any{"verdict": "approve", "reason": "grounded"},
		}, nil
	}

	var released []int64
	client := fakeWorkerClient{
		getTaskFunc: func(context.Context, string) (*Assignment, error) {
			return &Assignment{
				AssignmentID: 88,
				TaskType:     "verification",
				TaskKey:      "verification",
				Payload:      map[string]any{"title": "Paper"},
			}, nil
		},
		submitAssignmentFunc: func(context.Context, Assignment, map[string]any) (*SubmitContributionResponse, error) {
			return nil, errors.New("network timeout")
		},
		releaseOpenAssignmentFn: func(_ context.Context, assignment Assignment) error {
			released = append(released, assignment.AssignmentID)
			return nil
		},
	}

	_, err := runWorkerCycle(context.Background(), client, AgentSpec{Name: "codex"}, WorkerOptions{
		Output:      &bytes.Buffer{},
		ErrorOutput: &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "submit assignment 88") {
		t.Fatalf("expected submit assignment error, got %v", err)
	}
	if len(released) != 1 || released[0] != 88 {
		t.Fatalf("expected assignment 88 to be released, got %v", released)
	}
}
