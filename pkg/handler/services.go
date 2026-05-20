package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// HTTPResponse holds the result of an HTTP request made through the core proxy.
type HTTPResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// HTTP sends an HTTP request through the core's proxy and returns the response.
// The core handles authentication, TLS, and domain allowlisting.
func (p *Plugin) HTTP(method, path string, opts ...RequestOption) (*HTTPResponse, error) {
	req := Message{
		ID:       p.nextMsgID("http"),
		ParentID: p.callID,
		Type:     TypeHTTPRequest,
		Method:   method,
		Path:     path,
	}
	for _, opt := range opts {
		opt(&req)
	}

	p.send(req)

	resp, err := p.waitFor(req.ID, TypeHTTPResponse)
	if err != nil {
		return nil, fmt.Errorf("http %s %s: %w", method, path, err)
	}

	return &HTTPResponse{
		Status:  resp.Status,
		Headers: resp.Headers,
		Body:    resp.Body,
	}, nil
}

// RequestOption configures an HTTP request.
type RequestOption func(*Message)

// WithQuery sets query parameters on the request.
func WithQuery(q map[string]any) RequestOption {
	return func(m *Message) { m.Query = q }
}

// WithHeaders sets headers on the request.
func WithHeaders(h map[string]string) RequestOption {
	return func(m *Message) { m.Headers = h }
}

// WithBody sets a JSON body on the request.
func WithBody(body any) RequestOption {
	return func(m *Message) {
		data, err := json.Marshal(body)
		if err == nil {
			m.Body = data
		}
	}
}

// WithRawBody sets a pre-encoded JSON body on the request.
func WithRawBody(body json.RawMessage) RequestOption {
	return func(m *Message) { m.Body = body }
}

// WithURL sets an absolute URL instead of a relative path.
// When set, the core uses this URL directly instead of base_url + path.
func WithURL(url string) RequestOption {
	return func(m *Message) { m.URL = url }
}

// CacheGet retrieves a value from the core's cache.
// Returns the value and whether it was a cache hit.
func (p *Plugin) CacheGet(key string) (json.RawMessage, bool, error) {
	req := Message{
		ID:       p.nextMsgID("cache"),
		ParentID: p.callID,
		Type:     TypeCacheGet,
		Key:      key,
	}

	p.send(req)

	resp, err := p.waitFor(req.ID, TypeCacheGet)
	if err != nil {
		return nil, false, fmt.Errorf("cache get %q: %w", key, err)
	}
	if resp.Error != nil {
		return nil, false, fmt.Errorf("cache get %q: %s", key, resp.Error.Message)
	}

	hit := resp.Hit != nil && *resp.Hit
	return resp.Value, hit, nil
}

// CacheSet stores a value in the core's cache with an optional TTL in seconds.
// Pass 0 for ttl to use the plugin's default TTL.
func (p *Plugin) CacheSet(key string, value any, ttl int) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal cache value: %w", err)
	}

	req := Message{
		ID:       p.nextMsgID("cache"),
		ParentID: p.callID,
		Type:     TypeCacheSet,
		Key:      key,
		Value:    data,
	}
	if ttl > 0 {
		req.TTL = &ttl
	}

	p.send(req)

	resp, err := p.waitFor(req.ID, TypeCacheSet)
	if err != nil {
		return fmt.Errorf("cache set %q: %w", key, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("cache set %q: %s", key, resp.Error.Message)
	}
	if resp.OK != nil && !*resp.OK {
		return fmt.Errorf("cache set %q: rejected by core", key)
	}
	return nil
}

// CacheFlush removes all keys in this plugin's cache namespace.
// Returns the number of keys flushed.
func (p *Plugin) CacheFlush() (int, error) {
	req := Message{
		ID:       p.nextMsgID("cache"),
		ParentID: p.callID,
		Type:     TypeCacheFlush,
	}

	p.send(req)

	resp, err := p.waitFor(req.ID, TypeCacheFlush)
	if err != nil {
		return 0, fmt.Errorf("cache flush: %w", err)
	}
	if resp.Error != nil {
		return 0, fmt.Errorf("cache flush: %s", resp.Error.Message)
	}
	if resp.OK != nil && !*resp.OK {
		return 0, fmt.Errorf("cache flush: rejected by core")
	}

	count := 0
	if resp.Count != nil {
		count = *resp.Count
	}
	return count, nil
}

// CacheDel deletes a key from the core's cache.
func (p *Plugin) CacheDel(key string) error {
	req := Message{
		ID:       p.nextMsgID("cache"),
		ParentID: p.callID,
		Type:     TypeCacheDel,
		Key:      key,
	}

	p.send(req)

	resp, err := p.waitFor(req.ID, TypeCacheDel)
	if err != nil {
		return fmt.Errorf("cache del %q: %w", key, err)
	}
	if resp.Error != nil {
		return fmt.Errorf("cache del %q: %s", key, resp.Error.Message)
	}
	return nil
}

