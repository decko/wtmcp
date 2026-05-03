package server

import (
	"testing"
)

func TestValidCommandName(t *testing.T) {
	valid := []string{
		"reload-jira",
		"list",
		"reload-google-calendar",
		"reload-all",
		"reload",
		"a",
		"test-123",
	}
	for _, name := range valid {
		if !validCommandName.MatchString(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}

	invalid := []string{
		"../../etc/passwd",
		"UPPER",
		"has space",
		"has.dot",
		"unicode-日本語",
		"-leading-dash",
		"",
		"reload\x00-jira",
	}
	for _, name := range invalid {
		if validCommandName.MatchString(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input      string
		wantAction string
		wantPlugin string
	}{
		{"reload-jira", "reload", "jira"},
		{"reload-all", "reload", "all"},
		{"reload-google-calendar", "reload", "google-calendar"},
		{"list", "list", ""},
		{"reload", "reload", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			action, plugin := parseCommand(tt.input)
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
			if plugin != tt.wantPlugin {
				t.Errorf("plugin = %q, want %q", plugin, tt.wantPlugin)
			}
		})
	}
}
