package sah

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeReleaseVersion(t *testing.T) {
	if got := NormalizeReleaseVersion("0.6.2"); got != "v0.6.2" {
		t.Fatalf("unexpected normalized version: %q", got)
	}
	if got := NormalizeReleaseVersion("v0.6.2"); got != "v0.6.2" {
		t.Fatalf("unexpected normalized version: %q", got)
	}
	if got := NormalizeReleaseVersion("dev"); got != "" {
		t.Fatalf("expected dev version to be ignored, got %q", got)
	}
}

func TestCompareReleaseVersions(t *testing.T) {
	if cmp, ok := CompareReleaseVersions("v0.6.1", "v0.6.2"); !ok || cmp >= 0 {
		t.Fatalf("expected v0.6.1 < v0.6.2, got cmp=%d ok=%v", cmp, ok)
	}
	if cmp, ok := CompareReleaseVersions("v0.6.2", "v0.6.2"); !ok || cmp != 0 {
		t.Fatalf("expected equal versions, got cmp=%d ok=%v", cmp, ok)
	}
}

func TestResolveClientReleaseStatusDetectsAvailableUpdate(t *testing.T) {
	SetCLIVersion("v0.6.0")
	t.Cleanup(func() {
		SetCLIVersion("dev")
	})

	status := ResolveClientReleaseStatus(&ClientReleaseResponse{
		LatestVersion:      "v0.6.2",
		RecommendedVersion: "v0.6.2",
		UpgradeCommand:     "brew upgrade corca-ai/tap/sah-cli",
	})
	if !status.UpdateAvailable {
		t.Fatal("expected update to be available")
	}
}

func TestResolveClientReleaseStatusFallsBackToLatestVersion(t *testing.T) {
	SetCLIVersion("v0.6.0")
	t.Cleanup(func() {
		SetCLIVersion("dev")
	})

	status := ResolveClientReleaseStatus(&ClientReleaseResponse{
		LatestVersion: "v0.6.2",
	})
	if !status.UpdateAvailable {
		t.Fatalf("expected update to be available when only latest_version is present, got %#v", status)
	}
	if status.RecommendedVersion != "v0.6.2" {
		t.Fatalf("expected recommended version to normalize to latest, got %#v", status)
	}
}

func TestResolveClientReleaseStatusSkipsDevelopmentBuilds(t *testing.T) {
	SetCLIVersion("dev")

	status := ResolveClientReleaseStatus(&ClientReleaseResponse{
		LatestVersion: "v0.6.2",
	})
	if status.UpdateAvailable {
		t.Fatalf("expected dev builds to skip release gating, got %#v", status)
	}
}

func TestResolveWorkerContractViolationDetectsProtocolMismatchAndMissingCapabilities(t *testing.T) {
	violation := ResolveWorkerContractViolation(&ClientReleaseResponse{
		TaskProtocolVersion:         "2026-04-12",
		RequiredTaskProtocolVersion: "2026-04-12",
		RequiredClientCapabilities:  []string{"assignment-links", "future-capability"},
	})
	if violation == nil {
		t.Fatal("expected worker contract violation")
	}
	if violation.RequiredTaskProtocolVersion != "2026-04-12" {
		t.Fatalf("unexpected required protocol: %#v", violation)
	}
	if len(violation.MissingClientCapabilities) != 1 || violation.MissingClientCapabilities[0] != "future-capability" {
		t.Fatalf("unexpected missing capabilities: %#v", violation.MissingClientCapabilities)
	}
}

func TestClientGetClientRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/s@h/client-release" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"latest_version": "v0.6.2",
			"recommended_version": "v0.6.2",
			"task_protocol_version": "2026-04-11",
			"required_task_protocol_version": "2026-04-11",
			"required_client_capabilities": ["assignment-links"],
			"upgrade_command": "brew upgrade corca-ai/tap/sah-cli"
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "")
	release, err := client.GetClientRelease(context.Background())
	if err != nil {
		t.Fatalf("GetClientRelease returned error: %v", err)
	}
	if release.LatestVersion != "v0.6.2" {
		t.Fatalf("unexpected latest version: %#v", release)
	}
}
