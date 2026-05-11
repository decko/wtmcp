package proxy

import (
	"context"
	"net/http"
	"testing"
)

func TestToolAccessContext(t *testing.T) {
	ctx := context.Background()

	if got := ToolAccessFromContext(ctx); got != "" {
		t.Errorf("empty context should return empty, got %q", got)
	}

	ctx = WithToolAccess(ctx, "read")
	if got := ToolAccessFromContext(ctx); got != "read" {
		t.Errorf("expected read, got %q", got)
	}

	ctx = WithToolAccess(ctx, "write")
	if got := ToolAccessFromContext(ctx); got != "write" {
		t.Errorf("expected write, got %q", got)
	}
}

func TestReadOnlyAccessEnforcement(t *testing.T) {
	tests := []struct {
		name       string
		access     string
		localWrite bool
		method     string
		wantBlock  bool
	}{
		{"read+GET=allowed", "read", false, "GET", false},
		{"read+HEAD=allowed", "read", false, "HEAD", false},
		{"read+OPTIONS=allowed", "read", false, "OPTIONS", false},
		{"read+POST=blocked", "read", false, "POST", true},
		{"read+PUT=blocked", "read", false, "PUT", true},
		{"read+DELETE=blocked", "read", false, "DELETE", true},
		{"read+PATCH=blocked", "read", false, "PATCH", true},
		{"write+POST=allowed", "write", false, "POST", false},
		{"write+DELETE=allowed", "write", false, "DELETE", false},
		{"empty+POST=allowed", "", false, "POST", false},
		{"empty+GET=allowed", "", false, "GET", false},
		{"read+localwrite+POST=blocked", "read", true, "POST", true},
		{"read+localwrite+PUT=blocked", "read", true, "PUT", true},
		{"read+localwrite+DELETE=blocked", "read", true, "DELETE", true},
		{"read+localwrite+PATCH=blocked", "read", true, "PATCH", true},
		{"read+localwrite+GET=allowed", "read", true, "GET", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.access != "" {
				ctx = WithToolAccess(ctx, tt.access)
			}
			if tt.localWrite {
				ctx = WithLocalWrite(ctx, true)
			}
			access := ToolAccessFromContext(ctx)
			blocked := access == "read" && !isReadOnlyMethod(tt.method)
			if blocked != tt.wantBlock {
				t.Errorf("access=%q localWrite=%v method=%q: blocked=%v, want %v",
					tt.access, tt.localWrite, tt.method, blocked, tt.wantBlock)
			}
		})
	}
}

func TestLocalWriteContext(t *testing.T) {
	ctx := context.Background()
	if LocalWriteFromContext(ctx) {
		t.Error("empty context should return false")
	}

	ctx = WithLocalWrite(ctx, true)
	if !LocalWriteFromContext(ctx) {
		t.Error("expected true after WithLocalWrite(true)")
	}

	ctx = WithLocalWrite(ctx, false)
	if LocalWriteFromContext(ctx) {
		t.Error("expected false after WithLocalWrite(false)")
	}
}

func TestIsReadOnlyMethod(t *testing.T) {
	tests := []struct {
		method string
		want   bool
	}{
		{http.MethodGet, true},
		{http.MethodHead, true},
		{http.MethodOptions, true},
		{http.MethodPost, false},
		{http.MethodPut, false},
		{http.MethodPatch, false},
		{http.MethodDelete, false},
	}
	for _, tt := range tests {
		if got := isReadOnlyMethod(tt.method); got != tt.want {
			t.Errorf("isReadOnlyMethod(%q) = %v, want %v", tt.method, got, tt.want)
		}
	}
}
