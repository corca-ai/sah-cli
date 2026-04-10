package sah

import (
	"errors"
	"testing"
)

func TestParseAgentPayloadAcceptsPlainJSONObject(t *testing.T) {
	payload, err := ParseAgentPayload(`{"answer":"ok","score":1}`)
	if err != nil {
		t.Fatalf("ParseAgentPayload returned error: %v", err)
	}
	if payload["answer"] != "ok" {
		t.Fatalf("unexpected answer: %#v", payload["answer"])
	}
}

func TestParseAgentPayloadExtractsJSONObjectFromMarkdownFence(t *testing.T) {
	payload, err := ParseAgentPayload("```json\n{\"answer\":\"ok\"}\n```")
	if err != nil {
		t.Fatalf("ParseAgentPayload returned error: %v", err)
	}
	if payload["answer"] != "ok" {
		t.Fatalf("unexpected answer: %#v", payload["answer"])
	}
}

func TestParseAgentPayloadRecognizesAbort(t *testing.T) {
	_, err := ParseAgentPayload("ABORT: not enough evidence")
	if err == nil {
		t.Fatal("expected abort error")
	}

	var abortErr *AbortError
	if !errors.As(err, &abortErr) {
		t.Fatalf("expected AbortError, got %T", err)
	}
	if abortErr.Reason != "not enough evidence" {
		t.Fatalf("unexpected abort reason: %q", abortErr.Reason)
	}
}
