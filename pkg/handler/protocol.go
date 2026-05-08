// Package handler provides helpers for writing Go plugin handlers
// that communicate with wtmcp via the JSON-lines protocol.
//
// A Go plugin handler reads messages from stdin, processes tool calls,
// and writes responses to stdout. Logging goes to stderr.
package handler

import "encoding/json"

// Message is the wire format for communication with the core.
// Fields are selectively populated based on Type.
type Message struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Protocol string `json:"protocol,omitempty"`

	// tool_call fields
	Tool   string          `json:"tool,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Config json.RawMessage `json:"config,omitempty"`

	// tool_result fields
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
	Actions []Action        `json:"actions,omitempty"`

	// init_ok fields
	Domains      []string          `json:"domains,omitempty"`
	AuthBindings map[string]string `json:"auth_bindings,omitempty"`

	// resource provider fields
	URI       string        `json:"uri,omitempty"`
	Resources []ResourceDef `json:"resources,omitempty"`
	Content   string        `json:"content,omitempty"`
	MIMEType  string        `json:"mime_type,omitempty"`

	// http_request / http_response / file_write / file_read fields
	//
	// Path: HTTP endpoint path (http_request) or relative/resolved
	// file path (file_write, file_read, file_write_response).
	// Content: resource content (read_resource_ok) or file content
	// (file_write inline, file_read_response).
	// BodyEncoding: HTTP body encoding or file content encoding.
	NoAuth       bool              `json:"no_auth,omitempty"`
	Method       string            `json:"method,omitempty"`
	Path         string            `json:"path,omitempty"`
	URL          string            `json:"url,omitempty"`
	Query        map[string]any    `json:"query,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         json.RawMessage   `json:"body,omitempty"`
	BodyEncoding string            `json:"body_encoding,omitempty"`
	Status       int               `json:"status,omitempty"`

	// file_write / file_read fields
	SourcePath  string `json:"source_path,omitempty"`
	Permissions string `json:"permissions,omitempty"`
	Mkdir       *bool  `json:"mkdir,omitempty"`
	Size        *int64 `json:"size,omitempty"`

	// cache fields
	Key     string          `json:"key,omitempty"`
	Value   json.RawMessage `json:"value,omitempty"`
	TTL     *int            `json:"ttl,omitempty"`
	Hit     *bool           `json:"hit,omitempty"`
	OK      *bool           `json:"ok,omitempty"`
	Deleted *bool           `json:"deleted,omitempty"`
	Keys    []string        `json:"keys,omitempty"`
	Pattern string          `json:"pattern,omitempty"`
	Count   *int            `json:"count,omitempty"`

	// hasContent forces the "content" key into JSON even when Content
	// is empty. Used by FileWrite to support zero-byte file creation.
	// FileWriteFrom leaves this false so "content" is omitted.
	hasContent bool
}

// MarshalJSON implements custom marshaling to conditionally include the
// "content" key for file_write messages with empty content.
func (m Message) MarshalJSON() ([]byte, error) {
	type Alias Message
	a := (Alias)(m)
	data, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	if !m.hasContent || m.Content != "" {
		return data, nil
	}
	// Insert "content":"" into the JSON object. The standard marshal
	// omitted it due to omitempty — we patch it back in.
	// Find the closing '}' and insert before it.
	if len(data) > 1 && data[len(data)-1] == '}' {
		insert := []byte(`,"content":""}`)
		result := make([]byte, len(data)-1, len(data)-1+len(insert))
		copy(result, data[:len(data)-1])
		result = append(result, insert...)
		return result, nil
	}
	return data, nil
}

// Error is a structured error from a plugin handler.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return "[" + e.Code + "] " + e.Message
}

// Message type constants (wire-compatible with core protocol).
const (
	TypeInit              = "init"
	TypeInitOK            = "init_ok"
	TypeInitError         = "init_error"
	TypeShutdown          = "shutdown"
	TypeShutdownOK        = "shutdown_ok"
	TypeToolCall          = "tool_call"
	TypeToolResult        = "tool_result"
	TypeHTTPRequest       = "http_request"
	TypeHTTPResponse      = "http_response"
	TypeCacheGet          = "cache_get"
	TypeCacheSet          = "cache_set"
	TypeCacheDel          = "cache_del"
	TypeCacheList         = "cache_list"
	TypeCacheFlush        = "cache_flush"
	TypeFileWrite         = "file_write"
	TypeFileWriteResponse = "file_write_response"
	TypeFileRead          = "file_read"
	TypeFileReadResponse  = "file_read_response"
	TypeListResources     = "list_resources"
	TypeListResourcesOK   = "list_resources_ok"
	TypeReadResource      = "read_resource"
	TypeReadResourceOK    = "read_resource_ok"
)

// ResourceDef describes a resource provided by a plugin handler.
// Must be kept in sync with internal/protocol.ResourceDef.
type ResourceDef struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mime_type,omitempty"`
}

// Action describes a side effect that should happen after a tool result.
// Must be kept in sync with internal/protocol.Action.
type Action struct {
	Type string `json:"type"`
}
