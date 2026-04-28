package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkDir(t *testing.T) {
	// Default
	t.Setenv("WHAT_THE_MCP_WORKDIR", "")
	dir := WorkDir()
	if dir == "" || dir == "." {
		t.Error("default workdir should resolve to home-based path")
	}

	// Override
	t.Setenv("WHAT_THE_MCP_WORKDIR", "/custom/path")
	dir = WorkDir()
	if dir != "/custom/path" {
		t.Errorf("WorkDir() = %q, want /custom/path", dir)
	}
}

func TestLoadEnvGroups(t *testing.T) {
	dir := t.TempDir()
	envDir := filepath.Join(dir, "env.d")
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(envDir, "jira.env"), []byte("JIRA_URL=https://jira.example.com\nJIRA_TOKEN=secret123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "google.env"), []byte("GOOGLE_PROJECT=myproject\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Non-.env files should be ignored
	if err := os.WriteFile(filepath.Join(envDir, "skip.txt"), []byte("SKIP=nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := LoadEnvGroups(envDir)
	if err != nil {
		t.Fatalf("LoadEnvGroups: %v", err)
	}

	if len(result.Groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(result.Groups))
	}
	if len(result.Errors) != 0 {
		t.Fatalf("got %d errors, want 0: %v", len(result.Errors), result.Errors)
	}

	jira := result.Groups.Get("jira")
	if jira == nil {
		t.Fatal("expected jira group")
	}
	if jira["JIRA_URL"] != "https://jira.example.com" {
		t.Errorf("JIRA_URL = %q", jira["JIRA_URL"])
	}
	if jira["JIRA_TOKEN"] != "secret123" {
		t.Errorf("JIRA_TOKEN = %q", jira["JIRA_TOKEN"])
	}

	google := result.Groups.Get("google")
	if google == nil {
		t.Fatal("expected google group")
	}
	if google["GOOGLE_PROJECT"] != "myproject" {
		t.Errorf("GOOGLE_PROJECT = %q", google["GOOGLE_PROJECT"])
	}

	// Nonexistent group
	if result.Groups.Get("nonexistent") != nil {
		t.Error("expected nil for nonexistent group")
	}
}

func TestLoadEnvGroupsNotInProcessEnv(t *testing.T) {
	dir := t.TempDir()
	envDir := filepath.Join(dir, "env.d")
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		t.Fatal(err)
	}

	_ = os.Unsetenv("TEST_SCOPED_VAR")
	if err := os.WriteFile(filepath.Join(envDir, "test.env"), []byte("TEST_SCOPED_VAR=from_envd\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadEnvGroups(envDir)
	if err != nil {
		t.Fatalf("LoadEnvGroups: %v", err)
	}

	// Variable must NOT be in the process environment
	if val := os.Getenv("TEST_SCOPED_VAR"); val != "" {
		t.Errorf("TEST_SCOPED_VAR leaked into process env: %q", val)
	}
}

func TestLoadEnvGroupsMissingDir(t *testing.T) {
	result, err := LoadEnvGroups("/nonexistent/path/env.d")
	if err != nil {
		t.Errorf("should not error on missing dir: %v", err)
	}
	if len(result.Groups) != 0 {
		t.Errorf("expected empty groups, got %d", len(result.Groups))
	}
}

func TestLoadEnvGroupsPartialOnBadFilePerms(t *testing.T) {
	dir := t.TempDir()
	envDir := filepath.Join(dir, "env.d")
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Good file
	if err := os.WriteFile(filepath.Join(envDir, "good.env"), []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Bad permissions — should be skipped, not fatal
	if err := os.WriteFile(filepath.Join(envDir, "bad.env"), []byte("SECRET=oops\n"), 0o644); err != nil { //nolint:gosec // intentionally insecure for test
		t.Fatal(err)
	}

	result, err := LoadEnvGroups(envDir)
	if err != nil {
		t.Fatalf("should not return fatal error: %v", err)
	}

	// Good group loaded
	if result.Groups.Get("good") == nil {
		t.Error("expected good group to load")
	}

	// Bad group captured as error, not loaded
	if result.Groups.Get("bad") != nil {
		t.Error("bad group should not be loaded")
	}
	if errMsg, ok := result.Errors["bad"]; !ok {
		t.Error("expected error for bad group")
	} else if !strings.Contains(errMsg, "must not be accessible") {
		t.Errorf("error = %q, want permission error", errMsg)
	}
}

func TestLoadEnvGroupsLooseDirPermsSetsDirError(t *testing.T) {
	dir := t.TempDir()
	envDir := filepath.Join(dir, "env.d")
	if err := os.MkdirAll(envDir, 0o755); err != nil { //nolint:gosec // intentionally insecure for test
		t.Fatal(err)
	}

	result, err := LoadEnvGroups(envDir)
	if err != nil {
		t.Fatalf("should not return fatal error: %v", err)
	}
	if result.DirError == "" {
		t.Fatal("expected DirError to be set")
	}
	if !strings.Contains(result.DirError, "must not be accessible") {
		t.Errorf("DirError = %q, want permission error", result.DirError)
	}
	if len(result.Groups) != 0 {
		t.Errorf("expected empty groups, got %d", len(result.Groups))
	}
}

func TestCheckPermissions(t *testing.T) {
	dir := t.TempDir()

	good := filepath.Join(dir, "good")
	if err := os.WriteFile(good, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(good)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckPermissions(good, info); err != nil {
		t.Errorf("expected no error for 0600, got: %v", err)
	}

	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, nil, 0o644); err != nil { //nolint:gosec // intentionally insecure for test
		t.Fatal(err)
	}
	info, err = os.Stat(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckPermissions(bad, info); err == nil {
		t.Error("expected error for 0644")
	}
}

func TestLoadEnvGroupsRejectsSymlinks(t *testing.T) {
	dir := t.TempDir()
	envDir := filepath.Join(dir, "env.d")
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Good file alongside the symlink
	if err := os.WriteFile(filepath.Join(envDir, "good.env"), []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Symlink — should be captured as error, not fatal
	target := filepath.Join(dir, "real.env")
	if err := os.WriteFile(target, []byte("SECRET=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(envDir, "linked.env")); err != nil {
		t.Fatal(err)
	}

	result, err := LoadEnvGroups(envDir)
	if err != nil {
		t.Fatalf("should not return fatal error: %v", err)
	}

	// Good file loaded
	if result.Groups.Get("good") == nil {
		t.Error("expected good group to load")
	}

	// Symlink captured as error
	if errMsg, ok := result.Errors["linked"]; !ok {
		t.Error("expected error for linked group")
	} else if !strings.Contains(errMsg, "symlink") {
		t.Errorf("error = %q, want symlink error", errMsg)
	}
}

func TestLoadSingleEnvGroup(t *testing.T) {
	dir := t.TempDir()
	envDir := filepath.Join(dir, "env.d")
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(envDir, "mygroup.env"), []byte("KEY=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	vars, err := LoadSingleEnvGroup(envDir, "mygroup")
	if err != nil {
		t.Fatalf("LoadSingleEnvGroup: %v", err)
	}
	if vars["KEY"] != "value" {
		t.Errorf("KEY = %q", vars["KEY"])
	}
}

func TestLoadSingleEnvGroupBadPerms(t *testing.T) {
	dir := t.TempDir()
	envDir := filepath.Join(dir, "env.d")
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(envDir, "bad.env"), []byte("KEY=value\n"), 0o644); err != nil { //nolint:gosec // intentionally insecure for test
		t.Fatal(err)
	}

	_, err := LoadSingleEnvGroup(envDir, "bad")
	if err == nil {
		t.Fatal("expected error for bad permissions")
	}
	if !strings.Contains(err.Error(), "must not be accessible") {
		t.Errorf("error = %q", err)
	}
}

func TestParseEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "test.env")

	content := `
# Comment line
PLAIN_VAR=plain_value
QUOTED_VAR="quoted value"
SINGLE_QUOTED='single quoted'
export EXPORTED_VAR=exported_value
  SPACED_VAR = spaced_value

EMPTY_LINE_ABOVE=yes
`
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	vars, err := parseEnvFile(envFile)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		"PLAIN_VAR":        "plain_value",
		"QUOTED_VAR":       "quoted value",
		"SINGLE_QUOTED":    "single quoted",
		"EXPORTED_VAR":     "exported_value",
		"SPACED_VAR":       "spaced_value",
		"EMPTY_LINE_ABOVE": "yes",
	}

	for key, want := range tests {
		if got := vars[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestPaths(t *testing.T) {
	p := Paths("/opt/wtmcp")

	if p.ConfigFile != "/opt/wtmcp/config.yaml" {
		t.Errorf("ConfigFile = %q", p.ConfigFile)
	}
	if p.EnvDir != "/opt/wtmcp/env.d" {
		t.Errorf("EnvDir = %q", p.EnvDir)
	}
	if p.CredentialsDir != "/opt/wtmcp/credentials" {
		t.Errorf("CredentialsDir = %q", p.CredentialsDir)
	}
	if p.PluginsDir != "/opt/wtmcp/plugins" {
		t.Errorf("PluginsDir = %q", p.PluginsDir)
	}
	if p.CacheDir != "/opt/wtmcp/cache" {
		t.Errorf("CacheDir = %q", p.CacheDir)
	}
}
