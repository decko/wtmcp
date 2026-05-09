package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestServer returns an httptest.TLS server that responds to token
// requests. The handler function receives the parsed form values and
// returns the JSON response body and HTTP status code.
func newTestServer(t *testing.T, handler func(grant, clientID, refresh string) (any, int)) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil { //nolint:gosec // test server
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		body, status := handler(
			r.FormValue("grant_type"),    //nolint:gosec
			r.FormValue("client_id"),     //nolint:gosec
			r.FormValue("refresh_token"), //nolint:gosec
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newProvider creates a RefreshTokenProvider pointing at the test server.
func newProvider(t *testing.T, srv *httptest.Server) *RefreshTokenProvider {
	t.Helper()
	p, err := NewRefreshTokenProvider(srv.URL, "test-client", "offline-tok", srv.Client().Transport, "")
	if err != nil {
		t.Fatalf("NewRefreshTokenProvider: %v", err)
	}
	p.client = srv.Client()
	return p
}

func TestName(t *testing.T) {
	srv := newTestServer(t, func(_, _, _ string) (any, int) {
		return refreshTokenResponse{AccessToken: "a", ExpiresIn: 300}, 200
	})
	p := newProvider(t, srv)
	if p.Name() != "refresh_token" {
		t.Errorf("Name() = %q, want %q", p.Name(), "refresh_token")
	}
}

func TestAvailable(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		token     string
		wantAvail bool
	}{
		{"both set", "https://sso.example.com/token", "tok", true},
		{"empty url", "", "tok", false},
		{"empty token", "https://sso.example.com/token", "", false},
		{"both empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &RefreshTokenProvider{tokenURL: tt.url, refreshToken: tt.token}
			if got := p.Available(); got != tt.wantAvail {
				t.Errorf("Available() = %v, want %v", got, tt.wantAvail)
			}
		})
	}
}

func TestSuccessfulExchange(t *testing.T) {
	srv := newTestServer(t, func(grant, clientID, refresh string) (any, int) {
		if grant != "refresh_token" {
			t.Errorf("grant_type = %q", grant)
		}
		if clientID != "test-client" {
			t.Errorf("client_id = %q", clientID)
		}
		if refresh != "offline-tok" {
			t.Errorf("refresh_token = %q", refresh)
		}
		return refreshTokenResponse{AccessToken: "access-123", ExpiresIn: 300}, 200
	})
	p := newProvider(t, srv)

	h, err := p.Authenticate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got := h.Get("Authorization"); got != "Bearer access-123" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestTokenReuse(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(t, func(_, _, _ string) (any, int) {
		calls.Add(1)
		return refreshTokenResponse{AccessToken: "tok", ExpiresIn: 3600}, 200
	})
	p := newProvider(t, srv)

	for range 5 {
		if _, err := p.Authenticate(context.Background(), nil); err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
	}
	if c := calls.Load(); c != 1 {
		t.Errorf("expected 1 token request, got %d", c)
	}
}

func TestAutoRefreshOnExpiry(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(t, func(_, _, _ string) (any, int) {
		n := calls.Add(1)
		return refreshTokenResponse{AccessToken: fmt.Sprintf("tok-%d", n), ExpiresIn: 3600}, 200
	})
	p := newProvider(t, srv)

	// First call — triggers refresh.
	h1, _ := p.Authenticate(context.Background(), nil)

	// Force expiry.
	p.mu.Lock()
	p.expiry = time.Now().Add(-1 * time.Second)
	p.mu.Unlock()

	// Second call — should refresh again.
	h2, _ := p.Authenticate(context.Background(), nil)

	if calls.Load() != 2 {
		t.Errorf("expected 2 refreshes, got %d", calls.Load())
	}
	if h1.Get("Authorization") == h2.Get("Authorization") {
		t.Error("expected different tokens after refresh")
	}
}

func TestRefreshTokenRotation(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(t, func(_, _, refresh string) (any, int) {
		n := calls.Add(1)
		if n == 1 {
			if refresh != "offline-tok" {
				t.Errorf("first call: refresh = %q, want %q", refresh, "offline-tok")
			}
			return refreshTokenResponse{
				AccessToken:  "access-1",
				ExpiresIn:    3600,
				RefreshToken: "rotated-tok",
			}, 200
		}
		// Second call should use the rotated token.
		if refresh != "rotated-tok" {
			t.Errorf("second call: refresh = %q, want %q", refresh, "rotated-tok")
		}
		return refreshTokenResponse{AccessToken: "access-2", ExpiresIn: 3600}, 200
	})
	p := newProvider(t, srv)

	_, _ = p.Authenticate(context.Background(), nil)

	// Force expiry to trigger second refresh.
	p.mu.Lock()
	p.expiry = time.Now().Add(-1 * time.Second)
	p.mu.Unlock()

	_, _ = p.Authenticate(context.Background(), nil)

	if calls.Load() != 2 {
		t.Errorf("expected 2 refreshes, got %d", calls.Load())
	}
}

func TestHTTPError(t *testing.T) {
	srv := newTestServer(t, func(_, _, _ string) (any, int) {
		return map[string]string{"error": "invalid_grant"}, 401
	})
	p := newProvider(t, srv)

	_, err := p.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error on 401")
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Errorf("error = %q, want HTTP 401", err)
	}
}

