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
		name      string
		access    string
		method    string
		wantBlock bool
	}{
		{"read+GET=allowed", "read", "GET", false},
		{"read+HEAD=allowed", "read", "HEAD", false},
		{"read+OPTIONS=allowed", "read", "OPTIONS", false},
		{"read+POST=blocked", "read", "POST", true},
		{"read+PUT=blocked", "read", "PUT", true},
		{"read+DELETE=blocked", "read", "DELETE", true},
		{"read+PATCH=blocked", "read", "PATCH", true},
		{"write+POST=allowed", "write", "POST", false},
		{"write+DELETE=allowed", "write", "DELETE", false},
		{"empty+POST=allowed", "", "POST", false},
		{"empty+GET=allowed", "", "GET", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.access != "" {
				ctx = WithToolAccess(ctx, tt.access)
			}
			access := ToolAccessFromContext(ctx)
			blocked := access == "read" && !isReadOnlyMethod(tt.method)
			if blocked != tt.wantBlock {
				t.Errorf("access=%q method=%q: blocked=%v, want %v", tt.access, tt.method, blocked, tt.wantBlock)
			}
		})
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
