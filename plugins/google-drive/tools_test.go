package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func TestExtractFileID(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "google doc URL",
			url:  "https://docs.google.com/document/d/1abc123xyz/edit",
			want: "1abc123xyz",
		},
		{
			name: "google sheet URL",
			url:  "https://docs.google.com/spreadsheets/d/1abc123xyz/edit#gid=0",
			want: "1abc123xyz",
		},
		{
			name: "google slides URL",
			url:  "https://docs.google.com/presentation/d/1abc123xyz/edit",
			want: "1abc123xyz",
		},
		{
			name: "drive file URL",
			url:  "https://drive.google.com/file/d/1abc123xyz/view",
			want: "1abc123xyz",
		},
		{
			name: "drive open URL with id param",
			url:  "https://drive.google.com/open?id=1abc123xyz",
			want: "1abc123xyz",
		},
		{
			name: "file ID with hyphens and underscores",
			url:  "https://docs.google.com/document/d/1a-b_c123XYZ/edit",
			want: "1a-b_c123XYZ",
		},
		{
			name: "not a google URL",
			url:  "https://example.com/page",
			want: "",
		},
		{
			name: "empty string",
			url:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFileID(tt.url)
			if got != tt.want {
				t.Errorf("extractFileID(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestCleanGoogleDocsCSS(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no CSS",
			input: "# Hello\n\nSome text",
			want:  "# Hello\n\nSome text",
		},
		{
			name:  "strips @import lines",
			input: "# Title\n@import url('https://fonts.googleapis.com');\nReal content",
			want:  "# Title\nReal content",
		},
		{
			name:  "strips list-style-type blocks",
			input: "# Title\n.lst-kix_abc { list-style-type: disc; }\nReal content",
			want:  "# Title\nReal content",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanGoogleDocsCSS(tt.input)
			if got != tt.want {
				t.Errorf("cleanGoogleDocsCSS() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildSearchQuery(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		inNameOnly     bool
		mimeTypes      []string
		owners         []string
		includeTrashed bool
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:         "text only full-text",
			text:         "quarterly report",
			wantContains: []string{"fullText contains 'quarterly report'", "name contains 'quarterly report'", "trashed = false"},
		},
		{
			name:           "name only",
			text:           "budget",
			inNameOnly:     true,
			wantContains:   []string{"name contains 'budget'"},
			wantNotContain: []string{"fullText"},
		},
		{
			name:         "single mime type",
			text:         "doc",
			mimeTypes:    []string{"application/vnd.google-apps.document"},
			wantContains: []string{"mimeType = 'application/vnd.google-apps.document'"},
		},
		{
			name:         "multiple mime types ORed",
			text:         "doc",
			mimeTypes:    []string{"application/vnd.google-apps.document", "application/pdf"},
			wantContains: []string{"(mimeType = 'application/vnd.google-apps.document' or mimeType = 'application/pdf')"},
		},
		{
			name:         "single owner",
			text:         "design",
			owners:       []string{"me"},
			wantContains: []string{"'me' in owners"},
		},
		{
			name:         "multiple owners ORed",
			text:         "design",
			owners:       []string{"alice@example.com", "bob@example.com"},
			wantContains: []string{"('alice@example.com' in owners or 'bob@example.com' in owners)"},
		},
		{
			name:           "include trashed",
			text:           "old",
			includeTrashed: true,
			wantNotContain: []string{"trashed"},
		},
		{
			name:         "combined filters",
			text:         "meeting",
			mimeTypes:    []string{"application/vnd.google-apps.document"},
			owners:       []string{"me"},
			wantContains: []string{"fullText contains", "mimeType =", "'me' in owners", "trashed = false"},
		},
		{
			name:         "escapes quotes in text",
			text:         "it's a test",
			wantContains: []string{"it\\'s a test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSearchQuery(tt.text, tt.inNameOnly, tt.mimeTypes, tt.owners, tt.includeTrashed)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("query %q should contain %q", got, want)
				}
			}
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(got, notWant) {
					t.Errorf("query %q should not contain %q", got, notWant)
				}
			}
		})
	}
}

