package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

// --- bugzilla_get_attachments ---

type getAttachmentsParams struct {
	BugID any `json:"bug_id"`
}

func toolGetAttachments(params, _ json.RawMessage) (any, error) {
	var p getAttachmentsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	bugID, err := parseBugID(p.BugID)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
	}

	path := fmt.Sprintf("/rest/bug/%d/attachment", bugID)
	query := map[string]any{
		"exclude_fields": "data",
	}

	resp, err := plug.HTTP("GET", path, handler.WithQuery(query))
	if err != nil {
		return nil, fmt.Errorf("get attachments: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	// Response envelope: {"bugs": {"<id>": [{att}, ...]}}
	var envelope struct {
		Bugs map[string][]map[string]any `json:"bugs"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	idStr := strconv.Itoa(bugID)
	attachments := envelope.Bugs[idStr]

	var shaped []map[string]any
	for _, att := range attachments {
		shaped = append(shaped, map[string]any{
			"id":            att["id"],
			"file_name":     att["file_name"],
			"content_type":  att["content_type"],
			"size":          att["size"],
			"creator":       att["creator"],
			"creation_time": att["creation_time"],
			"is_obsolete":   att["is_obsolete"],
			"summary":       att["summary"],
		})
	}

	return map[string]any{
		"attachments": shaped,
		"count":       len(shaped),
		"bug_id":      bugID,
	}, nil
}

// --- bugzilla_download_attachment ---

type downloadAttachmentParams struct {
	AttachmentID any `json:"attachment_id"`
}

func toolDownloadAttachment(params, _ json.RawMessage) (any, error) {
	var p downloadAttachmentParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	attID, err := parseBugID(p.AttachmentID)
	if err != nil {
		return nil, &handler.Error{Code: "validation_error", Message: err.Error()}
	}

	if cfg.outputDir == "" {
		return nil, &handler.Error{
			Code:    "no_output_dir",
			Message: "output directory not configured",
		}
	}

	path := fmt.Sprintf("/rest/bug/attachment/%d", attID)
	resp, err := plug.HTTP("GET", path)
	if err != nil {
		return nil, fmt.Errorf("download attachment: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	// Response envelope: {"attachments": {"<att_id>": {attachment_obj}}}
	var envelope struct {
		Attachments map[string]struct {
			Data        string `json:"data"`
			FileName    string `json:"file_name"`
			ContentType string `json:"content_type"`
			Size        int    `json:"size"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	idStr := strconv.Itoa(attID)
	att, ok := envelope.Attachments[idStr]
	if !ok {
		return nil, &handler.Error{
			Code:    "not_found",
			Message: fmt.Sprintf("attachment %d not found in response", attID),
		}
	}

	decoded, err := base64Decode(att.Data)
	if err != nil {
		return nil, fmt.Errorf("decode attachment data: %w", err)
	}

	if len(decoded) > maxAttachBytes {
		return nil, &handler.Error{
			Code:    "too_large",
			Message: fmt.Sprintf("attachment exceeds %dMB limit (%d bytes)", maxAttachMB, len(decoded)),
		}
	}

	filename := sanitizeFilename(att.FileName, attID)
	outPath := filepath.Join("bugzilla", "attachments", filename)

	resolvedPath, err := confineWrite(outPath, cfg.outputDir)
	if err != nil {
		return nil, fmt.Errorf("path confinement: %w", err)
	}

	if err := os.WriteFile(resolvedPath, decoded, 0o600); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	return map[string]any{
		"file_path":     resolvedPath,
		"file_name":     att.FileName,
		"size":          len(decoded),
		"content_type":  att.ContentType,
		"attachment_id": attID,
	}, nil
}

// --- bugzilla_whoami ---

func toolWhoami(_, _ json.RawMessage) (any, error) {
	resp, err := plug.HTTP("GET", "/rest/whoami")
	if err != nil {
		return nil, fmt.Errorf("whoami: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var user map[string]any
	if err := json.Unmarshal(resp.Body, &user); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return map[string]any{
		"id":        user["id"],
		"name":      user["name"],
		"real_name": user["real_name"],
		"email":     user["email"],
	}, nil
}

// --- bugzilla_get_user ---

type getUserParams struct {
	User string `json:"user"`
}

func toolGetUser(params, _ json.RawMessage) (any, error) {
	var p getUserParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	p.User = strings.TrimSpace(p.User)
	if p.User == "" {
		return nil, &handler.Error{Code: "validation_error", Message: "user is required"}
	}

	query := map[string]any{}
	if _, err := strconv.Atoi(p.User); err == nil {
		query["ids"] = p.User
	} else if strings.Contains(p.User, "@") {
		query["names"] = p.User
	} else {
		query["match"] = p.User
	}

	resp, err := plug.HTTP("GET", "/rest/user", handler.WithQuery(query))
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var envelope struct {
		Users []map[string]any `json:"users"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var users []map[string]any
	for _, u := range envelope.Users {
		users = append(users, map[string]any{
			"id":        u["id"],
			"name":      u["name"],
			"real_name": u["real_name"],
			"email":     u["email"],
			"can_login": u["can_login"],
		})
	}

	return map[string]any{
		"users": users,
		"count": len(users),
	}, nil
}

// --- bugzilla_get_products ---

type getProductsParams struct {
	NameFilter     string `json:"name_filter"`
	IncludeDetails bool   `json:"include_details"`
}

const (
	productCacheKey  = "products"
	productCacheTTL  = 3600
	productBatchSize = 50
	fieldCacheTTL    = 3600
)

func toolGetProducts(params, _ json.RawMessage) (any, error) {
	var p getProductsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	cacheKey := productCacheKey
	if p.IncludeDetails {
		cacheKey += ":detailed"
	}

	cached, hit, err := plug.CacheGet(cacheKey)
	if err == nil && hit {
		var products []map[string]any
		if err := json.Unmarshal(cached, &products); err == nil {
			filtered := filterProducts(products, p.NameFilter)
			return map[string]any{
				"products": filtered,
				"count":    len(filtered),
				"cached":   true,
			}, nil
		}
	}

	// Step 1: get accessible product IDs
	resp, err := plug.HTTP("GET", "/rest/product_accessible")
	if err != nil {
		return nil, fmt.Errorf("get product IDs: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var accessible struct {
		IDs []float64 `json:"ids"`
	}
	if err := json.Unmarshal(resp.Body, &accessible); err != nil {
		return nil, fmt.Errorf("parse product IDs: %w", err)
	}

	if len(accessible.IDs) == 0 {
		return map[string]any{"products": []any{}, "count": 0}, nil
	}

	// Step 2: fetch product details in batches of 50
	var allProducts []map[string]any
	for i := 0; i < len(accessible.IDs); i += productBatchSize {
		end := i + productBatchSize
		if end > len(accessible.IDs) {
			end = len(accessible.IDs)
		}
		batch := accessible.IDs[i:end]

		ids := make([]any, len(batch))
		for j, id := range batch {
			ids[j] = int(id)
		}

		resp, err := plug.HTTP("GET", "/rest/product", handler.WithQuery(map[string]any{
			"ids": ids,
		}))
		if err != nil {
			return nil, fmt.Errorf("get products batch: %w", err)
		}
		if resp.Status >= 400 {
			return nil, parseAPIError(resp)
		}

		var prodEnv struct {
			Products []map[string]any `json:"products"`
		}
		if err := json.Unmarshal(resp.Body, &prodEnv); err != nil {
			return nil, fmt.Errorf("parse products: %w", err)
		}

		for _, prod := range prodEnv.Products {
			shaped := map[string]any{
				"id":   prod["id"],
				"name": prod["name"],
			}
			if p.IncludeDetails {
				shaped["components"] = extractNames(prod["components"])
				shaped["versions"] = extractNames(prod["versions"])
				shaped["milestones"] = extractNames(prod["milestones"])
			}
			allProducts = append(allProducts, shaped)
		}
	}

	_ = plug.CacheSet(cacheKey, allProducts, productCacheTTL)

	filtered := filterProducts(allProducts, p.NameFilter)
	return map[string]any{
		"products": filtered,
		"count":    len(filtered),
	}, nil
}

func filterProducts(products []map[string]any, filter string) []map[string]any {
	if filter == "" {
		return products
	}
	lower := strings.ToLower(filter)
	var result []map[string]any
	for _, p := range products {
		name, _ := p["name"].(string)
		if strings.Contains(strings.ToLower(name), lower) {
			result = append(result, p)
		}
	}
	return result
}

func extractNames(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	var names []string
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

// --- bugzilla_get_fields ---

type getFieldsParams struct {
	FieldName string `json:"field_name"`
}

func toolGetFields(params, _ json.RawMessage) (any, error) {
	var p getFieldsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	p.FieldName = strings.TrimSpace(p.FieldName)
	if p.FieldName == "" {
		return nil, &handler.Error{Code: "validation_error", Message: "field_name is required"}
	}
	if strings.ContainsAny(p.FieldName, "/\\") {
		return nil, &handler.Error{Code: "validation_error", Message: "field_name contains invalid characters"}
	}

	cacheKey := "field:" + p.FieldName
	cached, hit, err := plug.CacheGet(cacheKey)
	if err == nil && hit {
		var result any
		if err := json.Unmarshal(cached, &result); err == nil {
			return map[string]any{
				"field":  p.FieldName,
				"values": result,
				"cached": true,
			}, nil
		}
	}

	path := fmt.Sprintf("/rest/field/bug/%s", p.FieldName)
	resp, err := plug.HTTP("GET", path)
	if err != nil {
		return nil, fmt.Errorf("get fields: %w", err)
	}
	if resp.Status >= 400 {
		return nil, parseAPIError(resp)
	}

	var envelope struct {
		Fields []struct {
			Values []map[string]any `json:"values"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var values []string
	if len(envelope.Fields) > 0 {
		for _, v := range envelope.Fields[0].Values {
			if name, ok := v["name"].(string); ok {
				values = append(values, name)
			}
		}
	}

	_ = plug.CacheSet(cacheKey, values, fieldCacheTTL)

	return map[string]any{
		"field":  p.FieldName,
		"values": values,
	}, nil
}

// --- bugzilla_flush_cache ---

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
