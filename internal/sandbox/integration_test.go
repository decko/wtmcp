//go:build sandbox

package sandbox

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/LeGambiArt/wtmcp/internal/config"
)

type probeRequest struct {
	Cmd  string `json:"cmd"`
	Path string `json:"path,omitempty"`
	Data string `json:"data,omitempty"`
	Addr string `json:"addr,omitempty"`
	Msg  string `json:"msg,omitempty"`
	Key  string `json:"key,omitempty"`
}

type probeResponse struct {
	OK    bool   `json:"ok"`
	Data  string `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

func buildProbe(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "probe")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/probe.go") //nolint:gosec // test helper
	cmd.Dir = filepath.Join(projectRoot(), "internal", "sandbox")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build probe: %v\n%s", err, out)
	}
	return bin
}

func projectRoot() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..")
}

func runProbe(t *testing.T, mgr *Manager, info PluginInfo, env map[string]string, req probeRequest) probeResponse {
	t.Helper()
	ctx := context.Background()

	proc, err := mgr.Launch(ctx, info, env)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	data, _ := json.Marshal(req)
	data = append(data, '\n')
	if _, err := proc.Stdin().Write(data); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	proc.Stdin().Close() //nolint:errcheck,gosec // test cleanup

	var resp probeResponse
	if err := json.NewDecoder(proc.Stdout()).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	proc.Wait() //nolint:errcheck,gosec // test cleanup
	proc.Cleanup()
	return resp
}

func integrationTestManager(t *testing.T) *Manager {
	t.Helper()
	cfg := config.SandboxConfig{
		Defaults: config.SandboxResourceLimits{
			MaxMemoryMB:   256,
			MaxCPUPct:     100,
			MaxPIDs:       32,
			MaxFileSizeMB: 10,
		},
	}
	dataDir := filepath.Join(t.TempDir(), "data")
	mgr, err := NewManager(cfg, "", dataDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(mgr.Close)
	return mgr
}

func TestIntegration_Echo(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("sandbox integration tests require privileges")
	}

	bin := buildProbe(t)
	mgr := integrationTestManager(t)

	info := PluginInfo{
		Name:    "echo-test",
		Dir:     filepath.Dir(bin),
		Handler: filepath.Base(bin),
	}

	resp := runProbe(t, mgr, info, nil, probeRequest{Cmd: "echo", Msg: "hello sandbox"})
	if !resp.OK {
		t.Fatalf("echo failed: %s", resp.Error)
	}
	if resp.Data != "hello sandbox" {
		t.Errorf("echo data = %q, want %q", resp.Data, "hello sandbox")
	}
}

func TestIntegration_ReadOwnDir(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("sandbox integration tests require privileges")
	}

	bin := buildProbe(t)
	mgr := integrationTestManager(t)

	markerPath := filepath.Join(filepath.Dir(bin), "marker.txt")
	if err := os.WriteFile(markerPath, []byte("readable"), 0o600); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	info := PluginInfo{
		Name:    "read-own-dir",
		Dir:     filepath.Dir(bin),
		Handler: filepath.Base(bin),
	}

	resp := runProbe(t, mgr, info, nil, probeRequest{Cmd: "read_file", Path: markerPath})
	if !resp.OK {
		t.Fatalf("read own dir failed: %s", resp.Error)
	}
	if resp.Data != "readable" {
		t.Errorf("data = %q, want %q", resp.Data, "readable")
	}
}

func TestIntegration_CannotReadShadow(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("sandbox integration tests require privileges")
	}
	if runtime.GOOS != "linux" {
		t.Skip("Landlock test, Linux only")
	}

	bin := buildProbe(t)
	mgr := integrationTestManager(t)

	info := PluginInfo{
		Name:    "no-shadow",
		Dir:     filepath.Dir(bin),
		Handler: filepath.Base(bin),
	}

	resp := runProbe(t, mgr, info, nil, probeRequest{Cmd: "read_file", Path: "/etc/shadow"})
	if resp.OK {
		t.Error("sandboxed process should NOT be able to read /etc/shadow")
	}
}

func TestIntegration_CannotDialNetwork(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("sandbox integration tests require privileges")
	}

	bin := buildProbe(t)
	mgr := integrationTestManager(t)

	info := PluginInfo{
		Name:    "no-network",
		Dir:     filepath.Dir(bin),
		Handler: filepath.Base(bin),
	}

	resp := runProbe(t, mgr, info, nil, probeRequest{Cmd: "dial_tcp", Addr: "1.1.1.1:443"})
	if resp.OK {
		t.Error("sandboxed process should NOT be able to make TCP connections")
	}
}

func TestIntegration_WriteToTmpdir(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("sandbox integration tests require privileges")
	}

	bin := buildProbe(t)
	mgr := integrationTestManager(t)

	info := PluginInfo{
		Name:    "write-tmp",
		Dir:     filepath.Dir(bin),
		Handler: filepath.Base(bin),
	}

	tmpDir := mgr.TmpDir("write-tmp")
	testFile := filepath.Join(tmpDir, "test.txt")

	resp := runProbe(t, mgr, info, nil, probeRequest{Cmd: "write_file", Path: testFile, Data: "written"})
	if !resp.OK {
		t.Fatalf("write to tmpdir failed: %s", resp.Error)
	}

	data, err := os.ReadFile(testFile) //nolint:gosec // test verification
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != "written" {
		t.Errorf("data = %q, want %q", string(data), "written")
	}

	mgr.CleanupTmpDir("write-tmp")
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Error("tmpdir should be removed after cleanup")
	}
}

func TestIntegration_EnvPassthrough(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("sandbox integration tests require privileges")
	}

	bin := buildProbe(t)
	mgr := integrationTestManager(t)

	info := PluginInfo{
		Name:    "env-test",
		Dir:     filepath.Dir(bin),
		Handler: filepath.Base(bin),
	}

	env := map[string]string{"MY_TEST_VAR": "sandbox-value"}
	resp := runProbe(t, mgr, info, env, probeRequest{Cmd: "env", Key: "MY_TEST_VAR"})
	if !resp.OK {
		t.Fatalf("env failed: %s", resp.Error)
	}
	if resp.Data != "sandbox-value" {
		t.Errorf("env data = %q, want %q", resp.Data, "sandbox-value")
	}
}

func TestIntegration_TmpdirOverride(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("sandbox integration tests require privileges")
	}

	bin := buildProbe(t)
	mgr := integrationTestManager(t)

	info := PluginInfo{
		Name:    "tmpdir-test",
		Dir:     filepath.Dir(bin),
		Handler: filepath.Base(bin),
	}

	resp := runProbe(t, mgr, info, nil, probeRequest{Cmd: "tmpdir"})
	if !resp.OK {
		t.Fatalf("tmpdir failed: %s", resp.Error)
	}

	expected := mgr.TmpDir("tmpdir-test")
	if resp.Data != expected {
		t.Errorf("TMPDIR = %q, want %q", resp.Data, expected)
	}
}
