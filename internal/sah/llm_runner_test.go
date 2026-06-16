package sah

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSolveAssignmentWithLLMUsesStructuredOpenAIChatEndpoint(t *testing.T) {
	server, seenRequest := newStructuredLLMTestServer(t)
	defer server.Close()

	result, err := SolveAssignment(context.Background(), structuredLLMAssignment(), AgentRunOptions{
		Backend:      WorkerBackendLLM,
		LLMBaseURL:   server.URL,
		LLMModel:     "test-model",
		LLMMaxTokens: 123,
	})
	if err != nil {
		t.Fatalf("SolveAssignment returned error: %v", err)
	}

	if result.Agent.Name != LLMAgentSpec.Name {
		t.Fatalf("expected llm agent spec, got %q", result.Agent.Name)
	}
	if result.Model != "test-model" {
		t.Fatalf("unexpected model: %q", result.Model)
	}
	if got := result.Payload["answer"]; got != "ok" {
		t.Fatalf("unexpected payload answer: %#v", got)
	}
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 4 || result.Usage.CachedTokens != 3 {
		t.Fatalf("unexpected usage: %#v", result.Usage)
	}

	assertStructuredLLMRequest(t, *seenRequest)
}

func newStructuredLLMTestServer(t *testing.T) (*httptest.Server, *openAIChatCompletionRequest) {
	t.Helper()

	seenRequest := &openAIChatCompletionRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(seenRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"choices": [{"message": {"role": "assistant", "content": "{\"answer\":\"ok\",\"score\":1}"}}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 4,
				"total_tokens": 14,
				"prompt_tokens_details": {"cached_tokens": 3}
			}
		}`))
	}))
	return server, seenRequest
}

func structuredLLMAssignment() Assignment {
	return Assignment{
		AssignmentID: 42,
		TaskType:     "verification",
		TaskKey:      "paper/42",
		AgentRequest: &AssignmentAgentRequest{
			Prompt: "Return the answer.",
			ResponseSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"answer": map[string]any{"type": "string"},
					"score":  map[string]any{"type": "number"},
				},
				"required": []string{"answer", "score"},
			},
		},
	}
}

func assertStructuredLLMRequest(t *testing.T, request openAIChatCompletionRequest) {
	t.Helper()

	if request.Model != "test-model" {
		t.Fatalf("unexpected request model: %q", request.Model)
	}
	if request.MaxTokens != 123 {
		t.Fatalf("unexpected max tokens: %d", request.MaxTokens)
	}
	if len(request.Messages) != 2 || !strings.Contains(request.Messages[1].Content, "Return the answer.") {
		t.Fatalf("unexpected messages: %#v", request.Messages)
	}

	format, ok := request.ResponseFormat.(map[string]any)
	if !ok {
		t.Fatalf("expected response_format object, got %#v", request.ResponseFormat)
	}
	if format["type"] != "json_schema" {
		t.Fatalf("expected json_schema response format, got %#v", format)
	}
}

func TestSolveAssignmentWithLLMRequiresEndpointAndModel(t *testing.T) {
	assignment := Assignment{
		AssignmentID: 1,
		TaskType:     "verification",
		TaskKey:      "paper/1",
		AgentRequest: &AssignmentAgentRequest{Prompt: "Return JSON."},
	}

	if _, err := SolveAssignment(context.Background(), assignment, AgentRunOptions{
		Backend:  WorkerBackendLLM,
		LLMModel: "test-model",
	}); err == nil || !strings.Contains(err.Error(), "base URL") {
		t.Fatalf("expected missing URL error, got %v", err)
	}

	if _, err := SolveAssignment(context.Background(), assignment, AgentRunOptions{
		Backend:    WorkerBackendLLM,
		LLMBaseURL: "http://127.0.0.1:18080",
	}); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("expected missing model error, got %v", err)
	}
}
