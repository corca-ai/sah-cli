package sah

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DefaultClientReleaseCacheTTL = 24 * time.Hour

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

type cachedClientRelease struct {
	CheckedAt time.Time             `json:"checked_at"`
	Release   ClientReleaseResponse `json:"release"`
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

func LoadClientReleaseCache(paths Paths) (*cachedClientRelease, error) {
	data, err := os.ReadFile(paths.ReleaseCacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read client release cache: %w", err)
	}

	var cache cachedClientRelease
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("decode client release cache: %w", err)
	}
	return &cache, nil
}

func SaveClientReleaseCache(paths Paths, release ClientReleaseResponse, checkedAt time.Time) error {
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	cache := cachedClientRelease{
		CheckedAt: checkedAt.UTC(),
		Release:   normalizeClientRelease(release),
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("encode client release cache: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(paths.ReleaseCacheFile, data, 0o600); err != nil {
		return fmt.Errorf("write client release cache: %w", err)
	}
	return nil
}

func CachedClientRelease(paths Paths, ttl time.Duration) (*ClientReleaseResponse, bool, error) {
	cache, err := LoadClientReleaseCache(paths)
	if err != nil || cache == nil {
		return nil, false, err
	}
	if ttl <= 0 {
		ttl = DefaultClientReleaseCacheTTL
	}
	if time.Since(cache.CheckedAt) > ttl {
		return &cache.Release, false, nil
	}
	return &cache.Release, true, nil
}

func RefreshClientRelease(ctx context.Context, paths Paths, baseURL string) (*ClientReleaseResponse, error) {
	client := NewClient(baseURL, "")
	release, err := client.GetClientRelease(ctx)
	if err != nil {
		return nil, err
	}
	if err := SaveClientReleaseCache(paths, *release, time.Now()); err != nil {
		return nil, err
	}
	normalized := normalizeClientRelease(*release)
	return &normalized, nil
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
