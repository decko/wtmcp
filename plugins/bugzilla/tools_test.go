package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- bridge smoke tests (from commit 1) ---

func TestFlushCache(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolFlushCache, map[string]any{})
	bridge.expectCacheFlush(5)
	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
	result, ok := r.val.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", r.val)
	}
	if result["flushed"] != 5 {
		t.Errorf("flushed = %v, want 5", result["flushed"])
	}
}

func TestBridgeHTTP(t *testing.T) {
	bridge := setupToolTest(t)
	ch := make(chan toolResult, 1)
	go func() {
		resp, err := plug.HTTP("GET", "/rest/version")
		ch <- toolResult{resp, err}
	}()
	req := bridge.expectHTTP(200, map[string]any{"version": "5.0"})
	if req.Method != "GET" {
		t.Errorf("method = %s, want GET", req.Method)
	}
	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
}

func TestBridgeCacheGetMiss(t *testing.T) {
	bridge := setupToolTest(t)
	ch := make(chan toolResult, 1)
	go func() {
		_, hit, err := plug.CacheGet("test-key")
		ch <- toolResult{val: map[string]any{"hit": hit}, err: err}
	}()
	bridge.expectCacheGet(false, nil)
	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
	result := r.val.(map[string]any)
	if result["hit"] != false {
		t.Error("expected cache miss")
	}
}

func TestBridgeCacheSet(t *testing.T) {
	bridge := setupToolTest(t)
	ch := make(chan toolResult, 1)
	go func() {
		err := plug.CacheSet("key", "value", 300)
		ch <- toolResult{err: err}
	}()
	bridge.expectCacheSet()
	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
}

// --- bugzilla_search tests ---

func TestSearchBrief(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolSearch, map[string]any{
		"query": "kernel crash",
	})

	req := bridge.expectHTTP(200, map[string]any{
		"bugs": []map[string]any{
			{"id": float64(123), "summary": "crash bug", "status": "NEW",
				"priority": "high", "severity": "urgent",
				"product": "RHEL", "component": "kernel",
				"assigned_to": "dev@example.com"},
		},
	})

	if req.Path != "/rest/bug" {
		t.Errorf("path = %s, want /rest/bug", req.Path)
	}

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(1) {
		t.Errorf("count = %v, want 1", result["count"])
	}
	if result["has_more"] != false {
		t.Errorf("has_more = %v, want false", result["has_more"])
	}

	bugs := result["bugs"].([]any)
	bug := bugs[0].(map[string]any)
	if bug["id"] != float64(123) {
		t.Errorf("id = %v, want 123", bug["id"])
	}
	if _, ok := bug["url"]; !ok {
		t.Error("brief mode should include url")
	}
}

func TestSearchFull(t *testing.T) {
	bridge := setupToolTest(t)
	briefFalse := false
	ch := callTool(toolSearch, searchParams{
		Query: "test",
		Brief: &briefFalse,
	})

	bridge.expectHTTP(200, map[string]any{
		"bugs": []map[string]any{
			{"id": float64(1), "summary": "test", "description": "long desc"},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	bugs := result["bugs"].([]any)
	bug := bugs[0].(map[string]any)
	if _, ok := bug["description"]; !ok {
		t.Error("full mode should include description")
	}
}

func TestSearchHasMore(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolSearch, map[string]any{
		"query":       "test",
		"max_results": 2,
	})

	bridge.expectHTTP(200, map[string]any{
		"bugs": []map[string]any{
			{"id": float64(1), "summary": "a", "status": "NEW",
				"priority": "low", "severity": "low", "product": "P", "component": "C"},
			{"id": float64(2), "summary": "b", "status": "NEW",
				"priority": "low", "severity": "low", "product": "P", "component": "C"},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
	result := toMap(t, r.val)
	if result["has_more"] != true {
		t.Error("has_more should be true when count == max_results")
	}
}

func TestSearchPaginationCap(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolSearch, map[string]any{
		"query":       "test",
		"max_results": 999,
	})

	req := bridge.expectHTTP(200, map[string]any{"bugs": []any{}})
	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	limit := req.Query["limit"]
	if limit != float64(maxLimit) {
		t.Errorf("limit = %v, want %d (capped)", limit, maxLimit)
	}
}

func TestSearchEmptyResults(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolSearch, map[string]any{"query": "nonexistent"})
	bridge.expectHTTP(200, map[string]any{"bugs": []any{}})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
	result := toMap(t, r.val)
	if result["count"] != float64(0) {
		t.Errorf("count = %v, want 0", result["count"])
	}
}

func TestSearchAPIError(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolSearch, map[string]any{"query": "test"})
	bridge.expectHTTP(400, map[string]any{
		"error": true, "message": "Invalid search", "code": 108,
	})

	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for 400 response")
	}
}

func TestSearchWithFilters(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolSearch, map[string]any{
		"query":   "crash",
		"status":  "NEW",
		"product": "RHEL",
	})

	req := bridge.expectHTTP(200, map[string]any{"bugs": []any{}})
	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	if req.Query["status"] != "NEW" {
		t.Errorf("status = %v, want NEW", req.Query["status"])
	}
	if req.Query["product"] != "RHEL" {
		t.Errorf("product = %v, want RHEL", req.Query["product"])
	}
}

// --- bugzilla_get_bugs tests ---