func TestSaveExportFile(t *testing.T) {
	origOutputDir := outputDir
	t.Cleanup(func() { outputDir = origOutputDir })

	tmpDir := t.TempDir()
	outputDir = tmpDir

	t.Run("saves with explicit path", func(t *testing.T) {
		got, err := saveExportFile("", "test.md", "hello world")
		if err != nil {
			t.Fatalf("saveExportFile: %v", err)
		}
		if !strings.HasSuffix(got, "test.md") {
			t.Errorf("path %q should end with test.md", got)
		}

		data, err := os.ReadFile(got) //nolint:gosec // test file path
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(data) != "hello world" {
			t.Errorf("content = %q, want %q", string(data), "hello world")
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		_, err := saveExportFile("", filepath.Join("sub", "dir", "test.md"), "nested")
		if err != nil {
			t.Fatalf("saveExportFile: %v", err)
		}
	})

	t.Run("file permissions are 0600", func(t *testing.T) {
		got, err := saveExportFile("", "perms.md", "secret")
		if err != nil {
			t.Fatalf("saveExportFile: %v", err)
		}
		info, err := os.Stat(got)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("permissions = %o, want 0600", perm)
		}
	})

	t.Run("rejects path traversal", func(t *testing.T) {
		_, err := saveExportFile("", "../../etc/evil.md", "pwned")
		if err == nil {
			t.Fatal("expected error for path traversal, got nil")
		}
	})

	t.Run("sanitizes title with special characters", func(t *testing.T) {
		got, err := saveExportFile("../../etc/evil", "", "content")
		if err != nil {
			t.Fatalf("saveExportFile: %v", err)
		}
		resolved, _ := filepath.EvalSymlinks(tmpDir)
		if !strings.HasPrefix(got, resolved+string(os.PathSeparator)) {
			t.Errorf("path %q escapes output directory %q", got, resolved)
		}
	})

	t.Run("empty outputDir rejected", func(t *testing.T) {
		outputDir = ""
		t.Cleanup(func() { outputDir = tmpDir })
		_, err := saveExportFile("test", "", "content")
		if err == nil {
			t.Fatal("expected error when outputDir is empty")
		}
	})

	t.Run("symlink escape detected", func(t *testing.T) {
		outside := t.TempDir()
		linkDir := filepath.Join(tmpDir, "escape")
		if err := os.Symlink(outside, linkDir); err != nil {
			t.Skipf("symlinks not supported: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(linkDir) })

		_, err := saveExportFile("", "escape/evil.md", "pwned")
		if err == nil {
			t.Fatal("expected error for symlink escaping output dir")
		}
	})
}

// --- httptest integration tests ---

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func setupDriveTest(t *testing.T, handler http.Handler) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	svc, err := drive.NewService(context.Background(),
		option.WithHTTPClient(ts.Client()),
		option.WithEndpoint(ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	driveSvc = svc
}

func TestToolGetFileByID(t *testing.T) {
	setupDriveTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"abc","name":"doc.txt","mimeType":"text/plain","webViewLink":"https://drive.google.com/file/d/abc/view"}`)
	}))

	result, err := toolGetFileByID(mustJSON(t, map[string]any{
		"file_id": "abc",
	}), nil)
	if err != nil {
		t.Fatalf("toolGetFileByID: %v", err)
	}

	file, ok := result.(*drive.File)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if file.Name != "doc.txt" {
		t.Errorf("name = %q", file.Name)
	}
}

func TestToolGetFileByIDMissing(t *testing.T) {
	_, err := toolGetFileByID(mustJSON(t, map[string]any{}), nil)
	if err == nil {
		t.Fatal("expected error for missing file_id")
	}
}

func TestToolGetFileByURL(t *testing.T) {
	setupDriveTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"xyz","name":"design.doc","mimeType":"application/vnd.google-apps.document"}`)
	}))

	result, err := toolGetFileByURL(mustJSON(t, map[string]any{
		"url": "https://docs.google.com/document/d/xyz/edit",
	}), nil)
	if err != nil {
		t.Fatalf("toolGetFileByURL: %v", err)
	}

	file, ok := result.(*drive.File)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if file.Id != "xyz" {
		t.Errorf("id = %q", file.Id)
	}
}

func TestToolGetFileByURLInvalid(t *testing.T) {
	result, err := toolGetFileByURL(mustJSON(t, map[string]any{
		"url": "https://example.com/not-a-drive-url",
	}), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := result.(map[string]string)
	if !ok {
		t.Fatalf("expected error map, got %T", result)
	}
	if _, hasErr := m["error"]; !hasErr {
		t.Error("expected error key")
	}
}

func TestToolSearchText(t *testing.T) {
	setupDriveTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"files":[{"id":"f1","name":"report.doc","mimeType":"application/vnd.google-apps.document"}]}`)
	}))

	result, err := toolSearchText(mustJSON(t, map[string]any{
		"text": "quarterly report",
	}), nil)
	if err != nil {
		t.Fatalf("toolSearchText: %v", err)
	}

	list, ok := result.(*drive.FileList)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if len(list.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(list.Files))
	}
	if list.Files[0].Name != "report.doc" {
		t.Errorf("name = %q", list.Files[0].Name)
	}
}

func TestToolSearchTextMissing(t *testing.T) {
	_, err := toolSearchText(mustJSON(t, map[string]any{}), nil)
	if err == nil {
		t.Fatal("expected error for missing text")
	}
}

func TestToolSearchFiles(t *testing.T) {
	setupDriveTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"files":[{"id":"f2","name":"data.csv"}]}`)
	}))

	result, err := toolSearchFiles(mustJSON(t, map[string]any{
		"query": "name contains 'data'",
	}), nil)
	if err != nil {
		t.Fatalf("toolSearchFiles: %v", err)
	}

	list, ok := result.(*drive.FileList)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if len(list.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(list.Files))
	}
}
