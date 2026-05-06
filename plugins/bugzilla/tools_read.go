package main

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/LeGambiArt/wtmcp/pkg/handler"
)

// --- bugzilla_search ---

type searchParams struct {
	Query         string `json:"query"`
	Status        string `json:"status"`
	Product       string `json:"product"`
	Component     string `json:"component"`
	AssignedTo    string `json:"assigned_to"`
	IncludeFields string `json:"include_fields"`
	MaxResults    int    `json:"max_results"`
	Offset        int    `json:"offset"`
	Brief         *bool  `json:"brief"`
}

func toolSearch(params, _ json.RawMessage) (any, error) {
	var p searchParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.MaxResults <= 0 {
		p.MaxResults = defaultSearchLimit
	}
	p.MaxResults = capLimit(p.MaxResults, maxLimit)

	query := map[string]any{
		"limit":  p.MaxResults,
		"offset": p.Offset,
	}

	if p.Query != "" {
		query["quicksearch"] = p.Query
	}
	if p.Status != "" {
		query["status"] = p.Status
	}
	if p.Product != "" {
		query["product"] = p.Product
	}
	if p.Component != "" {
		query["component"] = p.Component
	}
	if p.AssignedTo != "" {
		query["assigned_to"] = p.AssignedTo
	}
	if p.IncludeFields != "" && !isBrief(p.Brief) {
		query["include_fields"] = p.IncludeFields
	}

	resp, err := plug.HTTP("GET", "/rest/bug", handler.WithQuery(query))
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var envelope struct {
		Bugs []map[string]any `json:"bugs"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	bugs := envelope.Bugs
	if isBrief(p.Brief) {
		shaped := make([]map[string]any, len(bugs))
		for i, b := range bugs {
			shaped[i] = bugBrief(b)
		}
		bugs = shaped
	}

	return map[string]any{
		"bugs":     bugs,
		"count":    len(bugs),
		"has_more": len(bugs) == p.MaxResults,
		"offset":   p.Offset,
	}, nil
}

// --- bugzilla_get_bugs ---

type getBugsParams struct {
	BugIDs        string `json:"bug_ids"`
	IncludeFields string `json:"include_fields"`
	Brief         *bool  `json:"brief"`
}

func toolGetBugs(params, _ json.RawMessage) (any, error) {
	var p getBugsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	ids, err := validateBugIDs(p.BugIDs)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
	}

	query := map[string]any{
		"id": toAnySlice(ids),
	}
	if p.IncludeFields != "" && !isBrief(p.Brief) {
		query["include_fields"] = p.IncludeFields
	}

	resp, err := plug.HTTP("GET", "/rest/bug", handler.WithQuery(query))
	if err != nil {
		return nil, fmt.Errorf("get bugs: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var envelope struct {
		Bugs []map[string]any `json:"bugs"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	bugs := envelope.Bugs
	if isBrief(p.Brief) {
		shaped := make([]map[string]any, len(bugs))
		for i, b := range bugs {
			shaped[i] = bugBrief(b)
		}
		bugs = shaped
	}

	return map[string]any{
		"bugs":  bugs,
		"count": len(bugs),
	}, nil
}

// --- bugzilla_get_comments ---

type getCommentsParams struct {
	BugID          any    `json:"bug_id"`
	IncludePrivate bool   `json:"include_private"`
	NewSince       string `json:"new_since"`
	MaxResults     int    `json:"max_results"`
}

func toolGetComments(params, _ json.RawMessage) (any, error) {
	var p getCommentsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	bugID, err := parseBugID(p.BugID)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
	}

	if p.MaxResults <= 0 {
		p.MaxResults = defaultCommentLimit
	}

	query := map[string]any{}
	if p.NewSince != "" {
		ts, err := parseTime(p.NewSince)
		if err != nil {
			return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
		}
		query["new_since"] = ts
	}

	path := fmt.Sprintf("/rest/bug/%d/comment", bugID)
	resp, err := plug.HTTP("GET", path, handler.WithQuery(query))
	if err != nil {
		return nil, fmt.Errorf("get comments: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	// Response envelope: {"bugs": {"<id>": {"comments": [...]}}}
	var envelope struct {
		Bugs map[string]struct {
			Comments []map[string]any `json:"comments"`
		} `json:"bugs"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	idStr := strconv.Itoa(bugID)
	allComments := envelope.Bugs[idStr].Comments

	if !p.IncludePrivate {
		var filtered []map[string]any
		for _, c := range allComments {
			if isPrivate, _ := c["is_private"].(bool); !isPrivate {
				filtered = append(filtered, c)
			}
		}
		allComments = filtered
	}

	// Client-side truncation: return most recent N
	truncated := len(allComments) > p.MaxResults
	if truncated {
		allComments = allComments[len(allComments)-p.MaxResults:]
	}

	return map[string]any{
		"comments":  allComments,
		"count":     len(allComments),
		"bug_id":    bugID,
		"truncated": truncated,
	}, nil
}

// --- bugzilla_get_history ---

type getHistoryParams struct {
	BugID    any    `json:"bug_id"`
	NewSince string `json:"new_since"`
}

func toolGetHistory(params, _ json.RawMessage) (any, error) {
	var p getHistoryParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	bugID, err := parseBugID(p.BugID)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
	}

	query := map[string]any{}
	if p.NewSince != "" {
		ts, err := parseTime(p.NewSince)
		if err != nil {
			return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
		}
		query["new_since"] = ts
	}

	path := fmt.Sprintf("/rest/bug/%d/history", bugID)
	resp, err := plug.HTTP("GET", path, handler.WithQuery(query))
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	// Response envelope: {"bugs": [{"id": N, "history": [...]}]}
	var envelope struct {
		Bugs []struct {
			History []map[string]any `json:"history"`
		} `json:"bugs"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var history []map[string]any
	if len(envelope.Bugs) > 0 {
		history = envelope.Bugs[0].History
	}

	return map[string]any{
		"history": history,
		"count":   len(history),
		"bug_id":  bugID,
	}, nil
}

// --- stubs for tools implemented in later commits ---

func toolGetAttachments(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_attachments not yet implemented"}
}

func toolDownloadAttachment(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_download_attachment not yet implemented"}
}

func toolWhoami(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_whoami not yet implemented"}
}

func toolGetUser(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_user not yet implemented"}
}

func toolGetProducts(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_products not yet implemented"}
}

func toolGetFields(_ /* params */, _ /* config */ json.RawMessage) (any, error) {
	return nil, &handler.Error{Code: "not_implemented", Message: "bugzilla_get_fields not yet implemented"}
}

func toolFlushCache(_, _ json.RawMessage) (any, error) {
	count, err := plug.CacheFlush()
	if err != nil {
		return nil, fmt.Errorf("flush cache: %w", err)
	}
	return map[string]any{"flushed": count}, nil
}

// toAnySlice converts []int to []any for WithQuery.
func toAnySlice(ids []int) []any {
	result := make([]any, len(ids))
	for i, id := range ids {
		result[i] = id
	}
	return result
}
