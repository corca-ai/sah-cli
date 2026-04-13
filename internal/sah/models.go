package sah

import (
	"encoding/json"
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
}

type AssignmentLinks struct {
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
	ID               int64     `json:"id"`
	Email            string    `json:"email"`
	Name             string    `json:"name"`
	Credits          int       `json:"credits"`
	LeaderboardScore int       `json:"leaderboard_score"`
	Trust            float64   `json:"trust"`
	CreatedAt        time.Time `json:"created_at"`
	Rank             int       `json:"rank"`
	PendingCredits   int       `json:"pending_credits"`
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
		Name        string `json:"name"`
		Earned      int    `json:"earned"`
		Rank        int    `json:"rank"`
	}

	var raw rawLeaderboardEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	entry.ID = raw.ID
	entry.PublicID = raw.PublicID
	entry.PublicLabel = raw.PublicLabel
	if entry.PublicLabel == "" {
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

type TokenUsage struct {
	Available    bool
	InputTokens  int64
	OutputTokens int64
	CachedTokens int64
	TotalTokens  int64
}