func TestGetBugsSingle(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetBugs, map[string]any{"bug_ids": "12345"})

	req := bridge.expectHTTP(200, map[string]any{
		"bugs": []map[string]any{
			{"id": float64(12345), "summary": "test bug", "status": "NEW",
				"priority": "medium", "severity": "low",
				"product": "RHEL", "component": "kernel"},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	if req.Path != "/rest/bug" {
		t.Errorf("path = %s, want /rest/bug", req.Path)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(1) {
		t.Errorf("count = %v, want 1", result["count"])
	}

	bugs := result["bugs"].([]any)
	bug := bugs[0].(map[string]any)
	if bug["id"] != float64(12345) {
		t.Errorf("id = %v, want 12345", bug["id"])
	}
	if _, ok := bug["url"]; !ok {
		t.Error("brief mode should include url")
	}
}

func TestGetBugsMultiple(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetBugs, map[string]any{"bug_ids": "1,2,3"})

	bridge.expectHTTP(200, map[string]any{
		"bugs": []map[string]any{
			{"id": float64(1), "summary": "bug 1", "status": "NEW",
				"priority": "low", "severity": "low", "product": "P", "component": "C"},
			{"id": float64(2), "summary": "bug 2", "status": "ASSIGNED",
				"priority": "high", "severity": "urgent", "product": "P", "component": "C"},
			{"id": float64(3), "summary": "bug 3", "status": "CLOSED",
				"priority": "medium", "severity": "medium", "product": "P", "component": "C",
				"resolution": "FIXED"},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(3) {
		t.Errorf("count = %v, want 3", result["count"])
	}
}

func TestGetBugsInvalidIDs(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolGetBugs, map[string]any{"bug_ids": "abc"})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for invalid bug IDs")
	}
}

func TestGetBugsEmptyIDs(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolGetBugs, map[string]any{"bug_ids": ""})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for empty bug IDs")
	}
}

// --- bugzilla_get_comments tests ---

func TestGetComments(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetComments, map[string]any{"bug_id": "123"})

	bridge.expectHTTP(200, map[string]any{
		"bugs": map[string]any{
			"123": map[string]any{
				"comments": []map[string]any{
					{"id": float64(1), "text": "First comment", "is_private": false,
						"creator": "user@example.com", "creation_time": "2026-01-01T00:00:00Z"},
					{"id": float64(2), "text": "Second comment", "is_private": false,
						"creator": "dev@example.com", "creation_time": "2026-01-02T00:00:00Z"},
				},
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(2) {
		t.Errorf("count = %v, want 2", result["count"])
	}
	if result["bug_id"] != float64(123) {
		t.Errorf("bug_id = %v, want 123", result["bug_id"])
	}
}

func TestGetCommentsPrivateFiltering(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetComments, map[string]any{
		"bug_id":          "123",
		"include_private": false,
	})

	bridge.expectHTTP(200, map[string]any{
		"bugs": map[string]any{
			"123": map[string]any{
				"comments": []map[string]any{
					{"id": float64(1), "text": "Public", "is_private": false},
					{"id": float64(2), "text": "Private", "is_private": true},
					{"id": float64(3), "text": "Also public", "is_private": false},
				},
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(2) {
		t.Errorf("count = %v, want 2 (private filtered)", result["count"])
	}
}

func TestGetCommentsIncludePrivate(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetComments, map[string]any{
		"bug_id":          "123",
		"include_private": true,
	})

	bridge.expectHTTP(200, map[string]any{
		"bugs": map[string]any{
			"123": map[string]any{
				"comments": []map[string]any{
					{"id": float64(1), "text": "Public", "is_private": false},
					{"id": float64(2), "text": "Private", "is_private": true},
				},
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(2) {
		t.Errorf("count = %v, want 2 (include private)", result["count"])
	}
}

func TestGetCommentsNewSince(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetComments, map[string]any{
		"bug_id":    "123",
		"new_since": "2026-05-01",
	})

	req := bridge.expectHTTP(200, map[string]any{
		"bugs": map[string]any{
			"123": map[string]any{"comments": []any{}},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	if req.Query["new_since"] != "2026-05-01T00:00:00Z" {
		t.Errorf("new_since = %v, want 2026-05-01T00:00:00Z", req.Query["new_since"])
	}
}

func TestGetCommentsTruncation(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetComments, map[string]any{
		"bug_id":      "123",
		"max_results": 2,
	})

	bridge.expectHTTP(200, map[string]any{
		"bugs": map[string]any{
			"123": map[string]any{
				"comments": []map[string]any{
					{"id": float64(1), "text": "oldest", "is_private": false},
					{"id": float64(2), "text": "middle", "is_private": false},
					{"id": float64(3), "text": "newest", "is_private": false},
				},
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(2) {
		t.Errorf("count = %v, want 2", result["count"])
	}
	if result["truncated"] != true {
		t.Error("truncated should be true when comments were trimmed")
	}
	comments := result["comments"].([]any)
	first := comments[0].(map[string]any)
	if first["text"] != "middle" {
		t.Errorf("first comment = %v, want 'middle' (tail truncation keeps most recent)", first["text"])
	}
}

func TestGetCommentsNotTruncatedAtExactLimit(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetComments, map[string]any{
		"bug_id":      "123",
		"max_results": 2,
	})

	bridge.expectHTTP(200, map[string]any{
		"bugs": map[string]any{
			"123": map[string]any{
				"comments": []map[string]any{
					{"id": float64(1), "text": "first", "is_private": false},
					{"id": float64(2), "text": "second", "is_private": false},
				},
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["truncated"] != false {
		t.Error("truncated should be false when count exactly equals max_results (no data lost)")
	}
}

func TestGetCommentsIntBugID(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetComments, map[string]any{
		"bug_id": 456,
	})

	bridge.expectHTTP(200, map[string]any{
		"bugs": map[string]any{
			"456": map[string]any{
				"comments": []map[string]any{
					{"id": float64(1), "text": "test", "is_private": false},
				},
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
	result := toMap(t, r.val)
	if result["bug_id"] != float64(456) {
		t.Errorf("bug_id = %v, want 456", result["bug_id"])
	}
}

func TestGetCommentsInvalidBugID(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolGetComments, map[string]any{"bug_id": "abc"})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for invalid bug_id")
	}
}

// --- bugzilla_get_history tests ---

func TestGetHistory(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetHistory, map[string]any{"bug_id": "456"})

	bridge.expectHTTP(200, map[string]any{
		"bugs": []map[string]any{
			{
				"id": float64(456),
				"history": []map[string]any{
					{"when": "2026-01-01T10:00:00Z", "who": "user@example.com",
						"changes": []map[string]any{
							{"field_name": "status", "removed": "NEW", "added": "ASSIGNED"},
						}},
				},
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(1) {
		t.Errorf("count = %v, want 1", result["count"])
	}
	if result["bug_id"] != float64(456) {
		t.Errorf("bug_id = %v, want 456", result["bug_id"])
	}
}

func TestGetHistoryEmpty(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetHistory, map[string]any{"bug_id": "789"})

	bridge.expectHTTP(200, map[string]any{
		"bugs": []map[string]any{
			{"id": float64(789), "history": []any{}},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(0) {
		t.Errorf("count = %v, want 0", result["count"])
	}
}

func TestGetHistoryNewSince(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetHistory, map[string]any{
		"bug_id":    "123",
		"new_since": "2026-05-01T00:00:00Z",
	})

	req := bridge.expectHTTP(200, map[string]any{
		"bugs": []map[string]any{
			{"id": float64(123), "history": []any{}},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	if req.Query["new_since"] != "2026-05-01T00:00:00Z" {
		t.Errorf("new_since = %v", req.Query["new_since"])
	}
}

func TestGetHistoryInvalidBugID(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolGetHistory, map[string]any{"bug_id": "-1"})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for negative bug_id")
	}
}

// --- bugzilla_download_attachment tests ---

func TestDownloadAttachment(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolDownloadAttachment, map[string]any{"attachment_id": "42"})

	bridge.expectHTTP(200, map[string]any{
		"attachments": map[string]any{
			"42": map[string]any{
				"data":         base64Encode("hello world"),
				"file_name":    "patch.diff",
				"content_type": "text/plain",
				"size":         11,
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["attachment_id"] != float64(42) {
		t.Errorf("attachment_id = %v", result["attachment_id"])
	}
	if result["size"] != float64(11) {
		t.Errorf("size = %v, want 11", result["size"])
	}
	if result["file_name"] != "patch.diff" {
		t.Errorf("file_name = %v", result["file_name"])
	}

	filePath, _ := result["file_path"].(string)
	data, err := os.ReadFile(filePath) //nolint:gosec // test reads file we just wrote
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("file content = %q, want %q", data, "hello world")
	}
}

func TestDownloadAttachmentFilenameHasID(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolDownloadAttachment, map[string]any{"attachment_id": "99"})

	bridge.expectHTTP(200, map[string]any{
		"attachments": map[string]any{
			"99": map[string]any{
				"data":         base64Encode("x"),
				"file_name":    "report.txt",
				"content_type": "text/plain",
				"size":         1,
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	filePath, _ := result["file_path"].(string)
	base := filepath.Base(filePath)
	if base != "99_report.txt" {
		t.Errorf("filename = %q, want %q", base, "99_report.txt")
	}
}

func TestDownloadAttachmentPathTraversal(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"dot-dot-slash", "../../../etc/passwd"},
		{"absolute", "/etc/passwd"},
		{"dot-dot-backslash", "..\\..\\etc\\passwd"},
		{"double-dot", ".."},
		{"single-dot", "."},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bridge := setupToolTest(t)
			ch := callTool(toolDownloadAttachment, map[string]any{"attachment_id": "1"})

			bridge.expectHTTP(200, map[string]any{
				"attachments": map[string]any{
					"1": map[string]any{
						"data":         base64Encode("evil"),
						"file_name":    tt.filename,
						"content_type": "application/octet-stream",
						"size":         4,
					},
				},
			})

			r := collectResult(t, ch)
			if r.err != nil {
				t.Fatalf("unexpected error: %v", r.err)
			}

			result := toMap(t, r.val)
			filePath, _ := result["file_path"].(string)

			if !strings.HasPrefix(filePath, cfg.outputDir) {
				t.Errorf("file written outside output dir: %s", filePath)
			}

			base := filepath.Base(filePath)
			if !strings.HasPrefix(base, "1_") {
				t.Errorf("filename should start with attachment ID prefix, got %q", base)
			}
		})
	}
}

func TestDownloadAttachmentNullByte(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolDownloadAttachment, map[string]any{"attachment_id": "1"})

	bridge.expectHTTP(200, map[string]any{
		"attachments": map[string]any{
			"1": map[string]any{
				"data":         base64Encode("x"),
				"file_name":    "file\x00.txt",
				"content_type": "text/plain",
				"size":         1,
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	filePath, _ := result["file_path"].(string)
	base := filepath.Base(filePath)
	if base != "1_attachment" {
		t.Errorf("null byte filename should be sanitized to '1_attachment', got %q", base)
	}
}

func TestDownloadAttachmentSizeLimit(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolDownloadAttachment, map[string]any{"attachment_id": "1"})

	// Create data that exceeds 6MB when decoded
	largeData := strings.Repeat("A", maxAttachBytes+100)
	encoded := base64Encode(largeData)

	bridge.expectHTTP(200, map[string]any{
		"attachments": map[string]any{
			"1": map[string]any{
				"data":         encoded,
				"file_name":    "huge.bin",
				"content_type": "application/octet-stream",
				"size":         len(largeData),
			},
		},
	})

	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for oversized attachment")
	}
}

func TestDownloadAttachmentExactLimit(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolDownloadAttachment, map[string]any{"attachment_id": "1"})

	exactData := strings.Repeat("X", maxAttachBytes)
	bridge.expectHTTP(200, map[string]any{
		"attachments": map[string]any{
			"1": map[string]any{
				"data":         base64Encode(exactData),
				"file_name":    "exact.bin",
				"content_type": "application/octet-stream",
				"size":         maxAttachBytes,
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("exact limit should succeed: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["size"] != float64(maxAttachBytes) {
		t.Errorf("size = %v, want %d", result["size"], maxAttachBytes)
	}
}

func TestDownloadAttachmentInvalidBase64(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolDownloadAttachment, map[string]any{"attachment_id": "1"})

	bridge.expectHTTP(200, map[string]any{
		"attachments": map[string]any{
			"1": map[string]any{
				"data":         "!!!not-valid-base64!!!",
				"file_name":    "bad.bin",
				"content_type": "application/octet-stream",
				"size":         0,
			},
		},
	})

	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDownloadAttachmentNoOutputDir(t *testing.T) {
	_ = setupToolTest(t)
	cfg.outputDir = ""
	ch := callTool(toolDownloadAttachment, map[string]any{"attachment_id": "1"})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error when output dir is empty")
	}
}

func TestDownloadAttachmentInvalidID(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolDownloadAttachment, map[string]any{"attachment_id": "abc"})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for invalid attachment_id")
	}
}

func TestDownloadAttachmentNotFound(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolDownloadAttachment, map[string]any{"attachment_id": "999"})

	bridge.expectHTTP(200, map[string]any{
		"attachments": map[string]any{},
	})

	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for missing attachment in response")
	}
}

// --- bugzilla_get_attachments tests ---

func TestGetAttachments(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetAttachments, map[string]any{"bug_id": "100"})

	bridge.expectHTTP(200, map[string]any{
		"bugs": map[string]any{
			"100": []map[string]any{
				{"id": float64(1), "file_name": "patch.diff", "content_type": "text/plain",
					"size": float64(1024), "creator": "dev@example.com",
					"creation_time": "2026-01-01T00:00:00Z", "is_obsolete": float64(0),
					"summary": "Fix for crash", "data": "should_be_excluded"},
			},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(1) {
		t.Errorf("count = %v, want 1", result["count"])
	}

	atts := result["attachments"].([]any)
	att := atts[0].(map[string]any)
	if att["file_name"] != "patch.diff" {
		t.Errorf("file_name = %v", att["file_name"])
	}
	if _, hasData := att["data"]; hasData {
		t.Error("shaped attachment should not include data field")
	}
}

func TestGetAttachmentsExcludeFieldsSent(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetAttachments, map[string]any{"bug_id": "100"})

	req := bridge.expectHTTP(200, map[string]any{
		"bugs": map[string]any{"100": []any{}},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	if req.Query["exclude_fields"] != "data" {
		t.Errorf("exclude_fields = %v, want 'data'", req.Query["exclude_fields"])
	}
}

// --- bugzilla_whoami tests ---

func TestWhoami(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolWhoami, map[string]any{})

	bridge.expectHTTP(200, map[string]any{
		"id":        float64(42),
		"name":      "testuser",
		"real_name": "Test User",
		"email":     "test@example.com",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["name"] != "testuser" {
		t.Errorf("name = %v", result["name"])
	}
	if result["email"] != "test@example.com" {
		t.Errorf("email = %v", result["email"])
	}
}

func TestWhoami404(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolWhoami, map[string]any{})

	bridge.expectHTTP(404, map[string]any{
		"error": true, "message": "endpoint not found", "code": 0,
	})

	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for 404")
	}
}

// --- bugzilla_get_user tests ---

func TestGetUserByName(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetUser, map[string]any{"user": "jdoe"})

	req := bridge.expectHTTP(200, map[string]any{
		"users": []map[string]any{
			{"id": float64(1), "name": "jdoe", "real_name": "John Doe",
				"email": "jdoe@example.com", "can_login": true,
				"groups": []map[string]any{{"name": "admin"}}},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	if req.Query["match"] != "jdoe" {
		t.Errorf("expected match param, got query = %v", req.Query)
	}

	result := toMap(t, r.val)
	users := result["users"].([]any)
	user := users[0].(map[string]any)
	if user["name"] != "jdoe" {
		t.Errorf("name = %v", user["name"])
	}
	if _, hasGroups := user["groups"]; hasGroups {
		t.Error("groups should be stripped from response")
	}
}

func TestGetUserByEmail(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetUser, map[string]any{"user": "jdoe@example.com"})

	req := bridge.expectHTTP(200, map[string]any{
		"users": []map[string]any{
			{"id": float64(1), "name": "jdoe", "real_name": "John Doe",
				"email": "jdoe@example.com", "can_login": true},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	if req.Query["names"] != "jdoe@example.com" {
		t.Errorf("expected names param, got query = %v", req.Query)
	}
}

func TestGetUserByID(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetUser, map[string]any{"user": "42"})

	req := bridge.expectHTTP(200, map[string]any{
		"users": []map[string]any{
			{"id": float64(42), "name": "user42", "real_name": "User 42",
				"email": "user42@example.com", "can_login": true},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	if req.Query["ids"] != "42" {
		t.Errorf("expected ids param, got query = %v", req.Query)
	}
}

func TestGetUserEmpty(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolGetUser, map[string]any{"user": ""})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for empty user")
	}
}

// --- bugzilla_get_products tests ---

func TestGetProductsBrief(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetProducts, map[string]any{})

	// cache miss
	bridge.expectCacheGet(false, nil)
	// product_accessible
	bridge.expectHTTP(200, map[string]any{
		"ids": []float64{1, 2},
	})
	// product details batch
	bridge.expectHTTP(200, map[string]any{
		"products": []map[string]any{
			{"id": float64(1), "name": "RHEL",
				"components": []map[string]any{{"name": "kernel"}, {"name": "glibc"}},
				"versions":   []map[string]any{{"name": "9.0"}, {"name": "10.0"}}},
			{"id": float64(2), "name": "Fedora",
				"components": []map[string]any{{"name": "dnf"}},
				"versions":   []map[string]any{{"name": "40"}}},
		},
	})
	// cache set
	bridge.expectCacheSet()

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(2) {
		t.Errorf("count = %v, want 2", result["count"])
	}

	prods := result["products"].([]any)
	prod := prods[0].(map[string]any)
	if prod["name"] != "RHEL" {
		t.Errorf("name = %v", prod["name"])
	}
	if _, hasComponents := prod["components"]; hasComponents {
		t.Error("brief mode should not include components")
	}
}

func TestGetProductsNameFilter(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetProducts, map[string]any{"name_filter": "rhel"})

	bridge.expectCacheGet(false, nil)
	bridge.expectHTTP(200, map[string]any{"ids": []float64{1, 2}})
	bridge.expectHTTP(200, map[string]any{
		"products": []map[string]any{
			{"id": float64(1), "name": "RHEL"},
			{"id": float64(2), "name": "Fedora"},
		},
	})
	bridge.expectCacheSet()

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["count"] != float64(1) {
		t.Errorf("count = %v, want 1 (filtered to RHEL)", result["count"])
	}
}

func TestGetProductsCacheHit(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetProducts, map[string]any{})

	cachedProducts := []map[string]any{
		{"id": float64(1), "name": "CachedProduct"},
	}
	bridge.expectCacheGet(true, cachedProducts)

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["cached"] != true {
		t.Error("expected cached=true")
	}
	if result["count"] != float64(1) {
		t.Errorf("count = %v, want 1", result["count"])
	}
}

// --- bugzilla_get_fields tests ---

func TestGetFields(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetFields, map[string]any{"field_name": "bug_status"})

	bridge.expectCacheGet(false, nil)
	bridge.expectHTTP(200, map[string]any{
		"fields": []map[string]any{
			{"values": []map[string]any{
				{"name": "NEW"}, {"name": "ASSIGNED"}, {"name": "CLOSED"},
			}},
		},
	})
	bridge.expectCacheSet()

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["field"] != "bug_status" {
		t.Errorf("field = %v", result["field"])
	}
	values := result["values"].([]any)
	if len(values) != 3 {
		t.Errorf("values count = %d, want 3", len(values))
	}
}

func TestGetFieldsCacheHit(t *testing.T) {
	bridge := setupToolTest(t)
	ch := callTool(toolGetFields, map[string]any{"field_name": "priority"})

	bridge.expectCacheGet(true, []string{"low", "medium", "high"})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["cached"] != true {
		t.Error("expected cached=true")
	}
}

func TestGetFieldsEmpty(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolGetFields, map[string]any{"field_name": ""})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for empty field_name")
	}
}

func TestGetFieldsPathTraversal(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolGetFields, map[string]any{"field_name": "../../whoami"})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for path traversal in field_name")
	}
}

func TestGetFieldsBackslash(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolGetFields, map[string]any{"field_name": "status\\..\\admin"})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for backslash in field_name")
	}
}

// --- helpers ---

func toMap(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// --- bugzilla_create_bug tests ---

func TestCreateBugDryRun(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolCreateBug, map[string]any{
		"product":   "RHEL",
		"component": "kernel",
		"summary":   "test bug",
		"version":   "9.0",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["dry_run"] != true {
		t.Error("default should be dry_run=true")
	}
	if result["method"] != "POST" {
		t.Errorf("method = %v", result["method"])
	}
	body := result["body"].(map[string]any)
	if body["product"] != "RHEL" {
		t.Errorf("product = %v", body["product"])
	}
	if body["summary"] != "test bug" {
		t.Errorf("summary = %v", body["summary"])
	}
}

func TestCreateBugExecute(t *testing.T) {
	bridge := setupToolTest(t)
	dryRunFalse := false
	ch := callTool(toolCreateBug, createBugParams{
		Product:   "RHEL",
		Component: "kernel",
		Summary:   "real bug",
		Version:   "9.0",
		DryRun:    &dryRunFalse,
	})

	bridge.expectHTTP(200, map[string]any{"id": float64(99999)})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["id"] != float64(99999) {
		t.Errorf("id = %v, want 99999", result["id"])
	}
}

func TestCreateBugMissingRequired(t *testing.T) {
	_ = setupToolTest(t)

	tests := []struct {
		name   string
		params map[string]any
	}{
		{"no product", map[string]any{"component": "k", "summary": "s", "version": "1"}},
		{"no component", map[string]any{"product": "P", "summary": "s", "version": "1"}},
		{"no summary", map[string]any{"product": "P", "component": "k", "version": "1"}},
		{"no version", map[string]any{"product": "P", "component": "k", "summary": "s"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := callTool(toolCreateBug, tt.params)
			r := collectResult(t, ch)
			if r.err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestCreateBugWithOptionalFields(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolCreateBug, map[string]any{
		"product":     "RHEL",
		"component":   "kernel",
		"summary":     "test",
		"version":     "9.0",
		"description": "detailed desc",
		"priority":    "high",
		"severity":    "urgent",
		"assigned_to": "dev@example.com",
		"cc":          []string{"a@b.com", "c@d.com"},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)
	if body["description"] != "detailed desc" {
		t.Errorf("description = %v", body["description"])
	}
	if body["priority"] != "high" {
		t.Errorf("priority = %v", body["priority"])
	}
	cc := body["cc"].([]any)
	if len(cc) != 2 {
		t.Errorf("cc count = %d, want 2", len(cc))
	}
}

// --- bugzilla_update_bug tests ---

func TestUpdateBugDryRun(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id":   "12345",
		"priority": "high",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["dry_run"] != true {
		t.Error("default should be dry_run=true")
	}
	if result["method"] != "PUT" {
		t.Errorf("method = %v", result["method"])
	}
	body := result["body"].(map[string]any)
	if body["priority"] != "high" {
		t.Errorf("priority = %v", body["priority"])
	}
}

func TestUpdateBugExecute(t *testing.T) {
	bridge := setupToolTest(t)
	dryRunFalse := false
	ch := callTool(toolUpdateBug, updateBugParams{
		BugID:    "12345",
		Priority: "high",
		DryRun:   &dryRunFalse,
	})

	bridge.expectHTTP(200, map[string]any{
		"bugs": []map[string]any{
			{"id": float64(12345), "changes": map[string]any{
				"priority": map[string]any{"removed": "low", "added": "high"},
			}},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
}

func TestUpdateBugClosedRequiresResolution(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id": "123",
		"status": "CLOSED",
	})

	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error: CLOSED requires resolution")
	}
}

func TestUpdateBugClosedWithResolution(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id":     "123",
		"status":     "CLOSED",
		"resolution": "FIXED",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)
	if body["status"] != "CLOSED" {
		t.Errorf("status = %v", body["status"])
	}
	if body["resolution"] != "FIXED" {
		t.Errorf("resolution = %v", body["resolution"])
	}
}

func TestUpdateBugCCNesting(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id":    "123",
		"cc_add":    []string{"new@example.com"},
		"cc_remove": []string{"old@example.com"},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)
	cc := body["cc"].(map[string]any)
	add := cc["add"].([]any)
	remove := cc["remove"].([]any)
	if add[0] != "new@example.com" {
		t.Errorf("cc.add = %v", add)
	}
	if remove[0] != "old@example.com" {
		t.Errorf("cc.remove = %v", remove)
	}
}

func TestUpdateBugKeywordsNesting(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id":       "123",
		"keywords_add": []string{"regression"},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)
	kw := body["keywords"].(map[string]any)
	add := kw["add"].([]any)
	if add[0] != "regression" {
		t.Errorf("keywords.add = %v", add)
	}
}

func TestUpdateBugCommentInBody(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id":  "123",
		"status":  "ASSIGNED",
		"comment": "Taking this bug",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)
	comment := body["comment"].(map[string]any)
	if comment["body"] != "Taking this bug" {
		t.Errorf("comment.body = %v", comment["body"])
	}
}

func TestUpdateBugDependsOnNesting(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id":         "123",
		"depends_on_add": []string{"456", "789"},
		"blocks_add":     []string{"100"},
		"blocks_remove":  []string{"200"},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)

	deps := body["depends_on"].(map[string]any)
	depsAdd := deps["add"].([]any)
	if len(depsAdd) != 2 {
		t.Errorf("depends_on.add count = %d, want 2", len(depsAdd))
	}
	if depsAdd[0] != float64(456) {
		t.Errorf("depends_on.add[0] = %v, want 456 (int)", depsAdd[0])
	}

	blocks := body["blocks"].(map[string]any)
	blocksAdd := blocks["add"].([]any)
	if blocksAdd[0] != float64(100) {
		t.Errorf("blocks.add[0] = %v, want 100 (int)", blocksAdd[0])
	}
	blocksRemove := blocks["remove"].([]any)
	if blocksRemove[0] != float64(200) {
		t.Errorf("blocks.remove[0] = %v, want 200 (int)", blocksRemove[0])
	}
}

func TestUpdateBugDependsOnInvalid(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id":         "123",
		"depends_on_add": []string{"abc"},
	})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for non-numeric depends_on_add")
	}
}

func TestUpdateBugWhitespaceResolution(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id":     "123",
		"status":     "CLOSED",
		"resolution": "   ",
	})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error: whitespace resolution should be treated as empty")
	}
}

func TestUpdateBugCommentOnly(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id":  "123",
		"comment": "just a note",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)
	if len(body) != 1 {
		t.Errorf("body should have exactly 1 key (comment), got %d", len(body))
	}
	comment := body["comment"].(map[string]any)
	if comment["body"] != "just a note" {
		t.Errorf("comment.body = %v", comment["body"])
	}
}

func TestUpdateBugNoFields(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{"bug_id": "123"})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error: at least one field required")
	}
}

func TestUpdateBugInvalidID(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateBug, map[string]any{
		"bug_id":   "abc",
		"priority": "high",
	})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for invalid bug_id")
	}
}

// --- bugzilla_add_comment tests ---

func TestAddCommentDryRun(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolAddComment, map[string]any{
		"bug_id":  "456",
		"comment": "This is a test comment",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["dry_run"] != true {
		t.Error("default should be dry_run=true")
	}
	body := result["body"].(map[string]any)
	if body["comment"] != "This is a test comment" {
		t.Errorf("comment = %v", body["comment"])
	}
	if body["is_private"] != false {
		t.Error("is_private should default to false")
	}
}

func TestAddCommentExecute(t *testing.T) {
	bridge := setupToolTest(t)
	dryRunFalse := false
	ch := callTool(toolAddComment, addCommentParams{
		BugID:   "456",
		Comment: "real comment",
		DryRun:  &dryRunFalse,
	})

	bridge.expectHTTP(201, map[string]any{"id": float64(789)})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["id"] != float64(789) {
		t.Errorf("id = %v, want 789", result["id"])
	}
}

func TestAddCommentPrivate(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolAddComment, map[string]any{
		"bug_id":     "456",
		"comment":    "secret",
		"is_private": true,
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)
	if body["is_private"] != true {
		t.Error("is_private should be true")
	}
}

func TestAddCommentEmptyText(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolAddComment, map[string]any{
		"bug_id":  "456",
		"comment": "",
	})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for empty comment")
	}
}

func TestAddCommentInvalidBugID(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolAddComment, map[string]any{
		"bug_id":  "-1",
		"comment": "test",
	})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for negative bug_id")
	}
}

func TestAddCommentIntBugID(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolAddComment, map[string]any{
		"bug_id":  789,
		"comment": "test with int bug_id",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["path"] != "/rest/bug/789/comment" {
		t.Errorf("path = %v", result["path"])
	}
}

// --- helpers ---

// --- bugzilla_add_attachment tests ---

func TestAddAttachmentDryRun(t *testing.T) {
	_ = setupToolTest(t)

	testFile := filepath.Join(cfg.outputDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}

	ch := callTool(toolAddAttachment, map[string]any{
		"bug_id":    "123",
		"file_path": testFile,
		"summary":   "test attachment",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["dry_run"] != true {
		t.Error("default should be dry_run=true")
	}
	if result["method"] != "POST" {
		t.Errorf("method = %v", result["method"])
	}
	body := result["body"].(map[string]any)
	if body["summary"] != "test attachment" {
		t.Errorf("summary = %v", body["summary"])
	}
	if body["content_type"] != "application/octet-stream" {
		t.Errorf("content_type = %v (should default)", body["content_type"])
	}
}

func TestAddAttachmentExecute(t *testing.T) {
	bridge := setupToolTest(t)

	testFile := filepath.Join(cfg.outputDir, "upload.txt")
	if err := os.WriteFile(testFile, []byte("file content"), 0o600); err != nil {
		t.Fatal(err)
	}

	dryRunFalse := false
	ch := callTool(toolAddAttachment, addAttachmentParams{
		BugID:    "123",
		FilePath: testFile,
		Summary:  "upload test",
		DryRun:   &dryRunFalse,
	})

	req := bridge.expectHTTP(201, map[string]any{"ids": []float64{42}})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	if req.Method != "POST" {
		t.Errorf("method = %s, want POST", req.Method)
	}

	var reqBody map[string]any
	if err := json.Unmarshal(req.Body, &reqBody); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	if reqBody["data"] == nil {
		t.Error("request body should contain base64 data")
	}
	if reqBody["summary"] != "upload test" {
		t.Errorf("summary = %v", reqBody["summary"])
	}
}

func TestAddAttachmentPathConfinement(t *testing.T) {
	_ = setupToolTest(t)

	ch := callTool(toolAddAttachment, map[string]any{
		"bug_id":    "123",
		"file_path": "/etc/passwd",
		"summary":   "evil",
	})

	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for path outside allowed dirs")
	}
}

func TestAddAttachmentSizeLimit(t *testing.T) {
	_ = setupToolTest(t)

	bigFile := filepath.Join(cfg.outputDir, "big.bin")
	data := make([]byte, maxAttachBytes+100)
	if err := os.WriteFile(bigFile, data, 0o600); err != nil {
		t.Fatal(err)
	}

	ch := callTool(toolAddAttachment, map[string]any{
		"bug_id":    "123",
		"file_path": bigFile,
		"summary":   "too big",
	})

	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for oversized file")
	}
}

func TestAddAttachmentMissingRequired(t *testing.T) {
	_ = setupToolTest(t)

	testFile := filepath.Join(cfg.outputDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		params map[string]any
	}{
		{"no file_path", map[string]any{"bug_id": "1", "summary": "s"}},
		{"no summary", map[string]any{"bug_id": "1", "file_path": testFile}},
		{"no bug_id", map[string]any{"file_path": testFile, "summary": "s"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := callTool(toolAddAttachment, tt.params)
			r := collectResult(t, ch)
			if r.err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

// --- bugzilla_update_attachment tests ---

func TestUpdateAttachmentDryRun(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateAttachment, map[string]any{
		"attachment_id": "42",
		"description":   "updated desc",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["dry_run"] != true {
		t.Error("default should be dry_run=true")
	}
	body := result["body"].(map[string]any)
	if body["summary"] != "updated desc" {
		t.Errorf("summary = %v (description maps to BZ summary field)", body["summary"])
	}
}

func TestUpdateAttachmentObsolete(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateAttachment, map[string]any{
		"attachment_id": "42",
		"is_obsolete":   true,
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)
	if body["is_obsolete"] != true {
		t.Error("is_obsolete should be true")
	}
}

func TestUpdateAttachmentExecute(t *testing.T) {
	bridge := setupToolTest(t)
	dryRunFalse := false
	ch := callTool(toolUpdateAttachment, updateAttachmentParams{
		AttachmentID: "42",
		Description:  "new desc",
		DryRun:       &dryRunFalse,
	})

	bridge.expectHTTP(200, map[string]any{
		"attachments": []map[string]any{
			{"id": float64(42), "changes": map[string]any{
				"summary": map[string]any{"removed": "old", "added": "new desc"},
			}},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
}

func TestUpdateAttachmentNoFields(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolUpdateAttachment, map[string]any{"attachment_id": "42"})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error: at least one field required")
	}
}

// --- bugzilla_mark_duplicate tests ---

func TestMarkDuplicateDryRun(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolMarkDuplicate, map[string]any{
		"bug_id":       "123",
		"duplicate_of": "456",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	if result["dry_run"] != true {
		t.Error("default should be dry_run=true")
	}
	body := result["body"].(map[string]any)
	if body["status"] != "CLOSED" {
		t.Errorf("status = %v", body["status"])
	}
	if body["resolution"] != "DUPLICATE" {
		t.Errorf("resolution = %v", body["resolution"])
	}
	if body["dupe_of"] != float64(456) {
		t.Errorf("dupe_of = %v", body["dupe_of"])
	}
}

func TestMarkDuplicateExecute(t *testing.T) {
	bridge := setupToolTest(t)
	dryRunFalse := false
	ch := callTool(toolMarkDuplicate, markDuplicateParams{
		BugID:       "123",
		DuplicateOf: "456",
		DryRun:      &dryRunFalse,
	})

	req := bridge.expectHTTP(200, map[string]any{
		"bugs": []map[string]any{
			{"id": float64(123), "changes": map[string]any{
				"status":     map[string]any{"removed": "NEW", "added": "CLOSED"},
				"resolution": map[string]any{"removed": "", "added": "DUPLICATE"},
			}},
		},
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	if req.Method != "PUT" {
		t.Errorf("method = %s, want PUT", req.Method)
	}
}

func TestMarkDuplicateWithComment(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolMarkDuplicate, map[string]any{
		"bug_id":       "123",
		"duplicate_of": "456",
		"comment":      "Same root cause",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)
	comment := body["comment"].(map[string]any)
	if comment["body"] != "Same root cause" {
		t.Errorf("comment.body = %v", comment["body"])
	}
}

func TestMarkDuplicateNoComment(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolMarkDuplicate, map[string]any{
		"bug_id":       "123",
		"duplicate_of": "456",
	})

	r := collectResult(t, ch)
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}

	result := toMap(t, r.val)
	body := result["body"].(map[string]any)
	if _, hasComment := body["comment"]; hasComment {
		t.Error("comment should not be in body when not provided (BZ auto-generates)")
	}
}

func TestMarkDuplicateSelfReference(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolMarkDuplicate, map[string]any{
		"bug_id":       "123",
		"duplicate_of": "123",
	})

	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error: cannot duplicate self")
	}
}

func TestMarkDuplicateInvalidIDs(t *testing.T) {
	_ = setupToolTest(t)
	ch := callTool(toolMarkDuplicate, map[string]any{
		"bug_id":       "abc",
		"duplicate_of": "456",
	})
	r := collectResult(t, ch)
	if r.err == nil {
		t.Fatal("expected error for invalid bug_id")
	}
}