// FileWriteOption configures a file write request.
type FileWriteOption func(*Message)

// FileReadOption configures a file read request.
type FileReadOption func(*Message)

// WithPermissions sets the file permissions (octal string, e.g., "0640").
func WithPermissions(mode string) FileWriteOption {
	return func(m *Message) { m.Permissions = mode }
}

// WithEncoding sets the content encoding ("text" or "base64").
func WithEncoding(enc string) FileWriteOption {
	return func(m *Message) { m.BodyEncoding = enc }
}

// WithNoMkdir disables automatic parent directory creation.
func WithNoMkdir() FileWriteOption {
	return func(m *Message) {
		f := false
		m.Mkdir = &f
	}
}

// WithReadEncoding sets the encoding for file read responses.
func WithReadEncoding(enc string) FileReadOption {
	return func(m *Message) { m.BodyEncoding = enc }
}

// FileWrite writes content to a file under the plugin's output directory.
// Returns the absolute resolved path of the written file. Defaults to
// "text" encoding — content must be valid UTF-8. For binary data, use
// WithEncoding("base64").
func (p *Plugin) FileWrite(path string, content []byte, opts ...FileWriteOption) (string, error) {
	req := Message{
		ID:           p.nextMsgID("fw"),
		ParentID:     p.callID,
		Type:         TypeFileWrite,
		Path:         path,
		Content:      string(content),
		BodyEncoding: "text",
		hasContent:   true,
	}
	for _, opt := range opts {
		opt(&req)
	}
	// Auto-encode content when base64 encoding is selected.
	// The Content field was set as string(content) above (raw bytes).
	// For base64 mode, we must encode it properly before sending.
	if req.BodyEncoding == "base64" {
		req.Content = base64.StdEncoding.EncodeToString(content)
	} else if len(content) > 0 && !utf8.Valid(content) {
		return "", fmt.Errorf("file write %q: content is not valid UTF-8 (use WithEncoding(\"base64\") for binary data)", path)
	}

	p.send(req)

	resp, err := p.waitFor(req.ID, TypeFileWriteResponse)
	if err != nil {
		return "", fmt.Errorf("file write %q: %w", path, err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("file write %q: %s", path, resp.Error.Message)
	}
	return resp.Path, nil
}

// FileWriteFrom writes a file using the source_path handoff mechanism.
// The source file must be in the plugin's tmpdir and have Nlink == 1.
// Returns the absolute resolved path of the written file.
func (p *Plugin) FileWriteFrom(path, sourcePath string, opts ...FileWriteOption) (string, error) {
	req := Message{
		ID:         p.nextMsgID("fw"),
		ParentID:   p.callID,
		Type:       TypeFileWrite,
		Path:       path,
		SourcePath: sourcePath,
	}
	for _, opt := range opts {
		opt(&req)
	}
	// source_path mode: encoding is irrelevant (content comes from file).
	// Clear it to prevent HasContent false-positive in the core.
	req.BodyEncoding = ""

	p.send(req)

	resp, err := p.waitFor(req.ID, TypeFileWriteResponse)
	if err != nil {
		return "", fmt.Errorf("file write from %q: %w", path, err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("file write from %q: %s", path, resp.Error.Message)
	}
	return resp.Path, nil
}

// FileRead reads a file from the plugin's output directory.
// Returns the file content. Defaults to "text" encoding.
func (p *Plugin) FileRead(path string, opts ...FileReadOption) ([]byte, error) {
	req := Message{
		ID:       p.nextMsgID("fr"),
		ParentID: p.callID,
		Type:     TypeFileRead,
		Path:     path,
	}
	for _, opt := range opts {
		opt(&req)
	}

	p.send(req)

	resp, err := p.waitFor(req.ID, TypeFileReadResponse)
	if err != nil {
		return nil, fmt.Errorf("file read %q: %w", path, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("file read %q: %s", path, resp.Error.Message)
	}

	encoding := req.BodyEncoding
	if encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(resp.Content)
		if err != nil {
			return nil, fmt.Errorf("file read %q: decode base64: %w", path, err)
		}
		return decoded, nil
	}
	return []byte(resp.Content), nil
}

// waitFor reads messages from stdin until it gets one matching the given id.
// This implements the synchronous request-response pattern used by
// concurrency=1 plugin handlers.
func (p *Plugin) waitFor(id, expectedType string) (Message, error) {
	for {
		msg, err := p.recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return Message{}, fmt.Errorf("unexpected EOF waiting for %s %s", expectedType, id)
			}
			return Message{}, err
		}
		if msg.ID == id {
			return msg, nil
		}
		// Unexpected message while waiting — log and skip.
		// This shouldn't happen with concurrency=1 plugins.
		p.logger.Printf("warning: unexpected message type=%s id=%s while waiting for %s", msg.Type, msg.ID, id)
	}
}
