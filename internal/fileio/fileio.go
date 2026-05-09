// Package fileio provides secure file I/O for the core's file_write
// and file_read services. All plugin file operations are mediated
// through this package — plugins never write to outputDir directly.
//
// Security properties:
//   - Path confinement via symlink-aware prefix checks
//   - Atomic writes via temp file + fsync + rename
//   - source_path validation via O_NOFOLLOW + Fstat + Nlink check
//   - Size limits, encoding validation, permission enforcement
package fileio

import (
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// WriteRequest holds the parameters for a file write operation.
type WriteRequest struct {
	Path        string // relative path under outputDir
	Content     string // inline content (mutually exclusive with SourcePath)
	HasContent  bool   // true if Content was explicitly provided (distinguishes "" from absent)
	SourcePath  string // temp-file handoff path (mutually exclusive with Content)
	Encoding    string // "text" (default) or "base64"
	Permissions string // octal string, default "0600"
	Mkdir       *bool  // create parent dirs, default true
}

// ReadRequest holds the parameters for a file read operation.
type ReadRequest struct {
	Path     string // relative path under outputDir
	Encoding string // "text" (default) or "base64"
}

// WriteResult holds the result of a successful file write.
type WriteResult struct {
	Path string // absolute resolved path
	Size int64  // bytes written
}

// ReadResult holds the result of a successful file read.
type ReadResult struct {
	Content string // file content (text or base64-encoded)
	Path    string // absolute resolved path
}

// Config holds the configuration for file I/O operations.
type Config struct {
	OutputDir string // per-plugin output directory (EvalSymlinks-resolved)
	TmpDir    string // per-plugin tmpdir (EvalSymlinks-resolved, for source_path)
	SizeLimit int64  // max file size in bytes (default: 50 MB)
}

const (
	defaultSizeLimit = 50 * 1024 * 1024 // 50 MB
	defaultFileMode  = 0o600
	dirMode          = 0o700
	maxFilenameLen   = 255
	tempFilePrefix   = "wtmcp-fw-"
)

// Test hooks for deterministic TOCTOU and lifecycle testing.
// Test-only hooks — nil in production. Tests set them to inject
// behavior between critical steps for TOCTOU race testing.
// Exported because _test.go files need cross-package access.
// Do not set these outside of test code.
// Tests using these must NOT use t.Parallel().
var (
	TestHookAfterMkdir          func() // fires between MkdirAll and post-mkdir EvalSymlinks
	TestHookAfterSync           func() // fires between f.Sync() and os.Rename
	TestHookBeforeSourceCleanup func() // fires before os.Remove of source file
)

// WriteFile writes content to a file under outputDir with path
// confinement, atomic writes, and permission enforcement.
func WriteFile(cfg Config, req WriteRequest) (*WriteResult, error) {
	if cfg.OutputDir == "" {
		return nil, fmt.Errorf("no output directory configured")
	}
	sizeLimit := cfg.SizeLimit
	if sizeLimit <= 0 {
		sizeLimit = defaultSizeLimit
	}

	// Validate mutual exclusion. HasContent distinguishes "content
	// was explicitly provided as empty string" from "content field
	// was absent." This allows writing zero-byte files via inline
	// content.
	hasContent := req.HasContent || req.Content != ""
	hasSource := req.SourcePath != ""
	if hasContent && hasSource {
		return nil, fmt.Errorf("cannot specify both content and source_path")
	}
	if !hasContent && !hasSource {
		return nil, fmt.Errorf("content or source_path is required")
	}

	// Validate target path and permissions FIRST — before reading
	// source content. This ensures source_path files are not deleted
	// if path validation fails (preserved for retry).
	finalPath, err := resolvePath(cfg.OutputDir, req.Path, req.Mkdir)
	if err != nil {
		return nil, err
	}

	mode, err := parsePermissions(req.Permissions)
	if err != nil {
		return nil, err
	}

	// Resolve content from source_path or inline.
	var data []byte
	var sourceCleanupPath string
	if hasSource {
		data, sourceCleanupPath, err = readSourcePath(cfg.TmpDir, req.SourcePath, sizeLimit)
		if err != nil {
			return nil, fmt.Errorf("source_path: %w", err)
		}
	} else {
		data, err = decodeContent(req.Content, req.Encoding, sizeLimit)
		if err != nil {
			return nil, err
		}
	}

	// Atomic write.
	if err := atomicWrite(finalPath, data, mode); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	// Cleanup source file AFTER successful write only.
	if sourceCleanupPath != "" {
		if TestHookBeforeSourceCleanup != nil {
			TestHookBeforeSourceCleanup()
		}
		_ = os.Remove(sourceCleanupPath)
	}

	size := int64(len(data))
	log.Printf("fileio: write %s (%d bytes)", filepath.Base(finalPath), size)

	return &WriteResult{Path: finalPath, Size: size}, nil
}

// ReadFile reads a file from outputDir with path confinement.
func ReadFile(cfg Config, req ReadRequest) (*ReadResult, error) {
	if cfg.OutputDir == "" {
		return nil, fmt.Errorf("no output directory configured")
	}
	sizeLimit := cfg.SizeLimit
	if sizeLimit <= 0 {
		sizeLimit = defaultSizeLimit
	}

	// Validate and resolve the path (read-specific: full EvalSymlinks
	// on the target since it must exist).
	finalPath, err := resolveReadPath(cfg.OutputDir, req.Path)
	if err != nil {
		return nil, err
	}

	// Check size before reading to prevent unbounded allocation.
	info, err := os.Stat(finalPath)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	if info.Size() > sizeLimit {
		return nil, fmt.Errorf("exceeds size limit")
	}

	// Read the file with bounded size (defense against concurrent growth).
	f, err := os.Open(finalPath) //nolint:gosec // path validated by resolveReadPath
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	defer f.Close() //nolint:errcheck,gosec

	data, err := io.ReadAll(io.LimitReader(f, sizeLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if int64(len(data)) > sizeLimit {
		return nil, fmt.Errorf("exceeds size limit")
	}

	// Encode the content for the response (case-sensitive matching).
	var content string
	encoding := req.Encoding
	if encoding == "" {
		encoding = "text"
	}
	switch encoding {
	case "text":
		content = string(data)
	case "base64":
		content = base64.StdEncoding.EncodeToString(data)
	default:
		return nil, fmt.Errorf("unsupported encoding: %q", req.Encoding)
	}

	log.Printf("fileio: read %s (%d bytes)", filepath.Base(finalPath), len(data))

	return &ReadResult{Content: content, Path: finalPath}, nil
}

// resolvePath validates and resolves a relative path under outputDir.
// This implements the 10-step path validation algorithm from the plan.
func resolvePath(outputDir, path string, mkdir *bool) (string, error) {
	// Step 0: Reject empty and degenerate paths.
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if path == "." || path == ".." {
		return "", fmt.Errorf("invalid path")
	}

	// Step 1: Reject null bytes.
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("path contains null byte")
	}

	// Step 2: Reject absolute paths and paths that clean to ".".
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("path escapes allowed directory")
	}
	if filepath.Clean(path) == "." {
		return "", fmt.Errorf("invalid path")
	}

	// Step 4: Resolve outputDir base. If it doesn't exist, create it
	// lazily (no eager creation at startup since Phase 5).
	resolvedBase, err := filepath.EvalSymlinks(outputDir)
	if err != nil && os.IsNotExist(err) {
		if mkErr := os.MkdirAll(outputDir, dirMode); mkErr != nil {
			return "", fmt.Errorf("create output directory failed")
		}
		resolvedBase, err = filepath.EvalSymlinks(outputDir)
	}
	if err != nil {
		return "", fmt.Errorf("resolve output directory failed")
	}

	// Step 3: Clean and join using resolved base — makes the function
	// self-contained regardless of whether the caller pre-resolved.
	cleaned := filepath.Clean(filepath.Join(resolvedBase, path))

	// Step 5: Lexical prefix check (pre-mkdir).
	if !strings.HasPrefix(cleaned, resolvedBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes allowed directory")
	}

	// Step 6: Sanitize final filename component.
	filename := filepath.Base(cleaned)
	if err := validateFilename(filename); err != nil {
		return "", err
	}

	// Step 7: Create parent directories (if mkdir enabled).
	parentDir := filepath.Dir(cleaned)
	if mkdir == nil || *mkdir {
		if err := os.MkdirAll(parentDir, dirMode); err != nil {
			return "", fmt.Errorf("create parent directory failed")
		}
	}

	if TestHookAfterMkdir != nil {
		TestHookAfterMkdir()
	}

	// Step 8: Post-mkdir symlink re-check.
	resolvedParent, err := filepath.EvalSymlinks(parentDir)
	if err != nil {
		return "", fmt.Errorf("resolve parent directory failed")
	}
	if !strings.HasPrefix(resolvedParent, resolvedBase+string(os.PathSeparator)) && resolvedParent != resolvedBase {
		return "", fmt.Errorf("path escapes allowed directory")
	}

	// Step 9: Construct final path.
	return filepath.Join(resolvedParent, filename), nil
}

// resolveReadPath validates a path for reading. The target file must
// exist, so we can EvalSymlinks on the full path.
func resolveReadPath(outputDir, path string) (string, error) {
	// Steps 0-2: same validation as write path.
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if path == "." || path == ".." {
		return "", fmt.Errorf("invalid path")
	}
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("path contains null byte")
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("path escapes allowed directory")
	}
	if filepath.Clean(path) == "." {
		return "", fmt.Errorf("invalid path")
	}

	// Resolve base. If outputDir doesn't exist, no files can exist
	// there — return "file not found" rather than a confusing
	// resolution error.
	resolvedBase, err := filepath.EvalSymlinks(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found")
		}
		return "", fmt.Errorf("resolve output directory failed")
	}
	cleaned := filepath.Clean(filepath.Join(resolvedBase, path))

	// Resolve the full path (must exist for reads).
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found")
		}
		return "", fmt.Errorf("resolve path failed")
	}

	// Prefix check on the fully resolved path.
	if !strings.HasPrefix(resolved, resolvedBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes allowed directory")
	}

	// Verify it is a regular file.
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file")
	}

	return resolved, nil
}

