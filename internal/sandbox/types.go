package sandbox

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/LeGambiArt/wtmcp/internal/config"
)

// PluginInfo holds the plugin metadata needed to build a sandbox
// profile. Avoids importing internal/plugin (circular dependency).
type PluginInfo struct {
	Name            string
	Dir             string
	Handler         string
	CredentialGroup string
	SessionDir      string // User's project directory (read access)
	OutputDir       string // Per-plugin output directory (write access)
}

// ResourceUsage holds cgroup v2 resource usage statistics.
type ResourceUsage struct {
	MemoryCurrentBytes int64
	MemoryPeakBytes    int64
	CPUUsageSeconds    float64
	PIDCount           int64
	IOReadBytes        int64
	IOWriteBytes       int64
}

// base holds shared fields and methods for the sandbox Manager,
// embedded by both the real (arapuca) and stub implementations.
type base struct {
	cfg     config.SandboxConfig
	credDir string
	dataDir string
}

// TmpDir returns the per-plugin temporary directory path.
func (b *base) TmpDir(pluginName string) string {
	return filepath.Join(os.TempDir(), "wtmcp", pluginName)
}

// DataDir returns the per-plugin persistent data directory path.
func (b *base) DataDir(pluginName string) string {
	return filepath.Join(b.dataDir, pluginName)
}

// PrepareDirs creates the per-plugin tmpdir, datadir, and outputDir
// with 0700 permissions. The outputDir is created only if set on
// info. Landlock needs directories to exist to create path_beneath
// rules. Safe to call multiple times.
func (b *base) PrepareDirs(info PluginInfo) (tmpDir, dataDir string, err error) {
	tmpDir = b.TmpDir(info.Name)
	dataDir = b.DataDir(info.Name)

	if err := os.MkdirAll(tmpDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create tmpdir: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", "", fmt.Errorf("create datadir: %w", err)
	}
	if info.OutputDir != "" {
		if err := os.MkdirAll(info.OutputDir, 0o700); err != nil {
			return "", "", fmt.Errorf("create output dir: %w", err)
		}
	}
	return tmpDir, dataDir, nil
}

// CleanupTmpDir removes the per-plugin tmpdir.
func (b *base) CleanupTmpDir(pluginName string) {
	if err := os.RemoveAll(b.TmpDir(pluginName)); err != nil {
		log.Printf("[%s] cleanup tmpdir: %v", pluginName, err)
	}
}

func isPython(handler string) bool {
	return strings.HasSuffix(handler, ".py")
}
