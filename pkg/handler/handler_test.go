package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func newTestPlugin(input string) (*Plugin, *bytes.Buffer) {
	out := &bytes.Buffer{}
	p := NewForTest(strings.NewReader(input), out)
	return p, out
}

func readMessages(t *testing.T, out *bytes.Buffer) []Message {
	t.Helper()
	var msgs []Message
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			t.Fatalf("unmarshal output: %v", err)
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func TestInitAndShutdown(t *testing.T) {
	input := `{"id":"init-1","type":"init","config":{}}` + "\n" +
		`{"id":"shutdown-1","type":"shutdown"}` + "\n"

	p, out := newTestPlugin(input)

	var gotConfig json.RawMessage
	p.OnInit(func(config json.RawMessage) error {
		gotConfig = config
		return nil
	})

	if err := p.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if string(gotConfig) != "{}" {
		t.Errorf("init config = %s, want {}", gotConfig)
	}

	msgs := readMessages(t, out)
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].Type != TypeInitOK || msgs[0].ID != "init-1" {
		t.Errorf("msg[0] = %s/%s, want init_ok/init-1", msgs[0].Type, msgs[0].ID)
	}
	if msgs[1].Type != TypeShutdownOK || msgs[1].ID != "shutdown-1" {
		t.Errorf("msg[1] = %s/%s, want shutdown_ok/shutdown-1", msgs[1].Type, msgs[1].ID)
	}
}

func TestToolCall(t *testing.T) {
	input := `{"id":"init-1","type":"init","config":{}}` + "\n" +
		`{"id":"req-1","type":"tool_call","tool":"greet","params":{"name":"world"}}` + "\n" +
		`{"id":"shutdown-1","type":"shutdown"}` + "\n"

	p, out := newTestPlugin(input)
	p.Handle("greet", func(params, _ json.RawMessage) (any, error) {
		var p struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return map[string]string{"greeting": "hello " + p.Name}, nil
	})

	if err := p.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs := readMessages(t, out)
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}

	result := msgs[1]
	if result.Type != TypeToolResult || result.ID != "req-1" {
		t.Errorf("result = %s/%s, want tool_result/req-1", result.Type, result.ID)
	}
	if result.Error != nil {
		t.Errorf("unexpected error: %v", result.Error)
	}

	var got map[string]string
	if err := json.Unmarshal(result.Result, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if got["greeting"] != "hello world" {
		t.Errorf("greeting = %q, want %q", got["greeting"], "hello world")
	}
}

func TestToolCallError(t *testing.T) {
	input := `{"id":"init-1","type":"init","config":{}}` + "\n" +
		`{"id":"req-1","type":"tool_call","tool":"fail","params":{}}` + "\n" +
		`{"id":"shutdown-1","type":"shutdown"}` + "\n"

	p, out := newTestPlugin(input)
	p.Handle("fail", func(_, _ json.RawMessage) (any, error) {
		return nil, &Error{Code: "test_error", Message: "something broke"}
	})

	if err := p.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs := readMessages(t, out)
	result := msgs[1]
	if result.Error == nil {
		t.Fatal("expected error in result")
	}
	if result.Error.Code != "test_error" {
		t.Errorf("error code = %q, want %q", result.Error.Code, "test_error")
	}
}

func TestUnknownTool(t *testing.T) {
	input := `{"id":"init-1","type":"init","config":{}}` + "\n" +
		`{"id":"req-1","type":"tool_call","tool":"nonexistent","params":{}}` + "\n" +
		`{"id":"shutdown-1","type":"shutdown"}` + "\n"

	p, out := newTestPlugin(input)

	if err := p.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs := readMessages(t, out)
	result := msgs[1]
	if result.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
	if result.Error.Code != "unknown_tool" {
		t.Errorf("error code = %q, want %q", result.Error.Code, "unknown_tool")
	}
}

func TestEOFWithoutShutdown(t *testing.T) {
	// Handler should exit gracefully on EOF (core crashed/killed)
	input := `{"id":"init-1","type":"init","config":{}}` + "\n"

	p, _ := newTestPlugin(input)
	if err := p.Run(); err != nil {
		t.Fatalf("Run should return nil on EOF, got: %v", err)
	}
}

func TestCacheFlush(t *testing.T) {
	count := 42
	response := Message{
		ID:    "cache-1",
		Type:  TypeCacheFlush,
		OK:    boolPtr(true),
		Count: &count,
	}
	respBytes, _ := json.Marshal(response)
	input := string(respBytes) + "\n"

	p, out := newTestPlugin(input)
	got, err := p.CacheFlush()
	if err != nil {
		t.Fatalf("CacheFlush: %v", err)
	}
	if got != 42 {
		t.Errorf("count = %d, want 42", got)
	}

	msgs := readMessages(t, out)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Type != TypeCacheFlush {
		t.Errorf("type = %s, want %s", msgs[0].Type, TypeCacheFlush)
	}
}

