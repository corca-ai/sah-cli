package sah

import "time"

type Assignment struct {
	AssignmentID       int64                  `json:"assignment_id"`
	TaskType           string                 `json:"task_type"`
	TaskKey            string                 `json:"task_key"`
	Payload            map[string]any         `json:"payload"`
	InstructionVersion string                 `json:"instruction_version"`
	SchemaVersion      string                 `json:"schema_version"`
	Instructions       AssignmentInstructions `json:"instructions"`
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
	LifetimeEarned   int       `json:"lifetime_earned"`
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
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Earned int    `json:"earned"`
}

type LeaderboardResponse struct {
	AllTime []LeaderboardEntry `json:"all_time"`
	Weekly  []LeaderboardEntry `json:"weekly"`
	Monthly []LeaderboardEntry `json:"monthly"`
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