func TestEmptyAccessToken(t *testing.T) {
	srv := newTestServer(t, func(_, _, _ string) (any, int) {
		return refreshTokenResponse{AccessToken: "", ExpiresIn: 300}, 200
	})
	p := newProvider(t, srv)

	_, err := p.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error on empty access_token")
	}
	if !strings.Contains(err.Error(), "empty access_token") {
		t.Errorf("error = %q", err)
	}
}

func TestMalformedJSON(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, "<html>Error</html>")
	}))
	t.Cleanup(srv.Close)

	p, _ := NewRefreshTokenProvider(srv.URL, "client", "tok", srv.Client().Transport, "")
	p.client = srv.Client()

	_, err := p.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("error = %q", err)
	}
}

func TestOversizedResponse(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		// Write >1MB of data — LimitReader should cap it, causing
		// json.Unmarshal to fail on truncated JSON.
		_, _ = w.Write([]byte(`{"access_token":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", 2<<20)))
		_, _ = w.Write([]byte(`","expires_in":300,"token_type":"Bearer"}`))
	}))
	t.Cleanup(srv.Close)

	p, _ := NewRefreshTokenProvider(srv.URL, "client", "tok", srv.Client().Transport, "")
	p.client = srv.Client()

	_, err := p.Authenticate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error on oversized response")
	}
}

func TestMissingExpiresInDefaults(t *testing.T) {
	srv := newTestServer(t, func(_, _, _ string) (any, int) {
		// Return without expires_in.
		return map[string]string{"access_token": "tok", "token_type": "Bearer"}, 200
	})
	p := newProvider(t, srv)

	_, err := p.Authenticate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	p.mu.Lock()
	remaining := time.Until(p.expiry)
	p.mu.Unlock()

	// Should default to ~270s (90% of 300).
	if remaining < 200*time.Second || remaining > 280*time.Second {
		t.Errorf("expiry in %v, expected ~270s", remaining)
	}
}

func TestNonHTTPSRejected(t *testing.T) {
	_, err := NewRefreshTokenProvider("http://sso.example.com/token", "client", "tok", nil, "")
	if err == nil {
		t.Fatal("expected error for http:// URL")
	}
	if !strings.Contains(err.Error(), "must use https") {
		t.Errorf("error = %q", err)
	}
}

func TestInvalidURL(t *testing.T) {
	_, err := NewRefreshTokenProvider("://bad", "client", "tok", nil, "")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestContextCancellation(t *testing.T) {
	// Use a server that delays long enough for the context to expire.
	arrived := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		close(arrived)
		time.Sleep(2 * time.Second)
	}))
	t.Cleanup(srv.Close)

	p, _ := NewRefreshTokenProvider(srv.URL, "client", "tok", srv.Client().Transport, "")
	p.client = srv.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := p.Authenticate(ctx, nil)
		done <- err
	}()

	// Wait for the request to arrive at the server.
	<-arrived

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error on context cancellation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Authenticate did not return within 5s after context timeout")
	}
}

func TestSaveLoadRefreshToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")

	if err := saveRefreshToken(path, "my-rotated-token"); err != nil {
		t.Fatalf("saveRefreshToken: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("file mode = %04o, want no group/other access", mode)
	}

	tok, err := loadRefreshToken(path)
	if err != nil {
		t.Fatalf("loadRefreshToken: %v", err)
	}
	if tok != "my-rotated-token" {
		t.Errorf("token = %q, want %q", tok, "my-rotated-token")
	}
}

func TestLoadRefreshToken_MissingFile(t *testing.T) {
	_, err := loadRefreshToken(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadRefreshToken_LoosePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	data := `{"refresh_token":"tok"}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil { //nolint:gosec // intentionally loose for test
		t.Fatal(err)
	}

	_, err := loadRefreshToken(path)
	if err == nil {
		t.Fatal("expected error for loose permissions")
	}
	if !strings.Contains(err.Error(), "group/other") {
		t.Errorf("error = %q, want group/other permission error", err)
	}
}

func TestLoadRefreshToken_Symlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "link.json")

	if err := os.WriteFile(target, []byte(`{"refresh_token":"tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	_, err := loadRefreshToken(link)
	if err == nil {
		t.Fatal("expected error for symlink")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %q, want symlink error", err)
	}
}

func TestNewRefreshTokenProvider_PrefersPersistedToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token.json")
	if err := saveRefreshToken(path, "persisted-tok"); err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, func(_, _, refresh string) (any, int) {
		if refresh != "persisted-tok" {
			t.Errorf("refresh = %q, want %q", refresh, "persisted-tok")
		}
		return refreshTokenResponse{AccessToken: "access", ExpiresIn: 3600}, 200
	})

	p, err := NewRefreshTokenProvider(srv.URL, "client", "env-tok", srv.Client().Transport, path)
	if err != nil {
		t.Fatal(err)
	}
	p.client = srv.Client()

	_, err = p.Authenticate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestConcurrentAccess(t *testing.T) {
	var calls atomic.Int32
	srv := newTestServer(t, func(_, _, _ string) (any, int) {
		calls.Add(1)
		return refreshTokenResponse{AccessToken: "tok", ExpiresIn: 3600}, 200
	})
	p := newProvider(t, srv)

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := p.Authenticate(context.Background(), nil)
			if err != nil {
				errs <- err
				return
			}
			if h.Get("Authorization") != "Bearer tok" {
				errs <- fmt.Errorf("unexpected auth: %q", h.Get("Authorization"))
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	// Mutex ensures only one refresh happens.
	if c := calls.Load(); c != 1 {
		t.Errorf("expected 1 refresh, got %d", c)
	}
}
