package sah

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func writeTestMeResponse(writer http.ResponseWriter) {
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
}

func assertRequestOrigin(t *testing.T, request *http.Request, want string) {
	t.Helper()
	if got := request.Header.Get("Origin"); got != want {
		t.Fatalf("unexpected origin header: %q", got)
	}
}

func newOAuthDeviceFlowTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_, _ = fmt.Fprintf(writer, `{
				"issuer": "https://sah.example",
				"device_authorization_endpoint": %q,
				"token_endpoint": %q
			}`, server.URL+"/oauth/device_authorization", server.URL+"/oauth/token")
		case "/oauth/device_authorization":
			if request.Method != http.MethodPost {
				t.Fatalf("unexpected method: %s", request.Method)
			}
			assertRequestOrigin(t, request, server.URL)
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := request.PostForm.Get("client_id"); got != DefaultOAuthClientID {
				t.Fatalf("unexpected client id: %q", got)
			}
			if got := request.PostForm.Get("scope"); got != "user:api offline_access" {
				t.Fatalf("unexpected scope: %q", got)
			}
			_, _ = writer.Write([]byte(`{
				"device_code": "device-code",
				"user_code": "ABCD-EFGH",
				"verification_uri": "https://sah.example/oauth/device",
				"verification_uri_complete": "https://sah.example/oauth/device?user_code=ABCD-EFGH",
				"expires_in": 900,
				"interval": 5
			}`))
		case "/oauth/token":
			assertRequestOrigin(t, request, server.URL)
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := request.PostForm.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:device_code" {
				t.Fatalf("unexpected grant type: %q", got)
			}
			if got := request.PostForm.Get("device_code"); got != "device-code" {
				t.Fatalf("unexpected device code: %q", got)
			}
			_, _ = writer.Write([]byte(`{
				"access_token": "access-token",
				"token_type": "Bearer",
				"expires_in": 3600,
				"refresh_token": "refresh-token",
				"scope": "user:api offline_access"
			}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	return server
}

func newOAuthRefreshTestServer(t *testing.T, meCalls *atomic.Int32) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_, _ = fmt.Fprintf(writer, `{
				"issuer": %q,
				"device_authorization_endpoint": %q,
				"token_endpoint": %q
			}`, server.URL, server.URL+"/oauth/device_authorization", server.URL+"/oauth/token")
		case "/oauth/token":
			assertRequestOrigin(t, request, server.URL)
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := request.PostForm.Get("grant_type"); got != "refresh_token" {
				t.Fatalf("unexpected grant type: %q", got)
			}
			if got := request.PostForm.Get("refresh_token"); got != "refresh-token" {
				t.Fatalf("unexpected refresh token: %q", got)
			}
			_, _ = writer.Write([]byte(`{
				"access_token": "fresh-access-token",
				"token_type": "Bearer",
				"expires_in": 3600,
				"refresh_token": "fresh-refresh-token"
			}`))
		case "/s@h/me":
			meCalls.Add(1)
			if got := request.Header.Get("Authorization"); got != "Bearer fresh-access-token" {
				t.Fatalf("unexpected authorization header: %q", got)
			}
			writeTestMeResponse(writer)
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	return server
}

func newCachedOAuthMetadataTestServer(t *testing.T, metadataCalls *atomic.Int32) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/oauth-authorization-server":
			metadataCalls.Add(1)
			writer.Header().Set("Cache-Control", "public, max-age=60")
			_, _ = fmt.Fprintf(writer, `{
				"issuer": %q,
				"device_authorization_endpoint": %q,
				"token_endpoint": %q
			}`, server.URL, server.URL+"/oauth/device_authorization", server.URL+"/oauth/token")
		case "/oauth/device_authorization":
			assertRequestOrigin(t, request, server.URL)
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			_, _ = writer.Write([]byte(`{
				"device_code": "cached-device-code",
				"user_code": "ABCD-EFGH",
				"verification_uri": "https://sah.example/oauth/device",
				"verification_uri_complete": "https://sah.example/oauth/device?user_code=ABCD-EFGH",
				"expires_in": 900,
				"interval": 5
			}`))
		case "/oauth/token":
			assertRequestOrigin(t, request, server.URL)
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			_, _ = writer.Write([]byte(`{
				"access_token": "access-token",
				"token_type": "Bearer",
				"expires_in": 3600,
				"refresh_token": "refresh-token",
				"scope": "user:api offline_access"
			}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	return server
}

func newRedirectingOAuthDeviceFlowTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_, _ = fmt.Fprintf(writer, `{
				"issuer": %q,
				"device_authorization_endpoint": %q,
				"token_endpoint": %q
			}`, server.URL, server.URL+"/oauth/device_authorization", server.URL+"/oauth/token")
		case "/oauth/device_authorization":
			http.Redirect(writer, request, "/oauth/device_authorization/canonical", http.StatusFound)
		case "/oauth/device_authorization/canonical":
			if request.Method != http.MethodPost {
				t.Fatalf("expected redirected device authorization to stay POST, got %s", request.Method)
			}
			assertRequestOrigin(t, request, server.URL)
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := request.PostForm.Get("client_id"); got != DefaultOAuthClientID {
				t.Fatalf("unexpected client id: %q", got)
			}
			_, _ = writer.Write([]byte(`{
				"device_code": "redirected-device-code",
				"user_code": "ABCD-EFGH",
				"verification_uri": "https://sah.example/oauth/device",
				"verification_uri_complete": "https://sah.example/oauth/device?user_code=ABCD-EFGH",
				"expires_in": 900,
				"interval": 5
			}`))
		case "/oauth/token":
			http.Redirect(writer, request, "/oauth/token/canonical", http.StatusSeeOther)
		case "/oauth/token/canonical":
			if request.Method != http.MethodPost {
				t.Fatalf("expected redirected token request to stay POST, got %s", request.Method)
			}
			assertRequestOrigin(t, request, server.URL)
			if err := request.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := request.PostForm.Get("device_code"); got != "redirected-device-code" {
				t.Fatalf("unexpected device code: %q", got)
			}
			_, _ = writer.Write([]byte(`{
				"access_token": "access-token",
				"token_type": "Bearer",
				"expires_in": 3600,
				"refresh_token": "refresh-token",
				"scope": "user:api offline_access"
			}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	return server
}

func newAssignmentResponseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		if request.URL.Path != "/s@h/assignments" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
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
			`</s@h/assignments/41/submission>; rel="submit", </s@h/assignments/41>; rel="release"`,
		)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(body))
	}))
}

func TestGetTaskMergesAssignmentLinksFromHeader(t *testing.T) {
	t.Parallel()

	server := newAssignmentResponseServer(t, `{
			"assignment_id": 41,
			"task_type": "novel-task",
			"task_key": "novel-task/v1",
			"payload": {"title": "Paper"},
			"instruction_version": "2026-04-10",
			"schema_version": "2026-04-09",
			"agent_request": {
				"title": "Novel task assignment",
				"description": "Return one JSON object.",
				"prompt": "server-owned prompt",
				"response_schema": {"type": "object"}
			},
			"instructions": {
				"summary": "Do the assignment.",
				"rules": [],
				"good_examples": [],
				"bad_patterns": [],
				"submission_schema": {"type": "object"},
				"stop_conditions": []
			}
		}`)
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	assignment, err := client.GetTask(context.Background(), "")
	if err != nil {
		t.Fatalf("GetTask returned error: %v", err)
	}

	if assignment.Links.Submit.Href != "/s@h/assignments/41/submission" {
		t.Fatalf("unexpected submit href: %q", assignment.Links.Submit.Href)
	}
	if assignment.Links.Release.Href != "/s@h/assignments/41" {
		t.Fatalf("unexpected release href: %q", assignment.Links.Release.Href)
	}
	if assignment.AgentRequest == nil || assignment.AgentRequest.Prompt != "server-owned prompt" {
		t.Fatalf("expected server-owned agent request, got %#v", assignment.AgentRequest)
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
		if request.URL.Path != "/s@h/assignments/41" {
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
					Href:   "/s@h/assignments/41",
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
		if request.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", request.Method)
		}
		if request.URL.Path != "/s@h/assignments" {
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

func TestGetServiceDocumentDecodesActions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/s@h" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"title": "SCIENCE@home CLI",
			"description": "Hypermedia entrypoint.",
			"actions": [
				{"command": "me", "method": "GET", "href": "/s@h/me", "title": "My account", "description": "View your account."},
				{"command": "auth login", "title": "Sign in", "description": "Pair this machine."}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	document, err := client.GetServiceDocument(context.Background())
	if err != nil {
		t.Fatalf("GetServiceDocument returned error: %v", err)
	}
	if got := document.Actions[0].Href; got != "/s@h/me" {
		t.Fatalf("unexpected href: %q", got)
	}
	if got := document.Actions[1].Command; got != "auth login" {
		t.Fatalf("unexpected command: %q", got)
	}
}

func TestGetNavigationPostsClientState(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/s@h/navigation" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}

		var body NavigationRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if !body.AuthConfigured || body.CurrentCommand != "me" {
			t.Fatalf("unexpected navigation body: %#v", body)
		}

		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"title": "Try next",
			"description": "Recommended next commands.",
			"actions": [
				{"command": "contributions", "title": "Recent contributions", "description": "See your recent work."}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key")
	response, err := client.GetNavigation(context.Background(), NavigationRequest{
		AuthConfigured: true,
		DetectedAgents: []string{"codex"},
		CurrentCommand: "me",
	})
	if err != nil {
		t.Fatalf("GetNavigation returned error: %v", err)
	}
	if got := response.Actions[0].Command; got != "contributions" {
		t.Fatalf("unexpected command: %q", got)
	}
}

func TestGetServiceDocumentRetriesOnceOnRetryAfter(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempt := attempts.Add(1)
		if request.Method != http.MethodGet || request.URL.Path != "/s@h" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if attempt == 1 {
			writer.Header().Set("Retry-After", "0")
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"title": "SCIENCE@home CLI",
			"description": "Hypermedia entrypoint.",
			"actions": []
		}`))
	}))
	defer server.Close()

	document, err := NewClient(server.URL, "").GetServiceDocument(context.Background())
	if err != nil {
		t.Fatalf("GetServiceDocument returned error: %v", err)
	}
	if document.Title != "SCIENCE@home CLI" {
		t.Fatalf("unexpected document: %#v", document)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("expected one retry, got %d attempts", got)
	}
}

