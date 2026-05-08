package handler

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestFileWrite_Success(t *testing.T) {
	size := int64(4)
	response := Message{
		ID:   "fw-1",
		Type: TypeFileWriteResponse,
		Path: "/output/data.json",
		Size: &size,
	}
	respBytes, _ := json.Marshal(response)

	p, out := newTestPlugin(string(respBytes) + "\n")
	path, err := p.FileWrite("data.json", []byte("test"))
	if err != nil {
		t.Fatalf("FileWrite: %v", err)
	}
	if path != "/output/data.json" {
		t.Errorf("path = %q, want /output/data.json", path)
	}

	msgs := readMessages(t, out)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Type != TypeFileWrite {
		t.Errorf("type = %s, want %s", msgs[0].Type, TypeFileWrite)
	}
	if msgs[0].Path != "data.json" {
		t.Errorf("path = %q, want data.json", msgs[0].Path)
	}
	if msgs[0].Content != "test" {
		t.Errorf("content = %q, want test", msgs[0].Content)
	}
	if msgs[0].BodyEncoding != "text" {
		t.Errorf("encoding = %q, want text", msgs[0].BodyEncoding)
	}
}

func TestFileWrite_Error(t *testing.T) {
	response := Message{
		ID:   "fw-1",
		Type: TypeFileWriteResponse,
		Error: &Error{
			Code:    "fileio_error",
			Message: "path escapes allowed directory",
		},
	}
	respBytes, _ := json.Marshal(response)

	p, _ := newTestPlugin(string(respBytes) + "\n")
	_, err := p.FileWrite("../../escape.txt", []byte("data"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "path escapes") {
		t.Errorf("error = %q, want 'path escapes'", err.Error())
	}
}

func TestFileWrite_WithOptions(t *testing.T) {
	size := int64(4)
	response := Message{
		ID:   "fw-1",
		Type: TypeFileWriteResponse,
		Path: "/output/data.bin",
		Size: &size,
	}
	respBytes, _ := json.Marshal(response)

	p, out := newTestPlugin(string(respBytes) + "\n")
	_, err := p.FileWrite("data.bin", []byte{0xDE, 0xAD},
		WithPermissions("0640"),
		WithEncoding("base64"),
		WithNoMkdir(),
	)
	if err != nil {
		t.Fatalf("FileWrite: %v", err)
	}

	msgs := readMessages(t, out)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Permissions != "0640" {
		t.Errorf("permissions = %q, want 0640", msgs[0].Permissions)
	}
	if msgs[0].BodyEncoding != "base64" {
		t.Errorf("encoding = %q, want base64", msgs[0].BodyEncoding)
	}
	if msgs[0].Mkdir == nil || *msgs[0].Mkdir != false {
		t.Error("mkdir should be explicitly false")
	}
	wantContent := base64.StdEncoding.EncodeToString([]byte{0xDE, 0xAD})
	if msgs[0].Content != wantContent {
		t.Errorf("content = %q, want base64-encoded %q", msgs[0].Content, wantContent)
	}
}

func TestFileWriteFrom_Success(t *testing.T) {
	size := int64(1024)
	response := Message{
		ID:   "fw-1",
		Type: TypeFileWriteResponse,
		Path: "/output/attachment.bin",
		Size: &size,
	}
	respBytes, _ := json.Marshal(response)

	p, out := newTestPlugin(string(respBytes) + "\n")
	path, err := p.FileWriteFrom("attachment.bin", "/tmp/wtmcp/plugin/dl-123.tmp")
	if err != nil {
		t.Fatalf("FileWriteFrom: %v", err)
	}
	if path != "/output/attachment.bin" {
		t.Errorf("path = %q, want /output/attachment.bin", path)
	}

	msgs := readMessages(t, out)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].SourcePath != "/tmp/wtmcp/plugin/dl-123.tmp" {
		t.Errorf("source_path = %q", msgs[0].SourcePath)
	}
	if msgs[0].Content != "" {
		t.Error("content should be empty for source_path mode")
	}

	// Verify the "content" key is NOT in the wire JSON — its presence
	// would trigger the core's mutual exclusion check.
	raw, _ := json.Marshal(msgs[0])
	if strings.Contains(string(raw), `"content"`) {
		t.Errorf("JSON should NOT contain \"content\" key for source_path writes, got: %s", raw)
	}
}

