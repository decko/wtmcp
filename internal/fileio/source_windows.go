//go:build windows

package fileio

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// readSourcePath reads content from a source file in the plugin's
// tmpdir. Windows fallback — no Nlink check (syscall.Stat_t not
// available), no O_NOFOLLOW equivalent, stat-based validation only.
//
// This is the weakest implementation. Production deployments should
// use Linux with the sandbox enabled.
func readSourcePath(resolvedTmpDir, sourcePath string, sizeLimit int64) ([]byte, string, error) {
	if resolvedTmpDir == "" {
		return nil, "", fmt.Errorf("no tmpdir configured")
	}

	// Resolve and check containment.
	resolved, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return nil, "", fmt.Errorf("resolve source: %w", err)
	}
	if !strings.HasPrefix(resolved, resolvedTmpDir+string(os.PathSeparator)) {
		return nil, "", fmt.Errorf("source_path must be within plugin tmpdir")
	}

	// Check file type and size (no Nlink on Windows).
	info, err := os.Lstat(resolved)
	if err != nil {
		return nil, "", fmt.Errorf("stat source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("not a regular file")
	}
	if info.Size() > sizeLimit {
		return nil, "", fmt.Errorf("exceeds size limit")
	}

	// Read with bounded size.
	f, err := os.Open(resolved) //nolint:gosec // path validated above
	if err != nil {
		return nil, "", fmt.Errorf("open source: %w", err)
	}
	defer f.Close() //nolint:errcheck,gosec

	data, err := io.ReadAll(io.LimitReader(f, sizeLimit+1))
	if err != nil {
		return nil, "", fmt.Errorf("read source: %w", err)
	}
	if int64(len(data)) > sizeLimit {
		return nil, "", fmt.Errorf("exceeds size limit")
	}

	return data, sourcePath, nil
}
