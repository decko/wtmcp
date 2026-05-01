// google-drive handler is a persistent plugin for Google Drive.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"

	googleauth "github.com/LeGambiArt/wtmcp/internal/google"
	"github.com/LeGambiArt/wtmcp/pkg/handler"
)

var (
	driveSvc  *drive.Service
	outputDir string
)

func main() {
	p := handler.New()

	p.OnInit(func(cfgRaw json.RawMessage) error {
		var cfg map[string]string
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}

		credDir := cfg["_credentials_dir"]
		if credDir == "" {
			credDir = googleauth.CredentialsDir()
		}

		client, err := googleauth.NewHTTPClientFromDir(
			context.Background(),
			credDir,
			"token-drive.json",
			[]string{"https://www.googleapis.com/auth/drive.readonly"},
		)
		if err != nil {
			return fmt.Errorf("google auth: %w", err)
		}

		svc, err := drive.NewService(context.Background(), option.WithHTTPClient(client))
		if err != nil {
			return fmt.Errorf("drive service: %w", err)
		}
		driveSvc = svc
		outputDir = cfg["_output_dir"]
		return nil
	})

	p.Handle("drive_get_file_by_id", toolGetFileByID)
	p.Handle("drive_get_file_by_url", toolGetFileByURL)
	p.Handle("drive_extract_and_get_from_text", toolExtractAndGet)
	p.Handle("drive_export_google_doc_text", toolExportDocText)
	p.Handle("drive_export_google_sheet_csv", toolExportSheetCSV)
	p.Handle("drive_export_slides_pdf", toolExportSlidesPDF)
	p.Handle("drive_export_google_doc_markdown", toolExportDocMarkdown)
	p.Handle("drive_search_files", toolSearchFiles)
	p.Handle("drive_search_text", toolSearchText)

	if err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "handler: %v\n", err)
		os.Exit(1)
	}
}
