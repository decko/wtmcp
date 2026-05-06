package main

import (
	"encoding/json"
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