func TestCacheFlushRejected(t *testing.T) {
	response := Message{
		ID:   "cache-1",
		Type: TypeCacheFlush,
		OK:   boolPtr(false),
	}
	respBytes, _ := json.Marshal(response)
	input := string(respBytes) + "\n"

	p, _ := newTestPlugin(input)
	_, err := p.CacheFlush()
	if err == nil {
		t.Fatal("expected error for rejected flush")
	}
	if !strings.Contains(err.Error(), "rejected") {
		t.Errorf("error = %q, want 'rejected'", err.Error())
	}
}

func TestCacheFlushCoreError(t *testing.T) {
	response := Message{
		ID:   "cache-1",
		Type: TypeCacheFlush,
		Error: &Error{
			Code:    "cache_error",
			Message: "backend down",
		},
	}
	respBytes, _ := json.Marshal(response)
	input := string(respBytes) + "\n"

	p, _ := newTestPlugin(input)
	_, err := p.CacheFlush()
	if err == nil {
		t.Fatal("expected error for core cache failure")
	}
	if !strings.Contains(err.Error(), "backend down") {
		t.Errorf("error = %q, want 'backend down'", err.Error())
	}
}

func TestCacheFlushEOF(t *testing.T) {
	p, _ := newTestPlugin("")
	_, err := p.CacheFlush()
	if err == nil {
		t.Fatal("expected error on EOF")
	}
	if !strings.Contains(err.Error(), "unexpected EOF") {
		t.Errorf("error = %q, want 'unexpected EOF'", err.Error())
	}
}

func TestNewForTest(t *testing.T) {
	in := strings.NewReader(`{"id":"s-1","type":"shutdown"}` + "\n")
	out := &bytes.Buffer{}
	p := NewForTest(in, out)
	if err := p.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	msgs := readMessages(t, out)
	if len(msgs) != 1 || msgs[0].Type != TypeShutdownOK {
		t.Errorf("expected shutdown_ok, got %v", msgs)
	}
}

func boolPtr(v bool) *bool { return &v }

func TestToolCallParentIDPropagation(t *testing.T) {
	// Use io.Pipe so we can interleave: send tool_call, read the HTTP
	// request from the handler, send an HTTP response, then read the
	// tool result.
	// core writes to coreW → plugin reads from coreR (plugin's stdin)
	coreR, coreW := io.Pipe()
	// plugin writes to pluginW → core reads from pluginR (plugin's stdout)
	pluginR, pluginW := io.Pipe()

	p := NewForTest(coreR, pluginW)
	p.Handle("make_http", func(_, _ json.RawMessage) (any, error) {
		_, err := p.HTTP("GET", "/api/test")
		if err != nil {
			return nil, err
		}
		return "ok", nil
	})

	errCh := make(chan error, 1)
	go func() { errCh <- p.Run() }()

	enc := json.NewEncoder(coreW)
	dec := json.NewDecoder(pluginR)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("protocol: %v", err)
		}
	}

	// Send init
	must(enc.Encode(Message{ID: "init-1", Type: TypeInit}))
	var initResp Message
	must(dec.Decode(&initResp))

	// Send tool_call
	must(enc.Encode(Message{ID: "call-42", Type: TypeToolCall, Tool: "make_http"}))

	// Read the HTTP request from the handler
	var httpReq Message
	must(dec.Decode(&httpReq))

	if httpReq.Type != TypeHTTPRequest {
		t.Fatalf("expected http_request, got %s", httpReq.Type)
	}
	if httpReq.ParentID != "call-42" {
		t.Errorf("parent_id = %q, want call-42", httpReq.ParentID)
	}

	// Send HTTP response
	must(enc.Encode(Message{ID: httpReq.ID, Type: TypeHTTPResponse, Status: 200}))

	// Read tool result
	var toolResult Message
	must(dec.Decode(&toolResult))
	if toolResult.Type != TypeToolResult {
		t.Fatalf("expected tool_result, got %s", toolResult.Type)
	}

	// Shutdown
	must(enc.Encode(Message{ID: "shutdown-1", Type: TypeShutdown}))
	var shutdownResp Message
	must(dec.Decode(&shutdownResp))

	_ = coreW.Close()

	if err := <-errCh; err != nil {
		t.Errorf("Run: %v", err)
	}
}
