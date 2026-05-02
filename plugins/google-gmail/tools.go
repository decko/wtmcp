package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"google.golang.org/api/gmail/v1"
)

const (
	maxMessagesPerRequest = 20
	maxMessagesList       = 100
	maxMessagesCache      = 200
)

// --- gmail_list_messages ---

type listMessagesParams struct {
	Query      string   `json:"query"`
	MaxResults int      `json:"max_results"`
	LabelIDs   []string `json:"label_ids"`
}

func toolListMessages(params, _ json.RawMessage) (any, error) {
	var p listMessagesParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}
	if p.MaxResults == 0 {
		p.MaxResults = 10
	}
	if p.MaxResults > maxMessagesList {
		p.MaxResults = maxMessagesList
	}

	call := gmailSvc.Users.Messages.List("me").MaxResults(int64(p.MaxResults))
	if p.Query != "" {
		call = call.Q(p.Query)
	}
	if len(p.LabelIDs) > 0 {
		call = call.LabelIds(p.LabelIDs...)
	}

	res, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return res, nil
}

// --- gmail_get_messages_summary ---

type getMessagesSummaryParams struct {
	MessageIDs  []string `json:"message_ids"`
	MaxMessages int      `json:"max_messages"`
}

func toolGetMessagesSummary(params, _ json.RawMessage) (any, error) {
	var p getMessagesSummaryParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}
	if len(p.MessageIDs) == 0 {
		return nil, fmt.Errorf("message_ids is required")
	}
	if p.MaxMessages == 0 {
		p.MaxMessages = 50
	}
	if p.MaxMessages > maxMessagesList {
		p.MaxMessages = maxMessagesList
	}
	if len(p.MessageIDs) > p.MaxMessages {
		p.MessageIDs = p.MessageIDs[:p.MaxMessages]
	}

	var summaries []any
	for _, mid := range p.MessageIDs {
		msg, err := gmailSvc.Users.Messages.Get("me", mid).Format("metadata").Do()
		if err != nil {
			summaries = append(summaries, map[string]string{
				"id":    mid,
				"error": err.Error(),
			})
			continue
		}
		summaries = append(summaries, extractSummary(msg))
	}

	return map[string]any{
		"total":    len(summaries),
		"messages": summaries,
	}, nil
}

// --- gmail_fetch_and_cache ---

type fetchAndCacheParams struct {
	Query       string   `json:"query"`
	MessageIDs  []string `json:"message_ids"`
	MaxMessages int      `json:"max_messages"`
	Format      string   `json:"format"`
}

func toolFetchAndCache(params, _ json.RawMessage) (any, error) {
	var p fetchAndCacheParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}
	if p.MaxMessages == 0 {
		p.MaxMessages = 50
	}
	if p.MaxMessages > maxMessagesCache {
		p.MaxMessages = maxMessagesCache
	}
	if p.Format == "" {
		p.Format = "full"
	}

	ids := p.MessageIDs

	if len(ids) == 0 {
		if p.Query == "" {
			return nil, fmt.Errorf("must provide either query or message_ids")
		}
		listRes, err := gmailSvc.Users.Messages.List("me").
			Q(p.Query).MaxResults(int64(p.MaxMessages)).Do()
		if err != nil {
			return nil, fmt.Errorf("list messages: %w", err)
		}
		for _, m := range listRes.Messages {
			ids = append(ids, m.Id)
		}
	}

	if len(ids) > p.MaxMessages {
		ids = ids[:p.MaxMessages]
	}
	if len(ids) == 0 {
		return map[string]any{
			"total":    0,
			"messages": []any{},
			"note":     "No messages found matching query",
		}, nil
	}

	var messages []*gmail.Message
	var summaries []any

	for _, mid := range ids {
		msg, err := gmailSvc.Users.Messages.Get("me", mid).Format(p.Format).Do()
		if err != nil {
			summaries = append(summaries, map[string]string{
				"id":    mid,
				"error": err.Error(),
			})
			continue
		}
		messages = append(messages, msg)
		summaries = append(summaries, extractSummary(msg))
	}

	// Save to cache
	cacheDir := cacheDirectory()
	if cacheDir == "" {
		return nil, fmt.Errorf("gmail cache requires a configured output directory")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	label := p.Query
	if label == "" {
		label = fmt.Sprintf("%d_messages", len(ids))
	}
	filename := generateCacheFilename(label)
	cachePath := filepath.Join(cacheDir, filename)

	cacheData := map[string]any{
		"query":      p.Query,
		"fetched_at": time.Now().UTC().Format(time.RFC3339),
		"total":      len(messages),
		"format":     p.Format,
		"messages":   messages,
	}

	data, err := json.MarshalIndent(cacheData, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal cache: %w", err)
	}
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		return nil, fmt.Errorf("write cache: %w", err)
	}

	// Return only first 10 summaries
	displaySummaries := summaries
	if len(displaySummaries) > 10 {
		displaySummaries = displaySummaries[:10]
	}

	return map[string]any{
		"cache_file":   cachePath,
		"total_cached": len(messages),
		"summaries":    displaySummaries,
		"note":         fmt.Sprintf("Full data cached to %s. Use file operations to process locally.", cachePath),
	}, nil
}

