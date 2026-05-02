package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// RefreshTokenProvider exchanges a long-lived refresh/offline token
// for short-lived access tokens via a standard OAuth2 token endpoint.
// Tokens are refreshed automatically when expired.
//
// Works with any OAuth2-compatible token endpoint that supports the
// refresh_token grant type (Keycloak, Azure AD, Okta, etc.).
//
// If the token endpoint rotates the refresh token (RFC 6749 Section 6),
// the provider updates its in-memory copy. However, the rotated token
// is NOT persisted — on process restart the original env-var value is
// used. If the endpoint revoked the old token, auth will fail. To
// avoid this, configure the SSO client with rotation disabled or use
// offline tokens (which typically do not rotate).
type RefreshTokenProvider struct {
	mu           sync.Mutex
	tokenURL     string
	clientID     string
	refreshToken string
	accessToken  string
	expiry       time.Time
	client       *http.Client
}

// NewRefreshTokenProvider creates a refresh-token auth provider.
// Returns an error if tokenURL is not a valid HTTPS URL or if
// transport is nil.
func NewRefreshTokenProvider(tokenURL, clientID, refreshToken string, transport http.RoundTripper) (*RefreshTokenProvider, error) {
	u, err := url.Parse(tokenURL)
	if err != nil {
		return nil, fmt.Errorf("refresh_token: invalid token_url: %w", err)
	}
	if u.Scheme != "https" {
		return nil, fmt.Errorf("refresh_token: token_url must use https: %s", tokenURL)
	}
	if transport == nil {
		return nil, fmt.Errorf("refresh_token: transport must not be nil")
	}

	return &RefreshTokenProvider{
		tokenURL:     tokenURL,
		clientID:     clientID,
		refreshToken: refreshToken,
		client:       &http.Client{Timeout: 30 * time.Second, Transport: transport},
	}, nil
}

// Name returns "refresh_token".
func (r *RefreshTokenProvider) Name() string { return "refresh_token" }

// Available reports whether a refresh token and token URL are configured.
func (r *RefreshTokenProvider) Available() bool {
	return r.refreshToken != "" && r.tokenURL != ""
}

// Authenticate returns a Bearer authorization header.
// Exchanges the refresh token for an access token if needed.
func (r *RefreshTokenProvider) Authenticate(ctx context.Context, _ *http.Request) (http.Header, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.accessToken == "" || !time.Now().Before(r.expiry) {
		if err := r.refresh(ctx); err != nil {
			return nil, err
		}
	}

	h := make(http.Header)
	h.Set("Authorization", "Bearer "+r.accessToken)
	return h, nil
}

// refreshTokenResponse is the JSON response from the token endpoint.
type refreshTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
}

func (r *RefreshTokenProvider) refresh(ctx context.Context) error {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {r.clientID},
		"refresh_token": {r.refreshToken},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.tokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("refresh_token: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("refresh_token: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB cap
	if err != nil {
		return fmt.Errorf("refresh_token: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Include truncated response body for debugging (no credentials).
		detail := string(body)
		if len(detail) > 200 {
			detail = detail[:200]
		}
		return fmt.Errorf("refresh_token: HTTP %d from token endpoint: %s", resp.StatusCode, detail)
	}

	var tok refreshTokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return fmt.Errorf("refresh_token: parse response: %w", err)
	}

	if tok.AccessToken == "" {
		return fmt.Errorf("refresh_token: empty access_token in response")
	}

	r.accessToken = tok.AccessToken

	// Handle refresh token rotation (RFC 6749 Section 6).
	if tok.RefreshToken != "" {
		r.refreshToken = tok.RefreshToken
	}

	// Refresh at 90% of expiry to avoid edge-case failures.
	expiresIn := tok.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 300
	}
	r.expiry = time.Now().Add(time.Duration(float64(expiresIn)*0.9) * time.Second)

	log.Printf("refresh_token: token refreshed (expires in %ds)", expiresIn)
	return nil
}
