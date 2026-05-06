package main

import (
	"testing"
)

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
