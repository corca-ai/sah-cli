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
		if got := request.Header.Get(TaskProtocolHeader); got != SupportedTaskProtocol {
			t.Fatalf("unexpected task protocol header: %q", got)
		}
		if got := request.Header.Get(ClientCapabilitiesHeader); got != SupportedClientCapabilitiesHeaderValue() {
			t.Fatalf("unexpected capabilities header: %q", got)
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

func TestGetTaskReturnsNoContentStatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/s@h/tasks" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	assignment, err := client.GetTask(context.Background(), "")
	if !IsStatus(err, http.StatusNoContent) {
		t.Fatalf("expected 204 status error, got assignment=%#v err=%v", assignment, err)
	}
	if assignment != nil {
		t.Fatalf("expected nil assignment on 204, got %#v", assignment)
	}
}

func TestGetMeDoesNotSendWorkerContractHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get(TaskProtocolHeader); got != "" {
			t.Fatalf("did not expect task protocol header, got %q", got)
		}
		if got := request.Header.Get(ClientCapabilitiesHeader); got != "" {
			t.Fatalf("did not expect capabilities header, got %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"id": 1,
			"email": "ada@example.com",
			"name": "Ada",
			"credits": 10,
			"leaderboard_score": 10,
			"trust": 1.0,
			"created_at": "2026-04-14T00:00:00Z",
			"rank": 1,
			"pending_credits": 0
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	response, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe returned error: %v", err)
	}
	if response.Name != "Ada" {
		t.Fatalf("expected name Ada, got %q", response.Name)
	}
	if response.Email != "ada@example.com" {
		t.Fatalf("expected email ada@example.com, got %q", response.Email)
	}
}

func TestMeResponsePreferredNameFallsBackToPublicIdentity(t *testing.T) {
	t.Parallel()

	response := MeResponse{
		DisplayName: "Ada Lovelace",
		PublicLabel: "Ada Lovelace (abc234defg)",
		PublicID:    "abc234defg",
	}
	if got := response.PreferredName(); got != "Ada Lovelace" {
		t.Fatalf("expected display-name fallback, got %q", got)
	}

	response = MeResponse{
		PublicLabel: "Anonymous (abc234defg)",
		PublicID:    "abc234defg",
	}
	if got := response.PreferredName(); got != "Anonymous (abc234defg)" {
		t.Fatalf("expected public-label fallback, got %q", got)
	}

	response = MeResponse{
		PublicID: "abc234defg",
	}
	if got := response.PreferredName(); got != "abc234defg" {
		t.Fatalf("expected public-id fallback, got %q", got)
	}
}

func TestGetLeaderboardDecodesPublicLabelsAndViewer(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("X-API-Key"); got != "test-key" {
			t.Fatalf("unexpected api key header: %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"weekly": [
				{"id": 1, "public_id": "usr_1", "public_label": "Ada", "earned": 42}
			],
			"monthly": [
				{"id": 2, "name": "Legacy Name", "earned": 21}
			],
			"all_time": [],
			"viewer": {
				"weekly": {"id": 99, "public_id": "usr_me", "public_label": "Grace", "earned": 7, "rank": 27}
			}
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	response, err := client.GetLeaderboard(context.Background())
	if err != nil {
		t.Fatalf("GetLeaderboard returned error: %v", err)
	}

	if got := response.Weekly[0].PublicLabel; got != "Ada" {
		t.Fatalf("unexpected weekly public label: %q", got)
	}
	if got := response.Monthly[0].PublicLabel; got != "Legacy Name" {
		t.Fatalf("expected legacy name fallback, got %q", got)
	}
	if response.Viewer == nil || response.Viewer.Weekly == nil {
		t.Fatalf("expected weekly viewer entry, got %#v", response.Viewer)
	}
	if got := response.Viewer.Weekly.Rank; got != 27 {
		t.Fatalf("unexpected weekly viewer rank: %d", got)
	}
}
