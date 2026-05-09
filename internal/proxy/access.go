package proxy

import (
	"context"
	"net/http"
)

type toolAccessKey struct{}

// WithToolAccess attaches a tool's access level ("read" or "write")
// to the context. The proxy reads this to enforce HTTP method
// restrictions on read-only tools.
func WithToolAccess(ctx context.Context, access string) context.Context {
	return context.WithValue(ctx, toolAccessKey{}, access)
}

// ToolAccessFromContext extracts the tool's access level from context.
// Returns "" if no access level was set (e.g., during plugin init).
func ToolAccessFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(toolAccessKey{}).(string); ok {
		return v
	}
	return ""
}

var readOnlyMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// isReadOnlyMethod returns true if the HTTP method is safe for
// read-only tools.
func isReadOnlyMethod(method string) bool {
	return readOnlyMethods[method]
}
