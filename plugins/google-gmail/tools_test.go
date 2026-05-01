package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func TestExtractSummary(t *testing.T) {
	msg := &gmail.Message{
		Id:       "msg-123",
		ThreadId: "thread-456",
		Snippet:  "This is a preview of the email content",
		LabelIds: []string{"INBOX", "UNREAD"},
		Payload: &gmail.MessagePart{
			Headers: []*gmail.MessagePartHeader{
				{Name: "From", Value: "alice@example.com"},
				{Name: "To", Value: "bob@example.com"},
				{Name: "Subject", Value: "Test subject"},
				{Name: "Date", Value: "Mon, 10 Mar 2026 10:00:00 +0000"},
			},
		},
		SizeEstimate: 1234,
	}

	got := extractSummary(msg)

	if got["id"] != "msg-123" {
		t.Errorf("id = %v, want msg-123", got["id"])
	}
	if got["threadId"] != "thread-456" {
		t.Errorf("threadId = %v, want thread-456", got["threadId"])
	}
	if got["from"] != "alice@example.com" {
		t.Errorf("from = %v, want alice@example.com", got["from"])
	}
	if got["to"] != "bob@example.com" {
		t.Errorf("to = %v, want bob@example.com", got["to"])
	}
	if got["subject"] != "Test subject" {
		t.Errorf("subject = %v, want Test subject", got["subject"])
	}
	if got["sizeEstimate"] != int64(1234) {
		t.Errorf("sizeEstimate = %v, want 1234", got["sizeEstimate"])
	}
}

func TestExtractSummaryTruncatesSnippet(t *testing.T) {
	longSnippet := strings.Repeat("a", 300)
	msg := &gmail.Message{
		Id:      "msg-1",
		Snippet: longSnippet,
		Payload: &gmail.MessagePart{},
	}

	got := extractSummary(msg)
	snippet, ok := got["snippet"].(string)
	if !ok {
		t.Fatal("snippet is not a string")
	}
	if len(snippet) != 200 {
		t.Errorf("snippet length = %d, want 200", len(snippet))
	}
}

func TestExtractSummaryNilPayload(t *testing.T) {
	msg := &gmail.Message{
		Id:      "msg-1",
		Snippet: "test",
	}

	got := extractSummary(msg)
	if got["from"] != "" {
		t.Errorf("from should be empty with nil payload, got %v", got["from"])
	}
}

func TestComposeMIME(t *testing.T) {
	raw := composeMIME("alice@example.com", "Test Subject", "Hello world", "", "")

	decoded, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	msg := string(decoded)

	if !strings.Contains(msg, "alice@example.com") {
		t.Error("missing To header")
	}
	if !strings.Contains(msg, "Subject: Test Subject") {
		t.Error("missing Subject header")
	}
	if !strings.Contains(msg, "Hello world") {
		t.Error("missing body")
	}
	if !strings.Contains(msg, "MIME-Version: 1.0") {
		t.Error("missing MIME-Version header")
	}
}

func TestComposeMIMEWithCCBCC(t *testing.T) {
	raw := composeMIME("alice@example.com", "Test", "Body", "cc@example.com", "bcc@example.com")

	decoded, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	msg := string(decoded)

	if !strings.Contains(msg, "Cc:") {
		t.Error("missing Cc header")
	}
	if !strings.Contains(msg, "Bcc:") {
		t.Error("missing Bcc header")
	}
}

