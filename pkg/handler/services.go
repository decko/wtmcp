package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
		ID:     p.nextMsgID("http"),
		Type:   TypeHTTPRequest,
		Method: method,
		Path:   path,
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
		ID:   p.nextMsgID("cache"),
		Type: TypeCacheGet,
		Key:  key,
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
		ID:    p.nextMsgID("cache"),
		Type:  TypeCacheSet,
		Key:   key,
		Value: data,
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
		ID:   p.nextMsgID("cache"),
		Type: TypeCacheFlush,
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
		ID:   p.nextMsgID("cache"),
		Type: TypeCacheDel,
		Key:  key,
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
