//go:build linux

package fileio

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// readSourcePath reads content from a source file in the plugin's
// tmpdir using fd-based validation to eliminate TOCTOU races.
// Returns the data and the verified cleanup path (for the caller to
// delete after a successful write). Does NOT delete the source —
// the caller is responsible for cleanup to ensure the source is
// preserved on write failure (retry).
func readSourcePath(resolvedTmpDir, sourcePath string, sizeLimit int64) ([]byte, string, error) {
	if resolvedTmpDir == "" {
		return nil, "", fmt.Errorf("no tmpdir configured")
	}

	// Step 1: Open with O_NOFOLLOW.
	fd, err := unix.Open(sourcePath, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, "", fmt.Errorf("open: %w", err)
	}

	// Wrap in os.File immediately — fd lifecycle belongs to goFile.
	goFile := os.NewFile(uintptr(fd), sourcePath) //nolint:gosec // fd from unix.Open is always valid
	defer goFile.Close()                          //nolint:errcheck,gosec // best effort cleanup

	// Step 2: Fstat the opened fd.
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return nil, "", fmt.Errorf("fstat: %w", err)
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, "", fmt.Errorf("not a regular file")
	}
	if st.Size > sizeLimit {
		return nil, "", fmt.Errorf("exceeds size limit")
	}
	if st.Nlink > 1 {
		return nil, "", fmt.Errorf("hardlink detected (nlink=%d)", st.Nlink)
	}

	// Step 3: Verify containment via /proc/self/fd.
	// Use readlink only for the containment check — the cleanup path
	// is the original sourcePath (already validated at open time via
	// O_NOFOLLOW). This avoids the " (deleted)" suffix ambiguity
	// where a file literally named "foo (deleted)" would have
	// TrimSuffix produce the wrong cleanup target.
	realPath, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return nil, "", fmt.Errorf("readlink fd: %w", err)
	}
	checkPath := strings.TrimSuffix(realPath, " (deleted)")
	if !strings.HasPrefix(checkPath, resolvedTmpDir+string(os.PathSeparator)) {
		return nil, "", fmt.Errorf("source_path must be within plugin tmpdir")
	}

	// Step 4: Read content from fd (bounded).
	data, err := io.ReadAll(io.LimitReader(goFile, sizeLimit+1))
	if err != nil {
		return nil, "", fmt.Errorf("read: %w", err)
	}
	if int64(len(data)) > sizeLimit {
		return nil, "", fmt.Errorf("exceeds size limit")
	}

	// Return the original sourcePath for cleanup — it was validated
	// at open time (O_NOFOLLOW) and the containment check above
	// confirmed the inode is within tmpdir.
	return data, sourcePath, nil
}
