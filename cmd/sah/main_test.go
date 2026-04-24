package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/corca-ai/sah-cli/internal/sah"
)

func newInvalidRefreshTokenServer(t *testing.T) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/oauth-authorization-server":
			_, _ = writer.Write([]byte(`{
				"issuer": "https://sah.example",
				"token_endpoint": "` + server.URL + `/oauth/token"
			}`))
		case "/oauth/token":
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{
				"error": "invalid_grant",
				"error_description": "Refresh token is invalid"
			}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	return server
}

func newAnonymousLeaderboardServer(
	t *testing.T,
	tokenEndpoint string,
	requests *int,
) *httptest.Server {
	t.Helper()

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		(*requests)++
		switch request.URL.Path {
		case "/.well-known/oauth-authorization-server":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(writer, `{
				"issuer": %q,
				"device_authorization_endpoint": %q,
				"token_endpoint": %q
			}`, server.URL, server.URL+"/oauth/device_authorization", tokenEndpoint)
		case "/s@h/leaderboard":
			if got := request.Header.Get("Authorization"); got != "" {
				t.Fatalf("expected anonymous fallback request, got %q", got)
			}
			if got := request.Header.Get("X-API-Key"); got != "" {
				t.Fatalf("expected anonymous fallback request, got api key %q", got)
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{
				"weekly": [
					{"id": 1, "public_id": "usr_1", "public_label": "Ada", "earned": 42}
				],
				"monthly": [],
				"all_time": []
			}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	return server
}

func TestPreferredLaunchdExecutableUsesStableHomebrewSymlink(t *testing.T) {
	dir := t.TempDir()
	cellarDir := filepath.Join(dir, "Cellar", "sah-cli", "0.2.3", "bin")
	if err := os.MkdirAll(cellarDir, 0o755); err != nil {
		t.Fatalf("mkdir cellar dir: %v", err)
	}

	cellarBinary := filepath.Join(cellarDir, "sah")
	if err := os.WriteFile(cellarBinary, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write cellar binary: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}

	stableBinary := filepath.Join(binDir, "sah")
	if err := os.Symlink(cellarBinary, stableBinary); err != nil {
		t.Fatalf("symlink stable binary: %v", err)
	}

	selected := selectLaunchdExecutable(cellarBinary, []string{stableBinary})
	if selected != stableBinary {
		t.Fatalf("expected stable symlink %q, got %q", stableBinary, selected)
	}
}

func TestPreferredLaunchdExecutableFallsBackToResolvedBinary(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "sah")
	if err := os.WriteFile(binary, []byte("binary"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	selected := selectLaunchdExecutable(binary, nil)
	expected, err := filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatalf("resolve binary symlink: %v", err)
	}
	if selected != expected {
		t.Fatalf("expected resolved binary %q, got %q", expected, selected)
	}
}

func testAgentBinaryPaths(t *testing.T, agents ...string) map[string]string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("PATH", dir)

	binaryPaths := make(map[string]string, len(agents))
	for _, agent := range agents {
		binaryPaths[agent] = writeTestExecutable(t, dir, agent)
	}
	return binaryPaths
}

func resolvedDaemonAgentOrder(t *testing.T, config sah.Config, binaryPaths map[string]string) string {
	t.Helper()

	pool, err := sah.ResolveAgentPool(config, sah.WorkerOptions{BinaryPaths: binaryPaths})
	if err != nil {
		t.Fatalf("ResolveAgentPool returned error: %v", err)
	}
	return joinAgentNames(pool)
}

func TestApplyDaemonInstallOptionsAutoRotatesInstalledAgentsByDefault(t *testing.T) {
	binaryPaths := testAgentBinaryPaths(t, "codex", "gemini")

	config := sah.DefaultConfig()
	if err := applyDaemonInstallOptions(&config, daemonInstallOptions{}, binaryPaths); err != nil {
		t.Fatalf("applyDaemonInstallOptions returned error: %v", err)
	}
	if !config.RotateInstalled {
		t.Fatal("expected daemon install to rotate installed agents by default")
	}
	if len(config.AgentPool) != 0 {
		t.Fatalf("expected daemon install to persist rotate-installed instead of an explicit pool, got %#v", config.AgentPool)
	}
	if got := resolvedDaemonAgentOrder(t, config, binaryPaths); got != "codex, gemini" {
		t.Fatalf("expected daemon order codex, gemini, got %q", got)
	}
}

func TestApplyDaemonInstallOptionsKeepsConfiguredPinnedAgent(t *testing.T) {
	binaryPaths := testAgentBinaryPaths(t, "codex", "claude")

	config := sah.DefaultConfig()
	config.DefaultAgent = "claude"

	if err := applyDaemonInstallOptions(&config, daemonInstallOptions{}, binaryPaths); err != nil {
		t.Fatalf("applyDaemonInstallOptions returned error: %v", err)
	}
	if config.RotateInstalled {
		t.Fatal("expected explicit configured agent to stay pinned")
	}
	if got := resolvedDaemonAgentOrder(t, config, binaryPaths); got != "claude" {
		t.Fatalf("expected daemon order claude, got %q", got)
	}
}

func TestApplyDaemonInstallOptionsKeepsGlobalModelPinnedConfig(t *testing.T) {
	binaryPaths := testAgentBinaryPaths(t, "codex", "gemini")

	config := sah.DefaultConfig()
	config.AgentModel = "gpt-5.4-mini"

	if err := applyDaemonInstallOptions(&config, daemonInstallOptions{}, binaryPaths); err != nil {
		t.Fatalf("applyDaemonInstallOptions returned error: %v", err)
	}
	if config.RotateInstalled {
		t.Fatal("expected global model override to keep the existing pinned agent config")
	}
	if got := resolvedDaemonAgentOrder(t, config, binaryPaths); got != "codex" {
		t.Fatalf("expected daemon order codex, got %q", got)
	}
}

func TestBuildRunWorkerSessionUsesConfiguredAgentPool(t *testing.T) {
	binaryPaths := testAgentBinaryPaths(t, "codex", "gemini")
	config := sah.DefaultConfig()
	config.AgentPool = []string{"gemini"}

	workerOptions, picker, err := buildRunWorkerSession(runSession{
		config:      config,
		binaryPaths: binaryPaths,
	}, runCommandOptions{})
	if err != nil {
		t.Fatalf("buildRunWorkerSession returned error: %v", err)
	}
	if workerOptions.Agent != "" {
		t.Fatalf("expected no explicit worker agent, got %q", workerOptions.Agent)
	}
	if got := joinAgentNames(picker.Pool()); got != "gemini" {
		t.Fatalf("expected configured agent pool gemini, got %q", got)
	}
}

func TestBuildRunWorkerSessionUsesConfiguredRotateInstalled(t *testing.T) {
	binaryPaths := testAgentBinaryPaths(t, "codex", "gemini")
	config := sah.DefaultConfig()
	config.RotateInstalled = true

	workerOptions, picker, err := buildRunWorkerSession(runSession{
		config:      config,
		binaryPaths: binaryPaths,
	}, runCommandOptions{})
	if err != nil {
		t.Fatalf("buildRunWorkerSession returned error: %v", err)
	}
	if workerOptions.Agent != "" {
		t.Fatalf("expected no explicit worker agent, got %q", workerOptions.Agent)
	}
	if got := joinAgentNames(picker.Pool()); got != "codex, gemini" {
		t.Fatalf("expected installed agent pool codex, gemini, got %q", got)
	}
}

func TestApplyDaemonInstallOptionsReturnsFriendlyErrorWhenNoAgentsDetected(t *testing.T) {
	t.Setenv("PATH", "")
	config := sah.DefaultConfig()

	err := applyDaemonInstallOptions(&config, daemonInstallOptions{}, nil)
	if err == nil {
		t.Fatal("expected daemon install to fail when no supported agent is detected")
	}
	if !sah.IsNoSupportedAgentCLI(err) {
		t.Fatalf("expected missing-agent error, got %v", err)
	}

	reported := daemonAgentSelectionError(err)
	for _, snippet := range []string{"cannot install daemon", "codex, gemini, claude, qwen", "sah agents"} {
		if !strings.Contains(reported.Error(), snippet) {
			t.Fatalf("expected error to contain %q, got %v", snippet, reported)
		}
	}
}

func TestLeaderboardDisplayEntriesKeepsTop15AndAppendsViewer(t *testing.T) {
	entries := make([]sah.LeaderboardEntry, 0, 20)
	for index := range 20 {
		entries = append(entries, sah.LeaderboardEntry{
			ID:          int64(index + 1),
			PublicLabel: "User " + strconv.Itoa(index+1),
			Earned:      200 - index,
		})
	}

	display := leaderboardDisplayEntries(entries, &sah.LeaderboardEntry{
		ID:          99,
		PublicLabel: "Grace",
		Earned:      17,
		Rank:        27,
	})
	if got := len(display); got != leaderboardVisibleRows+2 {
		t.Fatalf("expected 17 display rows, got %d", got)
	}
	if got := display[leaderboardVisibleRows-1].Rank; got != "15" {
		t.Fatalf("expected last visible rank 15, got %q", got)
	}
	if got := display[leaderboardVisibleRows].Rank; got != "..." {
		t.Fatalf("expected ellipsis row, got %q", got)
	}
	if got := display[leaderboardVisibleRows+1].Rank; got != "27" {
		t.Fatalf("expected viewer rank 27, got %q", got)
	}
	if got := display[leaderboardVisibleRows+1].Label; got != "Grace" {
		t.Fatalf("expected viewer label Grace, got %q", got)
	}
}

func TestLeaderboardDisplayEntriesDoesNotDuplicateViewerInTopRows(t *testing.T) {
	entries := []sah.LeaderboardEntry{
		{ID: 1, PublicLabel: "Ada", Earned: 50},
		{ID: 2, PublicLabel: "Grace", Earned: 40},
	}

	display := leaderboardDisplayEntries(entries, &sah.LeaderboardEntry{
		ID:          2,
		PublicLabel: "Grace",
		Earned:      40,
		Rank:        2,
	})
	if got := len(display); got != 2 {
		t.Fatalf("expected viewer not to be duplicated, got %d rows", got)
	}
}

func TestLeaderboardCmdUsesAPIKeyAndShowsViewerRank(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config")
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/s@h/leaderboard" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if got := request.Header.Get("X-API-Key"); got != "test-key" {
			t.Fatalf("unexpected api key header: %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"weekly": [
				{"id": 1, "public_id": "usr_1", "public_label": "Ada", "earned": 42},
				{"id": 2, "public_id": "usr_2", "public_label": "Linus", "earned": 35}
			],
			"monthly": [],
			"all_time": [],
			"viewer": {
				"weekly": {"id": 99, "public_id": "usr_me", "public_label": "Grace", "earned": 7, "rank": 27}
			}
		}`))
	}))
	defer server.Close()

	paths, err := sah.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths returned error: %v", err)
	}
	if err := sah.SaveConfig(paths, sah.Config{
		BaseURL:   server.URL,
		APIKey:    "test-key",
		AgentPool: nil,
	}); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	output := captureStdout(t, func() {
		if err := leaderboardCmd([]string{"--window", "weekly"}); err != nil {
			t.Fatalf("leaderboardCmd returned error: %v", err)
		}
	})

	for _, snippet := range []string{"Weekly", "Ada", "Linus", "Grace", "27", "..."} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected output to contain %q, got:\n%s", snippet, output)
		}
	}
}

func TestLeaderboardCmdFallsBackToPublicWhenStoredAPIKeyIsRejected(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config")
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Path != "/s@h/leaderboard" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if requests == 1 {
			if got := request.Header.Get("X-API-Key"); got != "stale-key" {
				t.Fatalf("expected stale api key on first request, got %q", got)
			}
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"detail":"Invalid API key"}`))
			return
		}
		if got := request.Header.Get("X-API-Key"); got != "" {
			t.Fatalf("expected anonymous fallback request, got %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"weekly": [
				{"id": 1, "public_id": "usr_1", "public_label": "Ada", "earned": 42}
			],
			"monthly": [],
			"all_time": []
		}`))
	}))
	defer server.Close()

	paths, err := sah.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths returned error: %v", err)
	}
	if err := sah.SaveConfig(paths, sah.Config{
		BaseURL:   server.URL,
		APIKey:    "stale-key",
		AgentPool: nil,
	}); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	output := captureStdout(t, func() {
		if err := leaderboardCmd([]string{"--window", "weekly"}); err != nil {
			t.Fatalf("leaderboardCmd returned error: %v", err)
		}
	})

	if requests != 2 {
		t.Fatalf("expected auth attempt plus anonymous fallback, got %d requests", requests)
	}
	if !strings.Contains(output, "Ada") {
		t.Fatalf("expected fallback output to contain leaderboard rows, got:\n%s", output)
	}
}

func TestLeaderboardCmdFallsBackToPublicWhenRefreshTokenIsRejected(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config")
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)

	requests := 0
	tokenServer := newInvalidRefreshTokenServer(t)
	defer tokenServer.Close()

	leaderboardServer := newAnonymousLeaderboardServer(t, tokenServer.URL+"/oauth/token", &requests)
	defer leaderboardServer.Close()

	paths, err := sah.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths returned error: %v", err)
	}
	if err := sah.SaveConfig(paths, sah.Config{
		BaseURL:       leaderboardServer.URL,
		AccessToken:   "expired-access-token",
		RefreshToken:  "refresh-token",
		TokenType:     "Bearer",
		TokenExpiry:   time.Now().Add(-time.Minute).Format(time.RFC3339),
		OAuthClientID: sah.DefaultOAuthClientID,
		AgentPool:     nil,
	}); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	output := captureStdout(t, func() {
		if err := leaderboardCmd([]string{"--window", "weekly"}); err != nil {
			t.Fatalf("leaderboardCmd returned error: %v", err)
		}
	})

	if requests != 2 {
		t.Fatalf("expected metadata fetch plus anonymous leaderboard request, got %d requests", requests)
	}
	if !strings.Contains(output, "Ada") {
		t.Fatalf("expected fallback output to contain leaderboard rows, got:\n%s", output)
	}
}

func TestMeCmdFallsBackToDisplayNameWhenLegacyNameIsMissing(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config")
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/s@h/me" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"id": 1,
			"email": "ada@example.com",
			"display_name": "Ada Lovelace",
			"public_label": "Ada Lovelace (abc234defg)",
			"public_id": "abc234defg",
			"credits": 10,
			"leaderboard_score": 10,
			"trust": 1.0,
			"created_at": "2026-04-14T00:00:00Z",
			"rank": 1,
			"pending_credits": 0
		}`))
	}))
	defer server.Close()

	paths, err := sah.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths returned error: %v", err)
	}
	if err := sah.SaveConfig(paths, sah.Config{
		BaseURL: server.URL,
		APIKey:  "test-key",
	}); err != nil {
		t.Fatalf("SaveConfig returned error: %v", err)
	}

	output := captureStdout(t, func() {
		if err := meCmd(nil); err != nil {
			t.Fatalf("meCmd returned error: %v", err)
		}
	})

	for _, snippet := range []string{"Name: Ada Lovelace", "Email: ada@example.com", "Rank: #1"} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected output to contain %q, got:\n%s", snippet, output)
		}
	}
}