func TestClaimAssignmentDoesNotRetryAfterOnPost(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts.Add(1)
		if request.Method != http.MethodPost || request.URL.Path != "/s@h/assignments" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		writer.Header().Set("Retry-After", "0")
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	assignment, err := NewClient(server.URL, "test-key").ClaimAssignment(context.Background(), "")
	if !IsStatus(err, http.StatusServiceUnavailable) {
		t.Fatalf("expected 503 status error, got assignment=%#v err=%v", assignment, err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected POST not to retry, got %d attempts", got)
	}
}

func TestGetServiceDocumentFollowsRedirect(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/s@h":
			http.Redirect(writer, request, "/s@h/service", http.StatusTemporaryRedirect)
		case "/s@h/service":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{
				"title": "SCIENCE@home CLI",
				"description": "Redirect target.",
				"actions": []
			}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	document, err := NewClient(server.URL, "").GetServiceDocument(context.Background())
	if err != nil {
		t.Fatalf("GetServiceDocument returned error: %v", err)
	}
	if got := document.Description; got != "Redirect target." {
		t.Fatalf("unexpected redirected document: %#v", document)
	}
}

func TestDeviceAuthorizationEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/cli/device-authorizations":
			_, _ = writer.Write([]byte(`{
				"title": "Sign in",
				"description": "Open the verification page and enter the code below.",
				"verification_url": "https://sah.example/cli",
				"user_code": "ABCD-EFGH",
				"device_code": "dev_token",
				"expires_in": 900,
				"interval": 5
			}`))
		case "/api/cli/device-token":
			_, _ = writer.Write([]byte(`{
				"status": "approved",
				"user_id": 7,
				"api_key": "sah_live_test"
			}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	start, err := StartDeviceAuthorization(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("StartDeviceAuthorization returned error: %v", err)
	}
	if got := start.DeviceCode; got != "dev_token" {
		t.Fatalf("unexpected device code: %q", got)
	}

	poll, err := PollDeviceAuthorization(context.Background(), server.URL, start.DeviceCode)
	if err != nil {
		t.Fatalf("PollDeviceAuthorization returned error: %v", err)
	}
	if poll.Status != "approved" || poll.APIKey != "sah_live_test" {
		t.Fatalf("unexpected device token response: %#v", poll)
	}
}

func TestOAuthDeviceAuthorizationFlow(t *testing.T) {
	t.Parallel()

	server := newOAuthDeviceFlowTestServer(t)
	defer server.Close()

	metadata, err := GetOAuthAuthorizationServerMetadata(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("GetOAuthAuthorizationServerMetadata returned error: %v", err)
	}
	if got := metadata.TokenEndpoint; got != server.URL+"/oauth/token" {
		t.Fatalf("unexpected token endpoint: %q", got)
	}

	start, err := StartOAuthDeviceAuthorization(
		context.Background(),
		server.URL,
		DefaultOAuthClientID,
		"user:api offline_access",
	)
	if err != nil {
		t.Fatalf("StartOAuthDeviceAuthorization returned error: %v", err)
	}
	if got := start.VerificationURIComplete; got == "" {
		t.Fatal("expected verification_uri_complete")
	}

	token, err := PollOAuthDeviceToken(context.Background(), server.URL, DefaultOAuthClientID, start.DeviceCode)
	if err != nil {
		t.Fatalf("PollOAuthDeviceToken returned error: %v", err)
	}
	if token.AccessToken != "access-token" || token.RefreshToken != "refresh-token" {
		t.Fatalf("unexpected oauth token response: %#v", token)
	}
}

func TestOAuthDeviceAuthorizationMetadataUsesHTTPCache(t *testing.T) {
	t.Parallel()

	var metadataCalls atomic.Int32
	server := newCachedOAuthMetadataTestServer(t, &metadataCalls)
	defer server.Close()

	paths := resolvePaths("darwin", t.TempDir(), "/Users/tester", func(string) string { return "" })
	start, err := StartOAuthDeviceAuthorizationWithPaths(
		context.Background(),
		paths,
		server.URL,
		DefaultOAuthClientID,
		"user:api offline_access",
	)
	if err != nil {
		t.Fatalf("StartOAuthDeviceAuthorizationWithPaths returned error: %v", err)
	}
	if _, err := PollOAuthDeviceTokenWithPaths(
		context.Background(),
		paths,
		server.URL,
		DefaultOAuthClientID,
		start.DeviceCode,
	); err != nil {
		t.Fatalf("PollOAuthDeviceTokenWithPaths returned error: %v", err)
	}
	if got := metadataCalls.Load(); got != 1 {
		t.Fatalf("expected one metadata request after caching, got %d", got)
	}
}

func TestOAuthDeviceAuthorizationFlowPreservesPOSTAcrossRedirects(t *testing.T) {
	t.Parallel()

	server := newRedirectingOAuthDeviceFlowTestServer(t)
	defer server.Close()

	start, err := StartOAuthDeviceAuthorization(
		context.Background(),
		server.URL,
		DefaultOAuthClientID,
		"user:api offline_access",
	)
	if err != nil {
		t.Fatalf("StartOAuthDeviceAuthorization returned error: %v", err)
	}
	if _, err := PollOAuthDeviceToken(
		context.Background(),
		server.URL,
		DefaultOAuthClientID,
		start.DeviceCode,
	); err != nil {
		t.Fatalf("PollOAuthDeviceToken returned error: %v", err)
	}
}

func TestShouldPreserveSensitiveHeadersOnRedirect(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		from string
		to   string
		want bool
	}{
		{name: "same host", from: "https://sah.example/s@h", to: "https://sah.example/s@h/service", want: true},
		{name: "http to https upgrade", from: "http://sah.example/s@h", to: "https://sah.example/s@h", want: true},
		{name: "www alias", from: "https://www.sah.example/s@h", to: "https://sah.example/s@h", want: true},
		{name: "https downgrade", from: "https://sah.example/s@h", to: "http://sah.example/s@h", want: false},
		{name: "different host", from: "https://sah.example/s@h", to: "https://api.example/s@h", want: false},
		{name: "different non-default port", from: "https://sah.example:8443/s@h", to: "https://sah.example:9443/s@h", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fromURL, err := url.Parse(testCase.from)
			if err != nil {
				t.Fatalf("parse from url: %v", err)
			}
			toURL, err := url.Parse(testCase.to)
			if err != nil {
				t.Fatalf("parse to url: %v", err)
			}
			if got := shouldPreserveSensitiveHeadersOnRedirect(fromURL, toURL); got != testCase.want {
				t.Fatalf("expected %t, got %t", testCase.want, got)
			}
		})
	}
}

func TestClientStripsAPIKeyOnUntrustedRedirect(t *testing.T) {
	t.Parallel()

	redirectTarget := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("expected redirected request to strip api key, got %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"title":"SCIENCE@home CLI","description":"ok","actions":[]}`))
	}))
	defer redirectTarget.Close()

	redirectSource := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, redirectTarget.URL+"/s@h/service", http.StatusTemporaryRedirect)
	}))
	defer redirectSource.Close()

	document, err := NewClient(redirectSource.URL, "test-key").GetServiceDocument(context.Background())
	if err != nil {
		t.Fatalf("GetServiceDocument returned error: %v", err)
	}
	if document.Description != "ok" {
		t.Fatalf("unexpected document: %#v", document)
	}
}