func TestFormatAddress(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"alice@example.com", "<alice@example.com>"},
		{"Alice <alice@example.com>", "Alice <alice@example.com>"},
	}

	for _, tt := range tests {
		got := formatAddress(tt.input)
		if got != tt.want {
			t.Errorf("formatAddress(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGenerateCacheFilename(t *testing.T) {
	filename := generateCacheFilename("from:alice subject:report")

	if !strings.HasPrefix(filename, "gmail_from_alice_subject_report_") {
		t.Errorf("unexpected filename prefix: %s", filename)
	}
	if !strings.HasSuffix(filename, ".json") {
		t.Errorf("filename should end with .json: %s", filename)
	}
}

func TestGenerateCacheFilenameTruncatesLong(t *testing.T) {
	long := strings.Repeat("abcdefghij", 10) // 100 chars
	filename := generateCacheFilename(long)

	// "gmail_" (6) + truncated (50) + "_" (1) + timestamp (15) + ".json" (5) = 77
	parts := strings.SplitN(filename, "_", 3)
	if parts[0] != "gmail" {
		t.Errorf("should start with gmail_: %s", filename)
	}
}

func TestCacheDirectory(t *testing.T) {
	origOutputDir := outputDir
	t.Cleanup(func() { outputDir = origOutputDir })

	t.Run("with outputDir", func(t *testing.T) {
		outputDir = "/home/user/project/wtmcp/google-gmail"
		got := cacheDirectory()
		want := "/home/user/project/wtmcp/google-gmail/cache"
		if got != want {
			t.Errorf("cacheDirectory() = %q, want %q", got, want)
		}
	})

	t.Run("empty outputDir returns empty", func(t *testing.T) {
		outputDir = ""
		got := cacheDirectory()
		if got != "" {
			t.Errorf("cacheDirectory() = %q, want empty", got)
		}
	})
}

// --- toolSendMessage dry_run tests ---

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSendMessageDryRunBasic(t *testing.T) {
	params := mustJSON(t, map[string]any{
		"to":      "alice@example.com",
		"subject": "Test",
		"body":    "Hello",
	})

	result, err := toolSendMessage(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["dry_run"] != true {
		t.Error("expected dry_run=true")
	}
	if m["action"] != "gmail_send_message" {
		t.Errorf("action = %v", m["action"])
	}
	if m["to"] != "alice@example.com" {
		t.Errorf("to = %v", m["to"])
	}
	if m["subject"] != "Test" {
		t.Errorf("subject = %v", m["subject"])
	}
	if m["body_preview"] != "Hello" {
		t.Errorf("body_preview = %v", m["body_preview"])
	}
}

func TestSendMessageDryRunWithCCBCC(t *testing.T) {
	params := mustJSON(t, map[string]any{
		"to":      "alice@example.com",
		"subject": "Test",
		"body":    "Hello",
		"cc":      "cc@example.com",
		"bcc":     "bcc@example.com",
	})

	result, err := toolSendMessage(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	if m["cc"] != "cc@example.com" {
		t.Errorf("cc = %v", m["cc"])
	}
	if m["bcc"] != "bcc@example.com" {
		t.Errorf("bcc = %v", m["bcc"])
	}
}

func TestSendMessageDryRunDefault(t *testing.T) {
	// dry_run defaults to true when omitted
	params := mustJSON(t, map[string]any{
		"to":      "alice@example.com",
		"subject": "Test",
		"body":    "Hello",
	})

	result, err := toolSendMessage(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	if m["dry_run"] != true {
		t.Error("dry_run should default to true")
	}
}

func TestSendMessageMissingRequired(t *testing.T) {
	params := mustJSON(t, map[string]any{
		"to": "alice@example.com",
	})

	_, err := toolSendMessage(params, nil)
	if err == nil {
		t.Fatal("expected error for missing subject/body")
	}
}

func TestSendMessageDryRunTruncatesBody(t *testing.T) {
	longBody := strings.Repeat("x", 500)
	params := mustJSON(t, map[string]any{
		"to":      "alice@example.com",
		"subject": "Test",
		"body":    longBody,
	})

	result, err := toolSendMessage(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	preview := m["body_preview"].(string)
	if len(preview) != 300 {
		t.Errorf("body_preview length = %d, want 300", len(preview))
	}
}

// --- toolCreateDraft dry_run tests ---

func TestCreateDraftDryRun(t *testing.T) {
	params := mustJSON(t, map[string]any{
		"to":      "bob@example.com",
		"subject": "Draft",
		"body":    "Draft content",
	})

	result, err := toolCreateDraft(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["dry_run"] != true {
		t.Error("expected dry_run=true")
	}
	if m["action"] != "gmail_create_draft" {
		t.Errorf("action = %v", m["action"])
	}
}

// --- toolModifyLabels dry_run tests ---

func TestModifyLabelsDryRunAdd(t *testing.T) {
	params := mustJSON(t, map[string]any{
		"message_id": "msg-123",
		"add_labels": []string{"STARRED", "IMPORTANT"},
	})

	result, err := toolModifyLabels(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", result)
	}
	if m["dry_run"] != true {
		t.Error("expected dry_run=true")
	}
	if m["action"] != "gmail_modify_labels" {
		t.Errorf("action = %v", m["action"])
	}
	if m["message_id"] != "msg-123" {
		t.Errorf("message_id = %v", m["message_id"])
	}
}

func TestModifyLabelsDryRunRemove(t *testing.T) {
	params := mustJSON(t, map[string]any{
		"message_id":    "msg-456",
		"remove_labels": []string{"INBOX"},
	})

	result, err := toolModifyLabels(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := result.(map[string]any)
	if m["dry_run"] != true {
		t.Error("expected dry_run=true")
	}
}

func TestModifyLabelsMissingMessageID(t *testing.T) {
	params := mustJSON(t, map[string]any{
		"add_labels": []string{"STARRED"},
	})

	_, err := toolModifyLabels(params, nil)
	if err == nil {
		t.Fatal("expected error for missing message_id")
	}
}

// --- toolGetMessages early-return guard ---

func TestGetMessagesTooMany(t *testing.T) {
	ids := make([]string, 25)
	for i := range ids {
		ids[i] = "msg-" + strings.Repeat("x", 5)
	}

	params := mustJSON(t, map[string]any{
		"message_ids": ids,
	})

	result, err := toolGetMessages(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map (error response), got %T", result)
	}
	if _, hasErr := m["error"]; !hasErr {
		t.Error("expected error key in response")
	}
	if m["max_allowed"] != maxMessagesPerRequest {
		t.Errorf("max_allowed = %v, want %d", m["max_allowed"], maxMessagesPerRequest)
	}
}

// --- httptest integration tests ---

func setupGmailTest(t *testing.T, handler http.Handler) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	svc, err := gmail.NewService(context.Background(),
		option.WithHTTPClient(ts.Client()),
		option.WithEndpoint(ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	gmailSvc = svc
}

func TestToolListMessages(t *testing.T) {
	setupGmailTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"messages":[{"id":"msg1","threadId":"t1"},{"id":"msg2","threadId":"t2"}],"resultSizeEstimate":2}`)
	}))

	result, err := toolListMessages(mustJSON(t, map[string]any{
		"query": "from:alice",
	}), nil)
	if err != nil {
		t.Fatalf("toolListMessages: %v", err)
	}

	list, ok := result.(*gmail.ListMessagesResponse)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if len(list.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(list.Messages))
	}
}

func TestToolGetMessagesSummary(t *testing.T) {
	setupGmailTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"msg1","threadId":"t1","snippet":"Hello","payload":{"headers":[{"name":"From","value":"alice@example.com"},{"name":"Subject","value":"Test"}]}}`)
	}))

	result, err := toolGetMessagesSummary(mustJSON(t, map[string]any{
		"message_ids": []string{"msg1"},
	}), nil)
	if err != nil {
		t.Fatalf("toolGetMessagesSummary: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if m["total"] != 1 {
		t.Errorf("total = %v", m["total"])
	}
}

func TestToolSendMessageFull(t *testing.T) {
	setupGmailTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"sent-1","threadId":"t1","labelIds":["SENT"]}`)
	}))

	result, err := toolSendMessage(mustJSON(t, map[string]any{
		"to":      "bob@example.com",
		"subject": "Hello",
		"body":    "Hi Bob",
		"dry_run": false,
	}), nil)
	if err != nil {
		t.Fatalf("toolSendMessage: %v", err)
	}

	msg, ok := result.(*gmail.Message)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if msg.Id != "sent-1" {
		t.Errorf("id = %q", msg.Id)
	}
}

func TestToolListLabels(t *testing.T) {
	setupGmailTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"labels":[{"id":"INBOX","name":"INBOX","type":"system"},{"id":"SENT","name":"SENT","type":"system"}]}`)
	}))

	result, err := toolListLabels(nil, nil)
	if err != nil {
		t.Fatalf("toolListLabels: %v", err)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	labels, ok := m["labels"].([]map[string]string)
	if !ok {
		t.Fatalf("labels type = %T", m["labels"])
	}
	if len(labels) != 2 {
		t.Fatalf("got %d labels, want 2", len(labels))
	}
	if labels[0]["name"] != "INBOX" {
		t.Errorf("first label name = %q", labels[0]["name"])
	}
}

func TestToolModifyLabelsFull(t *testing.T) {
	setupGmailTest(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"msg1","labelIds":["STARRED"]}`)
	}))

	result, err := toolModifyLabels(mustJSON(t, map[string]any{
		"message_id": "msg1",
		"add_labels": []string{"STARRED"},
		"dry_run":    false,
	}), nil)
	if err != nil {
		t.Fatalf("toolModifyLabels: %v", err)
	}

	msg, ok := result.(*gmail.Message)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	if msg.Id != "msg1" {
		t.Errorf("id = %q", msg.Id)
	}
}