func TestPrintResolvedAuthStatusTreatsInvalidRefreshTokenAsRejectedCredential(t *testing.T) {
	server := newInvalidRefreshTokenServer(t)
	defer server.Close()

	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".config")
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	paths, err := sah.ResolvePaths()
	if err != nil {
		t.Fatalf("ResolvePaths returned error: %v", err)
	}

	output := captureStdout(t, func() {
		err := printResolvedAuthStatus(
			context.Background(),
			paths,
			sah.Config{
				BaseURL:       server.URL,
				AccessToken:   "expired-access-token",
				RefreshToken:  "refresh-token",
				TokenType:     "Bearer",
				TokenExpiry:   "2000-01-01T00:00:00Z",
				OAuthClientID: sah.DefaultOAuthClientID,
			},
		)
		if err != nil {
			t.Fatalf("printResolvedAuthStatus returned error: %v", err)
		}
	})

	if !strings.Contains(output, "stored credential exists but was rejected by the server") {
		t.Fatalf("expected rejected credential status, got:\n%s", output)
	}
}

func captureStdout(t *testing.T, run func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
		_ = writer.Close()
	}()

	outputCh := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		outputCh <- string(data)
	}()

	run()

	_ = writer.Close()
	return <-outputCh
}

func writeTestExecutable(t *testing.T, dir string, name string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", name, err)
	}
	return path
}
