package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var reUnsafeFileChars = regexp.MustCompile(`[<>:"/\\|?*]`)

// saveExportFile saves exported content to a local file under outputDir.
func saveExportFile(title, outputPath, content string) (string, error) {
	if outputDir == "" {
		return "", fmt.Errorf("save requires a configured output directory")
	}

	absBase, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("resolve output dir: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(absBase); err == nil {
		absBase = resolved
	}
	baseDir := absBase

	if outputPath == "" {
		safeTitle := reUnsafeFileChars.ReplaceAllString(title, "_")
		safeTitle = filepath.Base(safeTitle)
		outputPath = filepath.Join(baseDir, safeTitle+".md")
	} else if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(baseDir, outputPath)
	}

	absOutput, err := filepath.Abs(outputPath)
	if err != nil {
		return "", fmt.Errorf("resolve output path: %w", err)
	}
	if !strings.HasPrefix(absOutput, absBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("output path escapes base directory: %s", outputPath)
	}

	dir := filepath.Dir(absOutput)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("resolve output dir: %w", err)
	}
	finalOutput := filepath.Join(resolvedDir, filepath.Base(absOutput))
	if !strings.HasPrefix(finalOutput, absBase+string(os.PathSeparator)) {
		return "", fmt.Errorf("output path escapes base directory after resolution: %s", outputPath)
	}

	if err := os.WriteFile(finalOutput, []byte(content), 0o600); err != nil {
		return "", err
	}

	return finalOutput, nil
}
