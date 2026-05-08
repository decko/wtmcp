package main

import (
	"path/filepath"
	"regexp"
)

var reUnsafeFileChars = regexp.MustCompile(`[<>:"/\\|?*]`)

// writeExportFile writes exported content via the core's file I/O service.
func writeExportFile(title, outputPath, content string) (string, error) {
	if outputPath == "" {
		safeTitle := reUnsafeFileChars.ReplaceAllString(title, "_")
		safeTitle = filepath.Base(safeTitle)
		outputPath = safeTitle + ".md"
	} else if filepath.IsAbs(outputPath) {
		outputPath = filepath.Base(outputPath)
	}

	return plug.FileWrite(outputPath, []byte(content))
}
