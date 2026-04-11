package sah

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

type fakeWorkerClient struct {
	getTaskFunc            func(context.Context, string) (*Assignment, error)
	submitContributionFunc func(context.Context, SubmitContributionRequest) (*SubmitContributionResponse, error)
	releaseAssignmentFunc  func(context.Context, int64) error
}

func (client fakeWorkerClient) GetTask(ctx context.Context, taskType string) (*Assignment, error) {
	return client.getTaskFunc(ctx, taskType)
}

func (client fakeWorkerClient) SubmitContribution(
	ctx context.Context,
	request SubmitContributionRequest,
) (*SubmitContributionResponse, error) {
	return client.submitContributionFunc(ctx, request)
}

func (client fakeWorkerClient) ReleaseAssignment(ctx context.Context, assignmentID int64) error {
	if client.releaseAssignmentFunc == nil {
		return nil
	}
	return client.releaseAssignmentFunc(ctx, assignmentID)
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
		submitContributionFunc: func(context.Context, SubmitContributionRequest) (*SubmitContributionResponse, error) {
			t.Fatal("SubmitContribution should not be called after abort")
			return nil, nil
		},
		releaseAssignmentFunc: func(_ context.Context, assignmentID int64) error {
			released = append(released, assignmentID)
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
		submitContributionFunc: func(context.Context, SubmitContributionRequest) (*SubmitContributionResponse, error) {
			t.Fatal("SubmitContribution should not be called after local failure")
			return nil, nil
		},
		releaseAssignmentFunc: func(_ context.Context, assignmentID int64) error {
			released = append(released, assignmentID)
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
		submitContributionFunc: func(context.Context, SubmitContributionRequest) (*SubmitContributionResponse, error) {
			t.Fatal("SubmitContribution should not be called when task fetch is rate-limited")
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