// validateFilename checks the final filename component for dangerous
// characters, reserved names, and length.
func validateFilename(name string) error {
	if len(name) > maxFilenameLen {
		return fmt.Errorf("filename too long")
	}

	for _, r := range name {
		if r < 0x20 || r == 0x7F {
			return fmt.Errorf("filename contains control character")
		}
		if unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("filename contains invisible character")
		}
		if r == ':' {
			return fmt.Errorf("filename contains colon")
		}
	}

	// Reject trailing dots and spaces (Windows strips them silently).
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("filename has trailing dot or space")
	}

	// Reject Windows reserved device names.
	if isWindowsReservedName(name) {
		return fmt.Errorf("filename is a reserved name")
	}

	return nil
}

// isWindowsReservedName checks for Windows reserved device names
// (CON, PRN, AUX, NUL, COM1-COM9, LPT1-LPT9), with or without
// extensions (e.g., CON.txt).
func isWindowsReservedName(name string) bool {
	base := strings.ToUpper(name)
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) {
		if base[3] >= '0' && base[3] <= '9' {
			return true
		}
	}
	return false
}

// decodeContent decodes inline content based on encoding type.
func decodeContent(content, encoding string, sizeLimit int64) ([]byte, error) {
	if encoding == "" {
		encoding = "text"
	}

	switch encoding {
	case "text":
		data := []byte(content)
		if !utf8.Valid(data) {
			return nil, fmt.Errorf("content is not valid UTF-8 (use encoding \"base64\" for binary data)")
		}
		if int64(len(data)) > sizeLimit {
			return nil, fmt.Errorf("exceeds size limit")
		}
		return data, nil

	case "base64":
		estimatedSize := base64.StdEncoding.DecodedLen(len(content))
		if int64(estimatedSize) > sizeLimit+4 {
			return nil, fmt.Errorf("exceeds size limit")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(content)
		if err != nil {
			return nil, fmt.Errorf("invalid base64: %w", err)
		}
		if int64(len(data)) > sizeLimit {
			return nil, fmt.Errorf("exceeds size limit")
		}
		return data, nil

	default:
		return nil, fmt.Errorf("unsupported encoding: %q", encoding)
	}
}

// parsePermissions parses an octal permission string.
func parsePermissions(s string) (os.FileMode, error) {
	if s == "" {
		return defaultFileMode, nil
	}

	mode, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid permissions %q: must be octal", s)
	}
	if mode > 0o777 {
		return 0, fmt.Errorf("invalid permissions %q: setuid/setgid/sticky not allowed", s)
	}
	if mode&0o007 != 0 {
		return 0, fmt.Errorf("invalid permissions %q: world access not allowed", s)
	}
	if mode&0o400 == 0 {
		return 0, fmt.Errorf("invalid permissions %q: owner-read required", s)
	}
	return os.FileMode(mode), nil
}

// atomicWrite writes data to path using temp + chmod + fsync + rename.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, tempFilePrefix+"*.tmp")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	defer os.Remove(f.Name()) //nolint:errcheck // cleanup on failure; harmless ENOENT after rename

	if err := f.Chmod(mode); err != nil {
		f.Close() //nolint:errcheck,gosec
		return fmt.Errorf("chmod temp: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		f.Close() //nolint:errcheck,gosec
		return fmt.Errorf("write temp: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close() //nolint:errcheck,gosec
		return fmt.Errorf("fsync: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}

	if TestHookAfterSync != nil {
		TestHookAfterSync()
	}

	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// CleanupStaleTempFiles removes stale temp files from a directory.
// Called at server startup to clean up files left behind by crashes.
func CleanupStaleTempFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), tempFilePrefix) || !strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoffTime()) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func cutoffTime() time.Time {
	return time.Now().Add(-1 * time.Hour)
}
