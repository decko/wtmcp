package main

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	gogitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestDetectMultiInstanceNone(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "tok")
	// Clear any multi-instance vars
	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		if key != "GITLAB_TOKEN" && key != "GITLAB_URL" &&
			len(key) > 7 && key[:7] == "GITLAB_" && key[len(key)-6:] == "_TOKEN" {
			t.Setenv(key, "")
		}
	}

	names := detectMultiInstance()
	if len(names) != 0 {
		t.Errorf("expected no multi-instance, got %v", names)
	}
}

func TestDetectMultiInstanceFound(t *testing.T) {
	t.Setenv("GITLAB_PUBLIC_TOKEN", "pub-token")
	t.Setenv("GITLAB_INTERNAL_TOKEN", "int-token")

	names := detectMultiInstance()
	if len(names) != 2 {
		t.Fatalf("expected 2 multi-instance names, got %v", names)
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://gitlab.com", "gitlab.com"},
		{"https://gitlab.example.com/path", "gitlab.example.com"},
		{"https://gitlab.internal:8443", "gitlab.internal"},
		{"", ""},
		{"://invalid", ""},
	}
	for _, tt := range tests {
		got := extractHost(tt.url)
		if got != tt.want {
			t.Errorf("extractHost(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestResolveInstanceSingle(t *testing.T) {
	ts := httptest.NewServer(nil)
	defer ts.Close()

	client, err := gogitlab.NewClient("", gogitlab.WithBaseURL(ts.URL+"/api/v4"))
	if err != nil {
		t.Fatal(err)
	}
	instances = map[string]*instance{
		"default": {Name: "default", URL: ts.URL, Client: client},
	}
	defaultInstance = "default"

	got, err := resolveInstance("")
	if err != nil {
		t.Fatalf("resolveInstance: %v", err)
	}
	if got == nil {
		t.Fatal("client is nil")
	}
}

func TestResolveInstanceUnknown(t *testing.T) {
	ts := httptest.NewServer(nil)
	defer ts.Close()

	client, err := gogitlab.NewClient("", gogitlab.WithBaseURL(ts.URL+"/api/v4"))
	if err != nil {
		t.Fatal(err)
	}
	instances = map[string]*instance{
		"default": {Name: "default", URL: ts.URL, Client: client},
	}
	defaultInstance = "default"

	_, err = resolveInstance("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown instance")
	}
}

func TestParseTime(t *testing.T) {
	if pt := parseTime("2026-03-10T14:00:00Z"); pt == nil {
		t.Error("parseTime(RFC3339) returned nil")
	}
	if pt := parseTime("2026-03-10"); pt == nil {
		t.Error("parseTime(date) returned nil")
	}
	if pt := parseTime("not-a-date"); pt != nil {
		t.Error("parseTime(invalid) should return nil")
	}
}
