package sah

import (
	"encoding/json"
	"strings"
	"time"
)

type Assignment struct {
	AssignmentID       int64                  `json:"assignment_id"`
	TaskType           string                 `json:"task_type"`
	TaskKey            string                 `json:"task_key"`
	Payload            map[string]any         `json:"payload"`
	ProtocolVersion    string                 `json:"protocol_version,omitempty"`
	InstructionVersion string                 `json:"instruction_version"`
	SchemaVersion      string                 `json:"schema_version"`
	Instructions       AssignmentInstructions `json:"instructions"`
	Links              AssignmentLinks        `json:"_links,omitempty"`
}

type AssignmentLink struct {
	Href        string `json:"href"`
	Method      string `json:"method,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type AssignmentLinks struct {
	Self    AssignmentLink `json:"self"`
	Submit  AssignmentLink `json:"submit"`
	Release AssignmentLink `json:"release"`
}

type AssignmentInstructions struct {
	Summary          string   `json:"summary"`
	Rules            []string `json:"rules"`
	GoodExamples     []any    `json:"good_examples"`
	BadPatterns      []string `json:"bad_patterns"`
	SubmissionSchema any      `json:"submission_schema"`
	StopConditions   []string `json:"stop_conditions"`
}

type SubmitContributionRequest struct {
	AssignmentID int64          `json:"assignment_id"`
	TaskType     string         `json:"task_type"`
	Payload      map[string]any `json:"payload"`
}

type SubmitContributionResponse struct {
	AssignmentID   int64 `json:"assignment_id"`
	ContributionID int64 `json:"contribution_id"`
	CreditsEarned  int   `json:"credits_earned"`
	PendingCredits int   `json:"pending_credits"`
}

type MeResponse struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	// Legacy field returned by older /s@h/me responses before public identity
	// fields were added for anonymous-safe display.
	Name             string    `json:"name"`
	PublicID         string    `json:"public_id,omitempty"`
	DisplayName      string    `json:"display_name,omitempty"`
	PublicLabel      string    `json:"public_label,omitempty"`
	Credits          int       `json:"credits"`
	LeaderboardScore int       `json:"leaderboard_score"`
	Trust            float64   `json:"trust"`
	CreatedAt        time.Time `json:"created_at"`
	Rank             int       `json:"rank"`
	PendingCredits   int       `json:"pending_credits"`
}

func (response MeResponse) PreferredName() string {
	if name := strings.TrimSpace(response.Name); name != "" {
		// Keep the legacy field first so older deployed servers still render a
		// usable name until they expose display_name/public_label consistently.
		return name
	}
	if displayName := strings.TrimSpace(response.DisplayName); displayName != "" {
		return displayName
	}
	if publicLabel := strings.TrimSpace(response.PublicLabel); publicLabel != "" {
		return publicLabel
	}
	return strings.TrimSpace(response.PublicID)
}

type HistoryEntry struct {
	ID           int64     `json:"id"`
	Kind         string    `json:"kind"`
	TaskType     string    `json:"task_type"`
	Status       string    `json:"status"`
	StatusLabel  string    `json:"status_label"`
	Subject      string    `json:"subject"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `json:"created_at"`
	CreditAmount int       `json:"credit_amount"`
	CreditState  string    `json:"credit_state"`
}

type ContributionsResponse struct {
	Submissions []HistoryEntry `json:"submissions"`
	Reviews     []HistoryEntry `json:"reviews"`
}

type LeaderboardEntry struct {
	ID          int64  `json:"id"`
	PublicID    string `json:"public_id,omitempty"`
	PublicLabel string `json:"public_label"`
	Earned      int    `json:"earned"`
	Rank        int    `json:"rank,omitempty"`
}

func (entry *LeaderboardEntry) UnmarshalJSON(data []byte) error {
	type rawLeaderboardEntry struct {
		ID          int64  `json:"id"`
		PublicID    string `json:"public_id"`
		PublicLabel string `json:"public_label"`
		// Legacy field returned by older leaderboard responses.
		Name   string `json:"name"`
		Earned int    `json:"earned"`
		Rank   int    `json:"rank"`
	}

	var raw rawLeaderboardEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	entry.ID = raw.ID
	entry.PublicID = raw.PublicID
	entry.PublicLabel = raw.PublicLabel
	if entry.PublicLabel == "" {
		// Accept the legacy name field while older servers are still deployed.
		entry.PublicLabel = raw.Name
	}
	entry.Earned = raw.Earned
	entry.Rank = raw.Rank
	return nil
}

type LeaderboardViewer struct {
	AllTime *LeaderboardEntry `json:"all_time,omitempty"`
	Weekly  *LeaderboardEntry `json:"weekly,omitempty"`
	Monthly *LeaderboardEntry `json:"monthly,omitempty"`
}

type LeaderboardResponse struct {
	AllTime []LeaderboardEntry `json:"all_time"`
	Weekly  []LeaderboardEntry `json:"weekly"`
	Monthly []LeaderboardEntry `json:"monthly"`
	Viewer  *LeaderboardViewer `json:"viewer,omitempty"`
}

type CLIExchangeResponse struct {
	UserID int64  `json:"user_id"`
	APIKey string `json:"api_key"`
}

type DeviceAuthorizationResponse struct {
	Title           string `json:"title"`
	Description     string `json:"description"`
	VerificationURL string `json:"verification_url"`
	UserCode        string `json:"user_code"`
	DeviceCode      string `json:"device_code"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type DeviceTokenResponse struct {
	Status   string `json:"status"`
	Interval int    `json:"interval,omitempty"`
	UserID   int64  `json:"user_id,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	Message  string `json:"message,omitempty"`
}

type OAuthAuthorizationServerMetadata struct {
	Issuer                      string   `json:"issuer"`
	DeviceAuthorizationEndpoint string   `json:"device_authorization_endpoint"`
	TokenEndpoint               string   `json:"token_endpoint"`
	GrantTypesSupported         []string `json:"grant_types_supported,omitempty"`
	ResponseTypesSupported      []string `json:"response_types_supported,omitempty"`
	ScopesSupported             []string `json:"scopes_supported,omitempty"`
	TokenEndpointAuthMethods    []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}

type OAuthDeviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval,omitempty"`
}

type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type CommandAction struct {
	Command     string `json:"command"`
	Method      string `json:"method,omitempty"`
	Href        string `json:"href,omitempty"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ServiceDocument struct {
	Title              string             `json:"title"`
	Description        string             `json:"description"`
	LatestVersion      string             `json:"latest_version,omitempty"`
	RecommendedVersion string             `json:"recommended_version,omitempty"`
	Links              ClientReleaseLinks `json:"_links,omitempty"`
	Actions            []CommandAction    `json:"actions"`
}

type NavigationRequest struct {
	AuthConfigured  bool     `json:"auth_configured"`
	DetectedAgents  []string `json:"detected_agents,omitempty"`
	DaemonSupported bool     `json:"daemon_supported"`
	DaemonInstalled bool     `json:"daemon_installed"`
	DaemonRunning   bool     `json:"daemon_running"`
	CurrentCommand  string   `json:"current_command,omitempty"`
	LastError       string   `json:"last_error,omitempty"`
}

type NavigationResponse struct {
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Actions     []CommandAction `json:"actions"`
}

type TokenUsage struct {
	Available        bool
	InputTokens      int64
	OutputTokens     int64
	CachedTokens     int64
	CacheWriteTokens int64
	InternalTokens   int64
	TotalTokens      int64
}