func TestClientStripsAuthorizationOnUntrustedRedirect(t *testing.T) {
	t.Parallel()

	redirectTarget := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected redirected request to strip authorization, got %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"title":"SCIENCE@home CLI","description":"ok","actions":[]}`))
	}))
	defer redirectTarget.Close()

	redirectSource := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, redirectTarget.URL+"/s@h/service", http.StatusTemporaryRedirect)
	}))
	defer redirectSource.Close()

	client := NewConfigClient(Paths{}, &Config{
		BaseURL:     redirectSource.URL,
		AccessToken: "access-token",
		TokenType:   "Bearer",
	})
	document, err := client.GetServiceDocument(context.Background())
	if err != nil {
		t.Fatalf("GetServiceDocument returned error: %v", err)
	}
	if document.Description != "ok" {
		t.Fatalf("unexpected document: %#v", document)
	}
}

func TestConfigClientPrefersBearerAuthorization(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}
		if got := request.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("did not expect api key header, got %q", got)
		}
		writeTestMeResponse(writer)
	}))
	defer server.Close()

	client := NewConfigClient(Paths{}, &Config{
		BaseURL:     server.URL,
		AccessToken: "access-token",
		TokenType:   "Bearer",
		APIKey:      "legacy-api-key",
	})
	if _, err := client.GetMe(context.Background()); err != nil {
		t.Fatalf("GetMe returned error: %v", err)
	}
}

func TestConfigClientRefreshesExpiredBearerToken(t *testing.T) {
	t.Parallel()

	var meCalls atomic.Int32
	server := newOAuthRefreshTestServer(t, &meCalls)
	defer server.Close()

	paths := resolvePaths("darwin", t.TempDir(), "/Users/tester", func(string) string { return "" })
	config := Config{
		BaseURL:       server.URL,
		AccessToken:   "expired-access-token",
		RefreshToken:  "refresh-token",
		TokenType:     "Bearer",
		TokenExpiry:   time.Now().Add(-time.Minute).Format(time.RFC3339),
		OAuthClientID: DefaultOAuthClientID,
	}
	client := NewConfigClient(paths, &config)
	if _, err := client.GetMe(context.Background()); err != nil {
		t.Fatalf("GetMe returned error: %v", err)
	}
	if got := meCalls.Load(); got != 1 {
		t.Fatalf("expected one me request, got %d", got)
	}
	if config.AccessToken != "fresh-access-token" || config.RefreshToken != "fresh-refresh-token" {
		t.Fatalf("expected refreshed tokens to persist, got %#v", config)
	}
}

func TestConfigClientFallsBackToAPIKeyWhenRefreshTokenIsRejected(t *testing.T) {
	t.Parallel()

	var meCalls atomic.Int32
	var tokenCalls atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_, _ = fmt.Fprintf(writer, `{
				"issuer": %q,
				"device_authorization_endpoint": %q,
				"token_endpoint": %q
			}`, server.URL, server.URL+"/oauth/device_authorization", server.URL+"/oauth/token")
		case "/oauth/token":
			tokenCalls.Add(1)
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":"invalid_grant","error_description":"Refresh token is invalid"}`))
		case "/s@h/me":
			meCalls.Add(1)
			if got := request.Header.Get("Authorization"); got != "" {
				t.Fatalf("expected authorization header to be cleared after refresh failure, got %q", got)
			}
			if got := request.Header.Get("X-API-Key"); got != "legacy-api-key" {
				t.Fatalf("expected api key fallback, got %q", got)
			}
			writeTestMeResponse(writer)
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	paths := resolvePaths("darwin", t.TempDir(), "/Users/tester", func(string) string { return "" })
	config := Config{
		BaseURL:       server.URL,
		APIKey:        "legacy-api-key",
		AccessToken:   "expired-access-token",
		RefreshToken:  "refresh-token",
		TokenType:     "Bearer",
		TokenExpiry:   time.Now().Add(-time.Minute).Format(time.RFC3339),
		OAuthClientID: DefaultOAuthClientID,
	}
	client := NewConfigClient(paths, &config)
	if _, err := client.GetMe(context.Background()); err != nil {
		t.Fatalf("GetMe returned error: %v", err)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected one refresh attempt, got %d", got)
	}
	if got := meCalls.Load(); got != 1 {
		t.Fatalf("expected one me request, got %d", got)
	}
	if config.AccessToken != "" || config.RefreshToken != "" || config.TokenType != "" || config.TokenExpiry != "" {
		t.Fatalf("expected rejected oauth tokens to be cleared, got %#v", config)
	}
}

