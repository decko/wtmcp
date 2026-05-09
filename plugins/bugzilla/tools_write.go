package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/LeGambiArt/wtmcp/pkg/handler"
)

// --- bugzilla_create_bug ---

type createBugParams struct {
	Product     string   `json:"product"`
	Component   string   `json:"component"`
	Summary     string   `json:"summary"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"`
	Severity    string   `json:"severity"`
	AssignedTo  string   `json:"assigned_to"`
	CC          []string `json:"cc"`
	DryRun      *bool    `json:"dry_run"`
}

func toolCreateBug(params, _ json.RawMessage) (any, error) {
	var p createBugParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.Product == "" || p.Component == "" || p.Summary == "" || p.Version == "" {
		return nil, &handler.Error{
			Code:    "validation_error",
			Message: "product, component, summary, and version are required",
		}
	}

	body := map[string]any{
		"product":   p.Product,
		"component": p.Component,
		"summary":   p.Summary,
		"version":   p.Version,
	}
	if p.Description != "" {
		body["description"] = p.Description
	}
	if p.Priority != "" {
		body["priority"] = p.Priority
	}
	if p.Severity != "" {
		body["severity"] = p.Severity
	}
	if p.AssignedTo != "" {
		body["assigned_to"] = p.AssignedTo
	}
	if len(p.CC) > 0 {
		body["cc"] = p.CC
	}

	if isDryRun(p.DryRun) {
		return dryRunPreview("POST", "/rest/bug", body), nil
	}

	resp, err := plug.HTTP("POST", "/rest/bug", handler.WithBody(body))
	if err != nil {
		return nil, fmt.Errorf("create bug: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// --- bugzilla_update_bug ---

type updateBugParams struct {
	BugID           any      `json:"bug_id"`
	Status          string   `json:"status"`
	Resolution      string   `json:"resolution"`
	AssignedTo      string   `json:"assigned_to"`
	Priority        string   `json:"priority"`
	Severity        string   `json:"severity"`
	Product         string   `json:"product"`
	Component       string   `json:"component"`
	Version         string   `json:"version"`
	TargetMilestone string   `json:"target_milestone"`
	Whiteboard      string   `json:"whiteboard"`
	QAContact       string   `json:"qa_contact"`
	Comment         string   `json:"comment"`
	CCAdd           []string `json:"cc_add"`
	CCRemove        []string `json:"cc_remove"`
	KeywordsAdd     []string `json:"keywords_add"`
	KeywordsRemove  []string `json:"keywords_remove"`
	DependsOnAdd    []string `json:"depends_on_add"`
	DependsOnRemove []string `json:"depends_on_remove"`
	BlocksAdd       []string `json:"blocks_add"`
	BlocksRemove    []string `json:"blocks_remove"`
	DryRun          *bool    `json:"dry_run"`
}

func toolUpdateBug(params, _ json.RawMessage) (any, error) {
	var p updateBugParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	bugID, err := parseBugID(p.BugID)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
	}

	p.Status = strings.TrimSpace(p.Status)
	p.Resolution = strings.TrimSpace(p.Resolution)

	if p.Status == "CLOSED" && p.Resolution == "" {
		return nil, &handler.Error{
			Code:    "validation_error",
			Message: "resolution is required when setting status to CLOSED",
		}
	}

	body := map[string]any{}

	if p.Status != "" {
		body["status"] = p.Status
	}
	if p.Resolution != "" {
		body["resolution"] = p.Resolution
	}
	if p.AssignedTo != "" {
		body["assigned_to"] = p.AssignedTo
	}
	if p.Priority != "" {
		body["priority"] = p.Priority
	}
	if p.Severity != "" {
		body["severity"] = p.Severity
	}
	if p.Product != "" {
		body["product"] = p.Product
	}
	if p.Component != "" {
		body["component"] = p.Component
	}
	if p.Version != "" {
		body["version"] = p.Version
	}
	if p.TargetMilestone != "" {
		body["target_milestone"] = p.TargetMilestone
	}
	if p.Whiteboard != "" {
		body["whiteboard"] = p.Whiteboard
	}
	if p.QAContact != "" {
		body["qa_contact"] = p.QAContact
	}
	if p.Comment != "" {
		body["comment"] = map[string]any{"body": p.Comment}
	}

	if len(p.CCAdd) > 0 || len(p.CCRemove) > 0 {
		cc := map[string]any{}
		if len(p.CCAdd) > 0 {
			cc["add"] = p.CCAdd
		}
		if len(p.CCRemove) > 0 {
			cc["remove"] = p.CCRemove
		}
		body["cc"] = cc
	}

	if len(p.KeywordsAdd) > 0 || len(p.KeywordsRemove) > 0 {
		kw := map[string]any{}
		if len(p.KeywordsAdd) > 0 {
			kw["add"] = p.KeywordsAdd
		}
		if len(p.KeywordsRemove) > 0 {
			kw["remove"] = p.KeywordsRemove
		}
		body["keywords"] = kw
	}

	if len(p.DependsOnAdd) > 0 || len(p.DependsOnRemove) > 0 {
		deps := map[string]any{}
		if len(p.DependsOnAdd) > 0 {
			ids, err := parseIntIDs(p.DependsOnAdd)
			if err != nil {
				return nil, &handler.Error{Code: "validation_error", Message: "depends_on_add: " + err.Error()}
			}
			deps["add"] = ids
		}
		if len(p.DependsOnRemove) > 0 {
			ids, err := parseIntIDs(p.DependsOnRemove)
			if err != nil {
				return nil, &handler.Error{Code: "validation_error", Message: "depends_on_remove: " + err.Error()}
			}
			deps["remove"] = ids
		}
		body["depends_on"] = deps
	}

	if len(p.BlocksAdd) > 0 || len(p.BlocksRemove) > 0 {
		blocks := map[string]any{}
		if len(p.BlocksAdd) > 0 {
			ids, err := parseIntIDs(p.BlocksAdd)
			if err != nil {
				return nil, &handler.Error{Code: "validation_error", Message: "blocks_add: " + err.Error()}
			}
			blocks["add"] = ids
		}
		if len(p.BlocksRemove) > 0 {
			ids, err := parseIntIDs(p.BlocksRemove)
			if err != nil {
				return nil, &handler.Error{Code: "validation_error", Message: "blocks_remove: " + err.Error()}
			}
			blocks["remove"] = ids
		}
		body["blocks"] = blocks
	}

	if len(body) == 0 {
		return nil, &handler.Error{
			Code:    "validation_error",
			Message: "at least one field must be specified",
		}
	}

	path := fmt.Sprintf("/rest/bug/%d", bugID)

	if isDryRun(p.DryRun) {
		return dryRunPreview("PUT", path, body), nil
	}

	resp, err := plug.HTTP("PUT", path, handler.WithBody(body))
	if err != nil {
		return nil, fmt.Errorf("update bug: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// --- bugzilla_add_comment ---

type addCommentParams struct {
	BugID     any    `json:"bug_id"`
	Comment   string `json:"comment"`
	IsPrivate bool   `json:"is_private"`
	DryRun    *bool  `json:"dry_run"`
}

func toolAddComment(params, _ json.RawMessage) (any, error) {
	var p addCommentParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	bugID, err := parseBugID(p.BugID)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
	}

	if p.Comment == "" {
		return nil, &handler.Error{
			Code:    "validation_error",
			Message: "comment text is required",
		}
	}

	body := map[string]any{
		"comment":    p.Comment,
		"is_private": p.IsPrivate,
	}

	path := fmt.Sprintf("/rest/bug/%d/comment", bugID)

	if isDryRun(p.DryRun) {
		return dryRunPreview("POST", path, body), nil
	}

	resp, err := plug.HTTP("POST", path, handler.WithBody(body))
	if err != nil {
		return nil, fmt.Errorf("add comment: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// --- bugzilla_add_attachment ---

type addAttachmentParams struct {
	BugID       any    `json:"bug_id"`
	FilePath    string `json:"file_path"`
	Summary     string `json:"summary"`
	ContentType string `json:"content_type"`
	IsPrivate   bool   `json:"is_private"`
	DryRun      *bool  `json:"dry_run"`
}

func toolAddAttachment(params, _ json.RawMessage) (any, error) {
	var p addAttachmentParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	bugID, err := parseBugID(p.BugID)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
	}

	p.FilePath = strings.TrimSpace(p.FilePath)
	if p.FilePath == "" {
		return nil, &handler.Error{Code: "validation_error", Message: "file_path is required"}
	}

	p.Summary = strings.TrimSpace(p.Summary)
	if p.Summary == "" {
		return nil, &handler.Error{Code: "validation_error", Message: "summary is required"}
	}

	// Try reading from outputDir via core file I/O service first.
	// Normalize absolute paths by stripping the outputDir prefix
	// (users/LLMs may pass full paths from prior tool results).
	// Fall back to direct read from sessionDir (still in sandbox ReadPaths).
	var fileData []byte
	var resolvedPath string
	readPath := p.FilePath
	if filepath.IsAbs(readPath) && cfg.outputDir != "" {
		if rel, relErr := filepath.Rel(cfg.outputDir, readPath); relErr == nil && !strings.HasPrefix(rel, "..") {
			readPath = rel
		}
	}
	fileData, err = plug.FileRead(readPath, handler.WithReadEncoding("base64"))
	switch {
	case err == nil:
		resolvedPath = p.FilePath
	case cfg.sessionDir != "":
		resolved, readErr := confineRead(p.FilePath, cfg.sessionDir)
		if readErr != nil {
			return nil, &handler.Error{Code: "validation_error", Message: "file_path: " + readErr.Error()}
		}
		resolvedPath = resolved
		fileData, readErr = os.ReadFile(resolved) //nolint:gosec // path validated by confineRead
		if readErr != nil {
			return nil, fmt.Errorf("read file: %w", readErr)
		}
	default:
		return nil, &handler.Error{Code: "validation_error", Message: "file_path: " + err.Error()}
	}

	if len(fileData) > maxAttachBytes {
		return nil, &handler.Error{
			Code:    "too_large",
			Message: fmt.Sprintf("file exceeds %dMB limit (%d bytes)", maxAttachMB, len(fileData)),
		}
	}

	if p.ContentType == "" {
		p.ContentType = "application/octet-stream"
	}

	body := map[string]any{
		"ids":          []int{bugID},
		"file_name":    filepath.Base(resolvedPath),
		"summary":      p.Summary,
		"content_type": p.ContentType,
		"is_private":   p.IsPrivate,
	}

	path := fmt.Sprintf("/rest/bug/%d/attachment", bugID)

	if isDryRun(p.DryRun) {
		preview := dryRunPreview("POST", path, body)
		preview["file_path"] = resolvedPath
		preview["file_size"] = len(fileData)
		return preview, nil
	}

	body["data"] = base64Encode(string(fileData))

	resp, err := plug.HTTP("POST", path, handler.WithBody(body))
	if err != nil {
		return nil, fmt.Errorf("add attachment: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// --- bugzilla_update_attachment ---

type updateAttachmentParams struct {
	AttachmentID any    `json:"attachment_id"`
	Description  string `json:"description"`
	ContentType  string `json:"content_type"`
	IsObsolete   *bool  `json:"is_obsolete"`
	IsPrivate    *bool  `json:"is_private"`
	DryRun       *bool  `json:"dry_run"`
}

func toolUpdateAttachment(params, _ json.RawMessage) (any, error) {
	var p updateAttachmentParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	attID, err := parseBugID(p.AttachmentID)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
	}

	body := map[string]any{}

	if p.Description != "" {
		body["summary"] = p.Description
	}
	if p.ContentType != "" {
		body["content_type"] = p.ContentType
	}
	if p.IsObsolete != nil {
		body["is_obsolete"] = *p.IsObsolete
	}
	if p.IsPrivate != nil {
		body["is_private"] = *p.IsPrivate
	}

	if len(body) == 0 {
		return nil, &handler.Error{
			Code:    "validation_error",
			Message: "at least one field must be specified",
		}
	}

	path := fmt.Sprintf("/rest/bug/attachment/%d", attID)

	if isDryRun(p.DryRun) {
		return dryRunPreview("PUT", path, body), nil
	}

	resp, err := plug.HTTP("PUT", path, handler.WithBody(body))
	if err != nil {
		return nil, fmt.Errorf("update attachment: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}

// --- bugzilla_mark_duplicate ---

type markDuplicateParams struct {
	BugID       any    `json:"bug_id"`
	DuplicateOf any    `json:"duplicate_of"`
	Comment     string `json:"comment"`
	DryRun      *bool  `json:"dry_run"`
}

func toolMarkDuplicate(params, _ json.RawMessage) (any, error) {
	var p markDuplicateParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	bugID, err := parseBugID(p.BugID)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: "bug_id: " + err.Error()}
	}

	dupeOf, err := parseBugID(p.DuplicateOf)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: "duplicate_of: " + err.Error()}
	}

	if bugID == dupeOf {
		return nil, &handler.Error{
			Code:    "validation_error",
			Message: "bug cannot be a duplicate of itself",
		}
	}

	body := map[string]any{
		"status":     "CLOSED",
		"resolution": "DUPLICATE",
		"dupe_of":    dupeOf,
	}

	if p.Comment != "" {
		body["comment"] = map[string]any{"body": p.Comment}
	}

	path := fmt.Sprintf("/rest/bug/%d", bugID)

	if isDryRun(p.DryRun) {
		return dryRunPreview("PUT", path, body), nil
	}

	resp, err := plug.HTTP("PUT", path, handler.WithBody(body))
	if err != nil {
		return nil, fmt.Errorf("mark duplicate: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return result, nil
}