// --- gmail_get_messages ---

type getMessagesParams struct {
	MessageIDs []string `json:"message_ids"`
	Format     string   `json:"format"`
}

func toolGetMessages(params, _ json.RawMessage) (any, error) {
	var p getMessagesParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}
	if len(p.MessageIDs) == 0 {
		return nil, fmt.Errorf("message_ids is required")
	}
	if p.Format == "" {
		p.Format = "metadata"
	}
	if len(p.MessageIDs) > maxMessagesPerRequest {
		return map[string]any{
			"error":       fmt.Sprintf("too many message IDs (max %d)", maxMessagesPerRequest),
			"requested":   len(p.MessageIDs),
			"max_allowed": maxMessagesPerRequest,
			"suggestion":  "Use gmail_get_messages_summary for lightweight summaries, or gmail_fetch_and_cache to cache locally.",
		}, nil
	}

	var results []any
	for _, mid := range p.MessageIDs {
		msg, err := gmailSvc.Users.Messages.Get("me", mid).Format(p.Format).Do()
		if err != nil {
			results = append(results, map[string]string{
				"id":    mid,
				"error": err.Error(),
			})
			continue
		}
		results = append(results, msg)
	}
	return results, nil
}

// --- gmail_send_message ---

type sendMessageParams struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	CC      string `json:"cc"`
	BCC     string `json:"bcc"`
	DryRun  bool   `json:"dry_run"`
}

func toolSendMessage(params, _ json.RawMessage) (any, error) {
	p := sendMessageParams{DryRun: true}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}
	if p.To == "" || p.Subject == "" || p.Body == "" {
		return nil, fmt.Errorf("to, subject, and body are required")
	}

	raw := composeMIME(p.To, p.Subject, p.Body, p.CC, p.BCC)

	if p.DryRun {
		preview := p.Body
		if len(preview) > 300 {
			preview = preview[:300]
		}
		return map[string]any{
			"dry_run":      true,
			"action":       "gmail_send_message",
			"to":           p.To,
			"cc":           p.CC,
			"bcc":          p.BCC,
			"subject":      p.Subject,
			"body_preview": preview,
		}, nil
	}

	res, err := gmailSvc.Users.Messages.Send("me", &gmail.Message{Raw: raw}).Do()
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}
	return res, nil
}

// --- gmail_create_draft ---

type createDraftParams struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	CC      string `json:"cc"`
	BCC     string `json:"bcc"`
	DryRun  bool   `json:"dry_run"`
}

func toolCreateDraft(params, _ json.RawMessage) (any, error) {
	p := createDraftParams{DryRun: true}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}
	if p.To == "" || p.Subject == "" || p.Body == "" {
		return nil, fmt.Errorf("to, subject, and body are required")
	}

	raw := composeMIME(p.To, p.Subject, p.Body, p.CC, p.BCC)

	if p.DryRun {
		preview := p.Body
		if len(preview) > 300 {
			preview = preview[:300]
		}
		return map[string]any{
			"dry_run":      true,
			"action":       "gmail_create_draft",
			"to":           p.To,
			"cc":           p.CC,
			"bcc":          p.BCC,
			"subject":      p.Subject,
			"body_preview": preview,
		}, nil
	}

	res, err := gmailSvc.Users.Drafts.Create("me", &gmail.Draft{
		Message: &gmail.Message{Raw: raw},
	}).Do()
	if err != nil {
		return nil, fmt.Errorf("create draft: %w", err)
	}
	return res, nil
}

