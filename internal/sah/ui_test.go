package sah

import "testing"

func TestFormatTokenUsageForClaude(t *testing.T) {
	got := formatTokenUsage("claude", TokenUsage{
		Available:        true,
		InputTokens:      3,
		OutputTokens:     8,
		CachedTokens:     12074,
		CacheWriteTokens: 4824,
		TotalTokens:      16909,
	})

	want := "3 uncached in / 8 out / 16,909 total / 12,074 cache read / 4,824 cache write"
	if got != want {
		t.Fatalf("unexpected format: %q", got)
	}
}

func TestFormatTokenUsageForGemini(t *testing.T) {
	got := formatTokenUsage("gemini", TokenUsage{
		Available:      true,
		InputTokens:    9916,
		OutputTokens:   690,
		InternalTokens: 62910,
		TotalTokens:    73516,
	})

	want := "9,916 in / 690 out / 73,516 total / 62,910 internal"
	if got != want {
		t.Fatalf("unexpected format: %q", got)
	}
}
