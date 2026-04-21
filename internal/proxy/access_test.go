package proxy

import (
	"context"
	"net/http"
	"testing"
)

func TestToolAccessContext(t *testing.T) {
	ctx := context.Background()

	if got := toolAccessFromContext(ctx); got != "" {
		t.Errorf("empty context should return empty, got %q", got)
	}

	ctx = WithToolAccess(ctx, "read")
	if got := toolAccessFromContext(ctx); got != "read" {
		t.Errorf("expected read, got %q", got)
	}

	ctx = WithToolAccess(ctx, "write")
	if got := toolAccessFromContext(ctx); got != "write" {
		t.Errorf("expected write, got %q", got)
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