func TestFileRead_Success(t *testing.T) {
	response := Message{
		ID:      "fr-1",
		Type:    TypeFileReadResponse,
		Content: "hello world",
		Path:    "/output/data.txt",
	}
	respBytes, _ := json.Marshal(response)

	p, out := newTestPlugin(string(respBytes) + "\n")
	data, err := p.FileRead("data.txt")
	if err != nil {
		t.Fatalf("FileRead: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("content = %q, want 'hello world'", data)
	}

	msgs := readMessages(t, out)
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
	if msgs[0].Type != TypeFileRead {
		t.Errorf("type = %s, want %s", msgs[0].Type, TypeFileRead)
	}
}

func TestFileRead_Base64(t *testing.T) {
	binary := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	response := Message{
		ID:      "fr-1",
		Type:    TypeFileReadResponse,
		Content: base64.StdEncoding.EncodeToString(binary),
		Path:    "/output/data.bin",
	}
	respBytes, _ := json.Marshal(response)

	p, _ := newTestPlugin(string(respBytes) + "\n")
	data, err := p.FileRead("data.bin", WithReadEncoding("base64"))
	if err != nil {
		t.Fatalf("FileRead: %v", err)
	}
	if string(data) != string(binary) {
		t.Errorf("content = %x, want %x", data, binary)
	}
}

func TestFileRead_Error(t *testing.T) {
	response := Message{
		ID:   "fr-1",
		Type: TypeFileReadResponse,
		Error: &Error{
			Code:    "fileio_error",
			Message: "file not found",
		},
	}
	respBytes, _ := json.Marshal(response)

	p, _ := newTestPlugin(string(respBytes) + "\n")
	_, err := p.FileRead("nonexistent.txt")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "file not found") {
		t.Errorf("error = %q, want 'file not found'", err.Error())
	}
}

func TestFileWrite_EmptyContent(t *testing.T) {
	size := int64(0)
	response := Message{
		ID:   "fw-1",
		Type: TypeFileWriteResponse,
		Path: "/output/empty.txt",
		Size: &size,
	}
	respBytes, _ := json.Marshal(response)

	p, out := newTestPlugin(string(respBytes) + "\n")
	path, err := p.FileWrite("empty.txt", []byte{})
	if err != nil {
		t.Fatalf("FileWrite empty content: %v", err)
	}
	if path != "/output/empty.txt" {
		t.Errorf("path = %q, want /output/empty.txt", path)
	}

	// Check the raw wire JSON (not re-marshaled struct) for the
	// "content" key. The custom MarshalJSON must inject it.
	raw := out.String()
	if !strings.Contains(raw, `"content"`) {
		t.Errorf("wire JSON should contain \"content\" key for empty writes, got: %s", raw)
	}
}

func TestFileWrite_NilContent(t *testing.T) {
	size := int64(0)
	response := Message{
		ID:   "fw-1",
		Type: TypeFileWriteResponse,
		Path: "/output/nil.txt",
		Size: &size,
	}
	respBytes, _ := json.Marshal(response)

	p, out := newTestPlugin(string(respBytes) + "\n")
	path, err := p.FileWrite("nil.txt", nil)
	if err != nil {
		t.Fatalf("FileWrite nil content: %v", err)
	}
	if path != "/output/nil.txt" {
		t.Errorf("path = %q, want /output/nil.txt", path)
	}

	raw := out.String()
	if !strings.Contains(raw, `"content"`) {
		t.Errorf("wire JSON should contain \"content\" key for nil writes, got: %s", raw)
	}
}

func TestFileWrite_InvalidUTF8(t *testing.T) {
	p, _ := newTestPlugin("")
	_, err := p.FileWrite("binary.dat", []byte{0xFF, 0xFE, 0x00, 0x01})
	if err == nil {
		t.Fatal("expected error for invalid UTF-8 content")
	}
	if !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Errorf("error = %q, want 'not valid UTF-8'", err.Error())
	}
}

func TestFileWrite_EOF(t *testing.T) {
	p, _ := newTestPlugin("")
	_, err := p.FileWrite("data.json", []byte("test"))
	if err == nil {
		t.Fatal("expected EOF error")
	}
	if !strings.Contains(err.Error(), "unexpected EOF") {
		t.Errorf("error = %q, want 'unexpected EOF'", err.Error())
	}
}
