package sah

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

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
