package sah

import "testing"

func TestBrowserCommandPrefersBROWSEROnLinux(t *testing.T) {
	command, err := browserCommand("https://example.com", "linux", func(key string) string {
		if key == "BROWSER" {
			return "w3m"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("browserCommand returned error: %v", err)
	}
	if got := command.Args[0]; got != "w3m" {
		t.Fatalf("expected text browser command, got %q", got)
	}
	if got := command.Args[1]; got != "https://example.com" {
		t.Fatalf("expected URL argument, got %q", got)
	}
}

func TestBrowserCommandSupportsPlaceholder(t *testing.T) {
	command, err := browserCommand("https://example.com", "linux", func(key string) string {
		if key == "BROWSER" {
			return "links %s"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("browserCommand returned error: %v", err)
	}
	if got := len(command.Args); got != 2 {
		t.Fatalf("expected 2 command args, got %d", got)
	}
	if got := command.Args[1]; got != "https://example.com" {
		t.Fatalf("expected placeholder to be replaced, got %q", got)
	}
}

func TestBrowserCommandFallsBackToXdgOpenOnLinux(t *testing.T) {
	command, err := browserCommand("https://example.com", "linux", func(string) string { return "" })
	if err != nil {
		t.Fatalf("browserCommand returned error: %v", err)
	}
	if got := command.Args[0]; got != "xdg-open" {
		t.Fatalf("expected xdg-open fallback, got %q", got)
	}
}
