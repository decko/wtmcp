package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/LeGambiArt/wtmcp/pkg/handler"
)

type toolResult struct {
	val any
	err error
}

type mockBridge struct {
	in  *bufio.Scanner
	out io.Writer
	t   *testing.T
}

func setupToolTest(t *testing.T) *mockBridge {
	t.Helper()
	pluginIn, bridgeOut := io.Pipe()
	bridgeIn, pluginOut := io.Pipe()

	p := handler.NewForTest(pluginIn, pluginOut)

	plug = p
	cfg.bugzillaURL = "https://bugzilla.example.com"
	cfg.outputDir = t.TempDir()
	cfg.sessionDir = t.TempDir()

	bridge := &mockBridge{
		in:  bufio.NewScanner(bridgeIn),
		out: bridgeOut,
		t:   t,
	}

	t.Cleanup(func() {
		_ = bridgeOut.Close()
		_ = pluginOut.Close()
	})
	return bridge
}

func callTool(fn handler.ToolFunc, params any) <-chan toolResult {
	ch := make(chan toolResult, 1)
	go func() {
		p, _ := json.Marshal(params)
		val, err := fn(p, nil)
		ch <- toolResult{val, err}
	}()
	return ch
}

func (b *mockBridge) expectHTTP(status int, body any) handler.Message {
	b.t.Helper()
	if !b.in.Scan() {
		b.t.Fatal("bridge: expected request, got EOF")
	}
	var req handler.Message
	if err := json.Unmarshal(b.in.Bytes(), &req); err != nil {
		b.t.Fatalf("bridge: unmarshal request: %v", err)
	}

	resp := handler.Message{
		ID:     req.ID,
		Type:   handler.TypeHTTPResponse,
		Status: status,
	}
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			b.t.Fatalf("bridge: marshal body: %v", err)
		}
		resp.Body = data
	}
	data, err := json.Marshal(resp)
	if err != nil {
		b.t.Fatalf("bridge: marshal response: %v", err)
	}
	_, _ = fmt.Fprintf(b.out, "%s\n", data)
	return req
}

func (b *mockBridge) expectCacheGet(hit bool, val any) {
	b.t.Helper()
	if !b.in.Scan() {
		b.t.Fatal("bridge: expected cache_get request, got EOF")
	}
	var req handler.Message
	if err := json.Unmarshal(b.in.Bytes(), &req); err != nil {
		b.t.Fatalf("bridge: unmarshal request: %v", err)
	}

	resp := handler.Message{
		ID:   req.ID,
		Type: handler.TypeCacheGet,
		Hit:  &hit,
	}
	if hit && val != nil {
		data, err := json.Marshal(val)
		if err != nil {
			b.t.Fatalf("bridge: marshal cache value: %v", err)
		}
		resp.Value = data
	}
	data, err := json.Marshal(resp)
	if err != nil {
		b.t.Fatalf("bridge: marshal response: %v", err)
	}
	_, _ = fmt.Fprintf(b.out, "%s\n", data)
}

func (b *mockBridge) expectCacheSet() {
	b.t.Helper()
	if !b.in.Scan() {
		b.t.Fatal("bridge: expected cache_set request, got EOF")
	}
	var req handler.Message
	if err := json.Unmarshal(b.in.Bytes(), &req); err != nil {
		b.t.Fatalf("bridge: unmarshal request: %v", err)
	}

	ok := true
	resp := handler.Message{
		ID:   req.ID,
		Type: handler.TypeCacheSet,
		OK:   &ok,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		b.t.Fatalf("bridge: marshal response: %v", err)
	}
	_, _ = fmt.Fprintf(b.out, "%s\n", data)
}

func (b *mockBridge) expectCacheFlush(count int) handler.Message {
	b.t.Helper()
	if !b.in.Scan() {
		b.t.Fatal("bridge: expected cache_flush request, got EOF")
	}
	var req handler.Message
	if err := json.Unmarshal(b.in.Bytes(), &req); err != nil {
		b.t.Fatalf("bridge: unmarshal request: %v", err)
	}

	ok := true
	resp := handler.Message{
		ID:    req.ID,
		Type:  handler.TypeCacheFlush,
		OK:    &ok,
		Count: &count,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		b.t.Fatalf("bridge: marshal response: %v", err)
	}
	_, _ = fmt.Fprintf(b.out, "%s\n", data)
	return req
}

func collectResult(t *testing.T, ch <-chan toolResult) toolResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("tool call timed out")
		return toolResult{}
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return data
}
