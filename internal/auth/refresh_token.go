package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/LeGambiArt/wtmcp/internal/config"
)

// RefreshTokenProvider exchanges a long-lived refresh/offline token
// for short-lived access tokens via a standard OAuth2 token endpoint.
// Tokens are refreshed automatically when expired.
//
// Works with any OAuth2-compatible token endpoint that supports the
// refresh_token grant type (Keycloak, Azure AD, Okta, etc.).
//
// If the token endpoint rotates the refresh token (RFC 6749 Section 6),
// the provider updates its in-memory copy. When tokenFile is configured,
// the rotated token is persisted atomically so it survives process
// restarts. Without tokenFile, the original env-var value is used on
// restart, which will fail if the endpoint revoked the old token.
type RefreshTokenProvider struct {
	mu           sync.Mutex
	tokenURL     string
	clientID     string
	refreshToken string
	accessToken  string
	expiry       time.Time
	client       *http.Client
	tokenFile    string // optional: persist rotated refresh tokens
}

// NewRefreshTokenProvider creates a refresh-token auth provider.
// Returns an error if tokenURL is not a valid HTTPS URL or if
// transport is nil.
//
// If tokenFile is non-empty and the file exists with a valid
// refresh token, that token is used instead of the refreshToken
// parameter. When the token endpoint rotates the refresh token,
// the new token is persisted to tokenFile atomically.
func NewRefreshTokenProvider(tokenURL, clientID, refreshToken string, transport http.RoundTripper, tokenFile string) (*RefreshTokenProvider, error) {
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

	if tokenFile != "" {
		if persisted, err := loadRefreshToken(tokenFile); err != nil {
			log.Printf("refresh_token: cannot read token file %s: %v (using env token)", tokenFile, err)
		} else if persisted != "" {
			refreshToken = persisted
		}
	}

	return &RefreshTokenProvider{
		tokenURL:     tokenURL,
		clientID:     clientID,
		refreshToken: refreshToken,
		client:       &http.Client{Timeout: 30 * time.Second, Transport: transport},
		tokenFile:    tokenFile,
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.tokenURL, //nolint:gosec // tokenURL from validated config
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("refresh_token: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.client.Do(req) //nolint:gosec
	if err != nil {
		return fmt.Errorf("refresh_token: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB cap
	if err != nil {
		return fmt.Errorf("refresh_token: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("refresh_token: HTTP %d from token endpoint", resp.StatusCode)
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
	if tok.RefreshToken != "" && tok.RefreshToken != r.refreshToken {
		r.refreshToken = tok.RefreshToken
		if r.tokenFile != "" {
			if err := saveRefreshToken(r.tokenFile, tok.RefreshToken); err != nil {
				log.Printf("refresh_token: failed to persist rotated token: %v", err)
			}
		}
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

type persistedRefreshToken struct {
	RefreshToken string `json:"refresh_token"`
}

func loadRefreshToken(path string) (string, error) {
	if err := config.RejectSymlink(path); err != nil {
		return "", fmt.Errorf("token file: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if err := config.CheckPermissions(path, info); err != nil {
		return "", fmt.Errorf("token file: %w", err)
	}

	data, err := os.ReadFile(path) //nolint:gosec // path validated above
	if err != nil {
		return "", err
	}
	var tok persistedRefreshToken
	if err := json.Unmarshal(data, &tok); err != nil {
		return "", fmt.Errorf("parse: %w", err)
	}
	return tok.RefreshToken, nil
}

func saveRefreshToken(path, token string) error {
	data, err := json.Marshal(persistedRefreshToken{RefreshToken: token}) //nolint:gosec // G117: intentional — persisting rotated token
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // path from validated config
		return fmt.Errorf("create dir: %w", err)
	}

	f, err := os.CreateTemp(dir, ".wtmcp-refresh-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	defer os.Remove(f.Name()) //nolint:errcheck // cleanup on failure

	if err := f.Chmod(0o600); err != nil {
		f.Close() //nolint:errcheck,gosec
		return fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close() //nolint:errcheck,gosec
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close() //nolint:errcheck,gosec
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path) //nolint:gosec // path from validated config
}
