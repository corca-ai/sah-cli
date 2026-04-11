package sah

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetTaskMergesAssignmentLinksFromHeader(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("unexpected Accept header: %q", got)
		}
		writer.Header().Set(
			"Link",
			`</s@h/assignments/41/submission>; rel="submit", </s@h/assignments/41/release>; rel="release"`,
		)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"assignment_id": 41,
			"task_type": "novel-task",
			"task_key": "novel-task/v1",
			"payload": {"title": "Paper"},
			"instruction_version": "2026-04-10",
			"schema_version": "2026-04-09",
			"instructions": {
				"summary": "Do the assignment.",
				"rules": [],
				"good_examples": [],
				"bad_patterns": [],
				"submission_schema": {"type": "object"},
				"stop_conditions": []
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	assignment, err := client.GetTask(context.Background(), "")
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}

	if assignment.Links.Submit.Href != "/s@h/assignments/41/submission" {
		t.Fatalf("unexpected submit href: %q", assignment.Links.Submit.Href)
	}
	if assignment.Links.Release.Href != "/s@h/assignments/41/release" {
		t.Fatalf("unexpected release href: %q", assignment.Links.Release.Href)
	}
}

func TestSubmitAssignmentUsesAssignmentLinkWithoutTaskType(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		if request.URL.Path != "/s@h/assignments/41/submission" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, exists := body["task_type"]; exists {
			t.Fatalf("task_type should not be sent to assignment-scoped submission: %#v", body)
		}

		payload, ok := body["payload"].(map[string]any)
		if !ok || payload["answer"] != "ok" {
			t.Fatalf("unexpected payload: %#v", body)
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"assignment_id": 41,
			"contribution_id": 99,
			"credits_earned": 10,
			"pending_credits": 20
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	response, err := client.SubmitAssignment(
		context.Background(),
		Assignment{
			AssignmentID: 41,
			TaskType:     "novel-task",
			Links: AssignmentLinks{
				Submit: AssignmentLink{
					Href:   "/s@h/assignments/41/submission",
					Method: http.MethodPut,
				},
			},
		},
		map[string]any{"answer": "ok"},
	)
	if err != nil {
		t.Fatalf("SubmitAssignment returned error: %v", err)
	}
	if response.ContributionID != 99 {
		t.Fatalf("unexpected contribution id: %d", response.ContributionID)
	}
}

func TestReleaseOpenAssignmentUsesReleaseLinkMethod(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodDelete {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		if request.URL.Path != "/s@h/assignments/41/release" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	err := client.ReleaseOpenAssignment(
		context.Background(),
		Assignment{
			AssignmentID: 41,
			Links: AssignmentLinks{
				Release: AssignmentLink{
					Href:   "/s@h/assignments/41/release",
					Method: http.MethodDelete,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("ReleaseOpenAssignment returned error: %v", err)
	}
}