// --- gmail_modify_labels ---

type modifyLabelsParams struct {
	MessageID    string   `json:"message_id"`
	AddLabels    []string `json:"add_labels"`
	RemoveLabels []string `json:"remove_labels"`
	DryRun       bool     `json:"dry_run"`
}

func toolModifyLabels(params, _ json.RawMessage) (any, error) {
	p := modifyLabelsParams{DryRun: true}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}
	if p.MessageID == "" {
		return nil, fmt.Errorf("message_id is required")
	}

	if p.DryRun {
		return map[string]any{
			"dry_run":        true,
			"action":         "gmail_modify_labels",
			"message_id":     p.MessageID,
			"addLabelIds":    p.AddLabels,
			"removeLabelIds": p.RemoveLabels,
		}, nil
	}

	res, err := gmailSvc.Users.Messages.Modify("me", p.MessageID, &gmail.ModifyMessageRequest{
		AddLabelIds:    p.AddLabels,
		RemoveLabelIds: p.RemoveLabels,
	}).Do()
	if err != nil {
		return nil, fmt.Errorf("modify labels: %w", err)
	}
	return res, nil
}

// --- gmail_list_labels ---

func toolListLabels(_, _ json.RawMessage) (any, error) {
	res, err := gmailSvc.Users.Labels.List("me").Do()
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}

	// Return only essential fields to save tokens
	var labels []map[string]string
	for _, l := range res.Labels {
		labels = append(labels, map[string]string{
			"id":   l.Id,
			"name": l.Name,
			"type": l.Type,
		})
	}
	return map[string]any{"labels": labels}, nil
}

// --- helpers ---

func extractSummary(msg *gmail.Message) map[string]any {
	headers := make(map[string]string)
	if msg.Payload != nil {
		for _, h := range msg.Payload.Headers {
			headers[h.Name] = h.Value
		}
	}

	snippet := msg.Snippet
	if len(snippet) > 200 {
		snippet = snippet[:200]
	}

	return map[string]any{
		"id":           msg.Id,
		"threadId":     msg.ThreadId,
		"date":         headers["Date"],
		"from":         headers["From"],
		"to":           headers["To"],
		"subject":      headers["Subject"],
		"snippet":      snippet,
		"labelIds":     msg.LabelIds,
		"sizeEstimate": msg.SizeEstimate,
	}
}

func composeMIME(to, subject, body, cc, bcc string) string {
	var buf strings.Builder

	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	fmt.Fprintf(&buf, "To: %s\r\n", formatAddress(to))
	fmt.Fprintf(&buf, "Subject: %s\r\n", subject)
	if cc != "" {
		fmt.Fprintf(&buf, "Cc: %s\r\n", formatAddress(cc))
	}
	if bcc != "" {
		fmt.Fprintf(&buf, "Bcc: %s\r\n", formatAddress(bcc))
	}
	buf.WriteString("\r\n")
	buf.WriteString(body)

	return base64.URLEncoding.EncodeToString([]byte(buf.String()))
}

func formatAddress(addr string) string {
	// If already looks like a formatted address, return as-is
	if strings.Contains(addr, "<") {
		return addr
	}
	a := mail.Address{Address: addr}
	return a.String()
}

func cacheDirectory() string {
	if outputDir != "" {
		return filepath.Join(outputDir, "cache")
	}
	return ""
}

func generateCacheFilename(label string) string {
	safe := regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(label, "_")
	safe = regexp.MustCompile(`_+`).ReplaceAllString(safe, "_")
	safe = strings.Trim(safe, "_")
	if len(safe) > 50 {
		safe = safe[:50]
	}
	ts := time.Now().UTC().Format("20060102_150405")
	return fmt.Sprintf("gmail_%s_%s.json", safe, ts)
}
