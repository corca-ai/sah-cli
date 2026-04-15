package sah

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	clientReleaseVersionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)
	cliVersion                  = "dev"
)

type ClientReleaseLink struct {
	Href string `json:"href"`
}

type ClientReleaseLinks struct {
	Upgrade      ClientReleaseLink `json:"upgrade,omitempty"`
	ReleaseNotes ClientReleaseLink `json:"release_notes,omitempty"`
}

type ClientReleaseResponse struct {
	LatestVersion               string             `json:"latest_version,omitempty"`
	RecommendedVersion          string             `json:"recommended_version,omitempty"`
	UpgradeCommand              string             `json:"upgrade_command,omitempty"`
	TaskProtocolVersion         string             `json:"task_protocol_version,omitempty"`
	RequiredTaskProtocolVersion string             `json:"required_task_protocol_version,omitempty"`
	RequiredClientCapabilities  []string           `json:"required_client_capabilities,omitempty"`
	Links                       ClientReleaseLinks `json:"_links,omitempty"`
}

type parsedReleaseVersion struct {
	Major int
	Minor int
	Patch int
}

type ClientReleaseStatus struct {
	CurrentVersion     string
	LatestVersion      string
	RecommendedVersion string
	UpgradeCommand     string
	UpgradeURL         string
	ReleaseNotesURL    string
	UpdateAvailable    bool
	Comparable         bool
	DevelopmentBuild   bool
}

func SetCLIVersion(raw string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		cliVersion = "dev"
		return
	}
	cliVersion = trimmed
}

func CLIVersion() string {
	return strings.TrimSpace(cliVersion)
}

func NormalizeReleaseVersion(raw string) string {
	parsed, ok := parseReleaseVersion(raw)
	if !ok {
		return ""
	}
	return fmt.Sprintf("v%d.%d.%d", parsed.Major, parsed.Minor, parsed.Patch)
}

func CompareReleaseVersions(left string, right string) (int, bool) {
	a, ok := parseReleaseVersion(left)
	if !ok {
		return 0, false
	}
	b, ok := parseReleaseVersion(right)
	if !ok {
		return 0, false
	}

	switch {
	case a.Major != b.Major:
		return compareInts(a.Major, b.Major), true
	case a.Minor != b.Minor:
		return compareInts(a.Minor, b.Minor), true
	default:
		return compareInts(a.Patch, b.Patch), true
	}
}

func ResolveClientReleaseStatus(release *ClientReleaseResponse) *ClientReleaseStatus {
	current := CLIVersion()
	status := &ClientReleaseStatus{
		CurrentVersion:   current,
		DevelopmentBuild: isDevelopmentBuildVersion(current),
	}
	if release == nil {
		return status
	}

	normalized := normalizeClientRelease(*release)
	status.LatestVersion = normalized.LatestVersion
	status.RecommendedVersion = normalized.RecommendedVersion
	status.UpgradeCommand = normalized.UpgradeCommand
	status.UpgradeURL = strings.TrimSpace(normalized.Links.Upgrade.Href)
	status.ReleaseNotesURL = strings.TrimSpace(normalized.Links.ReleaseNotes.Href)

	if status.DevelopmentBuild {
		return status
	}

	status.Comparable = NormalizeReleaseVersion(current) != ""
	if !status.Comparable {
		return status
	}

	recommended := status.RecommendedVersion
	if recommended == "" {
		recommended = status.LatestVersion
	}
	if recommended != "" {
		if cmp, ok := CompareReleaseVersions(current, recommended); ok && cmp < 0 {
			status.UpdateAvailable = true
		}
	}
	return status
}

func normalizeClientRelease(release ClientReleaseResponse) ClientReleaseResponse {
	release.LatestVersion = NormalizeReleaseVersion(release.LatestVersion)
	release.RecommendedVersion = NormalizeReleaseVersion(release.RecommendedVersion)
	release.UpgradeCommand = strings.TrimSpace(release.UpgradeCommand)
	release.TaskProtocolVersion = strings.TrimSpace(release.TaskProtocolVersion)
	release.RequiredTaskProtocolVersion = strings.TrimSpace(release.RequiredTaskProtocolVersion)
	release.RequiredClientCapabilities = normalizeCapabilities(release.RequiredClientCapabilities)
	release.Links.Upgrade.Href = strings.TrimSpace(release.Links.Upgrade.Href)
	release.Links.ReleaseNotes.Href = strings.TrimSpace(release.Links.ReleaseNotes.Href)
	if release.RecommendedVersion == "" {
		release.RecommendedVersion = release.LatestVersion
	}
	if release.LatestVersion == "" {
		release.LatestVersion = release.RecommendedVersion
	}
	if release.RequiredTaskProtocolVersion == "" {
		release.RequiredTaskProtocolVersion = release.TaskProtocolVersion
	}
	return release
}

func parseReleaseVersion(raw string) (parsedReleaseVersion, bool) {
	matches := clientReleaseVersionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if matches == nil {
		return parsedReleaseVersion{}, false
	}
	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return parsedReleaseVersion{}, false
	}
	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return parsedReleaseVersion{}, false
	}
	patch, err := strconv.Atoi(matches[3])
	if err != nil {
		return parsedReleaseVersion{}, false
	}
	return parsedReleaseVersion{Major: major, Minor: minor, Patch: patch}, true
}

func compareInts(left int, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func isDevelopmentBuildVersion(raw string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	return trimmed == "" || trimmed == "dev"
}