func TestConfigClientFallsBackToAPIKeyWhenStoredBearerTokenIsRejectedWithoutRefreshToken(t *testing.T) {
	t.Parallel()

	var meCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path != "/s@h/me" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}

		call := meCalls.Add(1)
		if call == 1 {
			if got := request.Header.Get("Authorization"); got != "Bearer stale-access-token" {
				t.Fatalf("expected stale bearer token on first request, got %q", got)
			}
			if got := request.Header.Get("X-API-Key"); got != "" {
				t.Fatalf("did not expect api key on first request, got %q", got)
			}
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"detail":"Expired service token"}`))
			return
		}

		if got := request.Header.Get("Authorization"); got != "" {
			t.Fatalf("expected authorization header to be cleared after bearer rejection, got %q", got)
		}
		if got := request.Header.Get("X-API-Key"); got != "legacy-api-key" {
			t.Fatalf("expected api key fallback, got %q", got)
		}
		writeTestMeResponse(writer)
	}))
	defer server.Close()

	paths := resolvePaths("darwin", t.TempDir(), "/Users/tester", func(string) string { return "" })
	config := Config{
		BaseURL:     server.URL,
		APIKey:      "legacy-api-key",
		AccessToken: "stale-access-token",
		TokenType:   "Bearer",
		TokenExpiry: time.Now().Add(-time.Minute).Format(time.RFC3339),
	}
	client := NewConfigClient(paths, &config)
	if _, err := client.GetMe(context.Background()); err != nil {
		t.Fatalf("GetMe returned error: %v", err)
	}
	if got := meCalls.Load(); got != 2 {
		t.Fatalf("expected bearer attempt plus api key fallback, got %d requests", got)
	}
	if config.AccessToken != "" || config.RefreshToken != "" || config.TokenType != "" || config.TokenExpiry != "" {
		t.Fatalf("expected rejected bearer tokens to be cleared, got %#v", config)
	}
}
