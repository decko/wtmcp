package auth

import (
	"context"
	"net/http"
	"testing"
)

func TestBearerProvider(t *testing.T) {
	p, _ := NewBearerProvider("my-token", "", "")

	if p.Name() != "bearer" {
		t.Errorf("Name() = %q, want %q", p.Name(), "bearer")
	}
	if !p.Available() {
		t.Error("should be available with token set")
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://example.com", nil)
	headers, err := p.Authenticate(context.Background(), req)
	if err != nil {
		t.Fatalf("Authenticate failed: %v", err)
	}

	got := headers.Get("Authorization")
	expected := "Bearer my-token"
	if got != expected {
		t.Errorf("Authorization = %q, want %q", got, expected)
	}
}

func TestBearerProviderCustomHeader(t *testing.T) {
	p, _ := NewBearerProvider("tok", "X-API-Key", "Token")

	headers, err := p.Authenticate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if headers.Get("X-API-Key") != "Token tok" {
		t.Errorf("got %q", headers.Get("X-API-Key"))
	}
}

func TestBearerProviderEmpty(t *testing.T) {
	p, _ := NewBearerProvider("", "", "")
	if p.Available() {
		t.Error("should not be available without token")
	}
	_, err := p.Authenticate(context.Background(), nil)
	if err == nil {
		t.Error("should error without token")
	}
}

func TestBearerProviderNoPrefix(t *testing.T) {
	p, err := NewBearerProvider("glpat-xxx", "Private-Token", "none")
	if err != nil {
		t.Fatal(err)
	}
	headers, err := p.Authenticate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := headers.Get("Private-Token")
	if got != "glpat-xxx" {
		t.Errorf("Private-Token = %q, want %q", got, "glpat-xxx")
	}
}

func TestBearerProviderNoPrefixCaseInsensitive(t *testing.T) {
	p, err := NewBearerProvider("tok", "X-Custom", "None")
	if err != nil {
		t.Fatal(err)
	}
	headers, _ := p.Authenticate(context.Background(), nil)
	if got := headers.Get("X-Custom"); got != "tok" {
		t.Errorf("got %q, want %q", got, "tok")
	}
}

func TestBearerProviderBlockedHeader(t *testing.T) {
	blocked := []string{"Host", "Cookie", "Connection", "X-Forwarded-For"}
	for _, h := range blocked {
		_, err := NewBearerProvider("tok", h, "")
		if err == nil {
			t.Errorf("NewBearerProvider with header %q should fail", h)
		}
	}
}

func TestBasicProvider(t *testing.T) {
	p := NewBasicProvider("user", "pass")

	if p.Name() != "basic" {
		t.Errorf("Name() = %q, want %q", p.Name(), "basic")
	}
	if !p.Available() {
		t.Error("should be available")
	}

	headers, err := p.Authenticate(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}

	got := headers.Get("Authorization")
	// base64("user:pass") = "dXNlcjpwYXNz"
	if got != "Basic dXNlcjpwYXNz" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestBasicProviderEmpty(t *testing.T) {
	p := NewBasicProvider("", "")
	if p.Available() {
		t.Error("should not be available")
	}
	_, err := p.Authenticate(context.Background(), nil)
	if err == nil {
		t.Error("should error without credentials")
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()

	bearer, _ := NewBearerProvider("tok", "", "")
	r.Register(bearer)

	if !r.Has("bearer") {
		t.Error("should have bearer")
	}
	if r.Has("basic") {
		t.Error("should not have basic")
	}

	p, err := r.Get("bearer")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "bearer" {
		t.Errorf("Name() = %q", p.Name())
	}

	_, err = r.Get("nonexistent")
	if err == nil {
		t.Error("should error for unknown provider")
	}
}

func TestResolveVariantSingle(t *testing.T) {
	cfg := VariantConfig{
		Type: "bearer",
		Variants: map[string]SingleAuthConfig{
			"default": {Type: "bearer", Token: "tok123"},
		},
		VariantOrder: []string{"default"},
		Select:       "default",
	}

	p, err := ResolveVariant(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "bearer" {
		t.Errorf("Name() = %q", p.Name())
	}
}

func TestResolveVariantAuto(t *testing.T) {
	cfg := VariantConfig{
		Select:       "auto",
		VariantOrder: []string{"basic", "bearer"},
		Variants: map[string]SingleAuthConfig{
			"basic":  {Type: "basic", Username: "", Password: ""}, // not available
			"bearer": {Type: "bearer", Token: "tok"},              // available
		},
	}

	p, err := ResolveVariant(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "bearer" {
		t.Errorf("expected bearer (first available), got %q", p.Name())
	}
}

func TestResolveVariantNoneAvailable(t *testing.T) {
	cfg := VariantConfig{
		Select:       "auto",
		VariantOrder: []string{"basic"},
		Variants: map[string]SingleAuthConfig{
			"basic": {Type: "basic", Username: "", Password: ""},
		},
	}

	_, err := ResolveVariant(cfg)
	if err == nil {
		t.Error("should error when no variant is available")
	}
}

func TestIsKnownProviderType(t *testing.T) {
	for _, name := range KnownProviderTypes {
		if !IsKnownProviderType(name) {
			t.Errorf("IsKnownProviderType(%q) = false, want true", name)
		}
	}
	if IsKnownProviderType("unknown") {
		t.Error("IsKnownProviderType(unknown) should be false")
	}
}

func TestNormalizeProviderType(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"kerberos", "kerberos/spnego"},
		{"kerberos/spnego", "kerberos/spnego"},
		{"bearer", "bearer"},
		{"oauth2", "oauth2"},
	}
	for _, tt := range tests {
		got := NormalizeProviderType(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeProviderType(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
