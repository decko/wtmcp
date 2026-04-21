package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// BearerProvider injects a token into requests via a configurable
// header. Supports both standard "Authorization: Bearer <token>"
// and custom headers like "PRIVATE-TOKEN: <token>".
type BearerProvider struct {
	token  string
	header string
	prefix string
}

// blockedAuthHeaders are header names that must not be used as
// custom auth headers. injectAuth runs after stripDangerousHeaders,
// so these headers would bypass the strip filter if allowed.
var blockedAuthHeaders = map[string]bool{
	"host":              true,
	"cookie":            true,
	"set-cookie":        true,
	"connection":        true,
	"upgrade":           true,
	"transfer-encoding": true,
	"te":                true,
	"trailer":           true,
	"forwarded":         true,
	"x-forwarded-for":   true,
	"x-forwarded-host":  true,
	"x-forwarded-proto": true,
	"x-real-ip":         true,
	"x-original-url":    true,
	"x-rewrite-url":     true,
}

// NewBearerProvider creates a bearer token auth provider.
// If header is empty, defaults to "Authorization".
// If prefix is "none", the token is injected without any prefix.
// If prefix is empty, defaults to "Bearer".
// Returns an error if header is a blocked header name.
func NewBearerProvider(token, header, prefix string) (*BearerProvider, error) {
	if header == "" {
		header = "Authorization"
	}
	if blockedAuthHeaders[strings.ToLower(header)] {
		return nil, fmt.Errorf("auth header %q is not allowed (blocked for security)", header)
	}
	switch strings.ToLower(prefix) {
	case "none":
		prefix = ""
	case "":
		prefix = "Bearer"
	}
	return &BearerProvider{token: token, header: header, prefix: prefix}, nil
}

// Name returns "bearer".
func (b *BearerProvider) Name() string { return "bearer" }

// Available reports whether a token is configured.
func (b *BearerProvider) Available() bool { return b.token != "" }

// Authenticate returns the authorization header.
func (b *BearerProvider) Authenticate(_ context.Context, _ *http.Request) (http.Header, error) {
	if b.token == "" {
		return nil, fmt.Errorf("bearer token not configured")
	}
	h := make(http.Header)
	if b.prefix != "" {
		h.Set(b.header, b.prefix+" "+b.token)
	} else {
		h.Set(b.header, b.token)
	}
	return h, nil
}
