//go:build !sandbox

package sandbox

import (
	"context"
	"strings"
	"testing"
)

func TestStubNewManager(t *testing.T) {
	cfg := testConfig()
	mgr, err := NewManager(cfg, "", t.TempDir())
	if err != nil {
		t.Fatalf("stub NewManager should succeed with default config: %v", err)
	}
	defer mgr.Close()

	if mgr == nil {
		t.Fatal("stub NewManager should return non-nil Manager")
	}
}

func TestStubNewManagerExplicitEnable(t *testing.T) {
	enabled := true
	cfg := testConfig()
	cfg.Enabled = &enabled

	_, err := NewManager(cfg, "", t.TempDir())
	if err == nil {
		t.Fatal("expected error when sandbox explicitly enabled but not compiled in")
	}
	if !strings.Contains(err.Error(), "sandbox") {
		t.Errorf("error should mention sandbox, got: %v", err)
	}
}

func TestStubNewManagerExplicitDisable(t *testing.T) {
	disabled := false
	cfg := testConfig()
	cfg.Enabled = &disabled

	mgr, err := NewManager(cfg, "", t.TempDir())
	if err != nil {
		t.Fatalf("explicit disable should succeed: %v", err)
	}
	if mgr == nil {
		t.Fatal("manager should be non-nil")
	}
}

func TestStubEnabled(t *testing.T) {
	mgr, _ := NewManager(testConfig(), "", t.TempDir())
	defer mgr.Close()

	if mgr.Enabled() {
		t.Error("stub Enabled() must always return false")
	}
}

func TestStubAvailable(t *testing.T) {
	mgr, _ := NewManager(testConfig(), "", t.TempDir())
	defer mgr.Close()

	if mgr.Available() {
		t.Error("stub Available() must always return false")
	}
}

func TestStubLaunchReturnsError(t *testing.T) {
	mgr, _ := NewManager(testConfig(), "", t.TempDir())
	defer mgr.Close()

	info := PluginInfo{Name: "test", Dir: "/tmp", Handler: "./handler"}
	_, err := mgr.Launch(context.Background(), info, nil)
	if err == nil {
		t.Fatal("stub Launch() must return an error")
	}
}

func TestStubCloseIdempotent(t *testing.T) {
	mgr, _ := NewManager(testConfig(), "", t.TempDir())
	mgr.Close()
	mgr.Close()
}
