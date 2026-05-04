package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LeGambiArt/wtmcp/internal/secrets/vault"
)

func TestDiscoverPopulatesVaultResolver(t *testing.T) {
	t.Setenv("WTMCP_VAULT_PASSWORD", "test-password")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Discover(DiscoveryOptions{
		ConfigPath:      cfgPath,
		WorkdirOverride: dir,
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if result.VaultResolver == nil {
		t.Fatal("VaultResolver should be non-nil")
	}

	pw, err := result.VaultResolver("")
	if err != nil {
		t.Fatalf("VaultResolver call failed: %v", err)
	}
	defer vault.ZeroBytes(pw)

	if string(pw) != "test-password" {
		t.Errorf("VaultResolver returned %q, want %q", pw, "test-password")
	}
}

func TestDiscoverVaultResolverAlwaysSet(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Discover(DiscoveryOptions{
		ConfigPath:      cfgPath,
		WorkdirOverride: dir,
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if result.VaultResolver == nil {
		t.Fatal("VaultResolver should always be set")
	}

	_, err = result.VaultResolver("")
	if err == nil {
		t.Error("expected error when no password source configured")
	}
}
