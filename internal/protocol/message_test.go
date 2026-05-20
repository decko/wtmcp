package protocol

import (
	"encoding/json"
	"testing"
)

func TestHasJSONKey(t *testing.T) {
	tests := []struct {
		name string
		json string
		key  string
		want bool
	}{
		{
			name: "key present with value",
			json: `{"content":"hello","type":"file_write"}`,
			key:  "content",
			want: true,
		},
		{
			name: "key present with empty string",
			json: `{"content":"","type":"file_write"}`,
			key:  "content",
			want: true,
		},
		{
			name: "key absent",
			json: `{"type":"file_write","source_path":"/tmp/f"}`,
			key:  "content",
			want: false,
		},
		{
			name: "empty object",
			json: `{}`,
			key:  "content",
			want: false,
		},
		{
			name: "nested key not matched",
			json: `{"params":{"content":"nested"},"type":"tool_call"}`,
			key:  "content",
			want: false,
		},
		{
			name: "key as last field",
			json: `{"type":"file_write","content":"last"}`,
			key:  "content",
			want: true,
		},
		{
			name: "key with null value",
			json: `{"content":null}`,
			key:  "content",
			want: true,
		},
		{
			name: "key with numeric value",
			json: `{"content":42}`,
			key:  "content",
			want: true,
		},
		{
			name: "key with array value",
			json: `{"content":[1,2,3]}`,
			key:  "content",
			want: true,
		},
		{
			name: "invalid json",
			json: `not json`,
			key:  "content",
			want: false,
		},
		{
			name: "json array not object",
			json: `["content"]`,
			key:  "content",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasJSONKey([]byte(tt.json), tt.key)
			if got != tt.want {
				t.Errorf("hasJSONKey(%s, %q) = %v, want %v", tt.json, tt.key, got, tt.want)
			}
		})
	}
}

func TestUnmarshalJSON_HasContent(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		wantHas     bool
		wantContent string
	}{
		{
			name:        "content present and non-empty",
			json:        `{"id":"1","type":"file_write","content":"data"}`,
			wantHas:     true,
			wantContent: "data",
		},
		{
			name:        "content present but empty",
			json:        `{"id":"1","type":"file_write","content":""}`,
			wantHas:     true,
			wantContent: "",
		},
		{
			name:        "content absent with source_path",
			json:        `{"id":"1","type":"file_write","source_path":"/tmp/f"}`,
			wantHas:     false,
			wantContent: "",
		},
		{
			name:        "non-file-write message skips hasJSONKey",
			json:        `{"id":"1","type":"read_resource_ok","content":"resource data"}`,
			wantHas:     false,
			wantContent: "resource data",
		},
		{
			name:        "tool_result without content",
			json:        `{"id":"1","type":"tool_result","result":{"ok":true}}`,
			wantHas:     false,
			wantContent: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Message
			if err := json.Unmarshal([]byte(tt.json), &m); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if m.HasContent != tt.wantHas {
				t.Errorf("HasContent = %v, want %v", m.HasContent, tt.wantHas)
			}
			if m.Content != tt.wantContent {
				t.Errorf("Content = %q, want %q", m.Content, tt.wantContent)
			}
		})
	}
}

func TestParentIDRoundtrip(t *testing.T) {
	msg := Message{
		ID:       "http-1",
		ParentID: "req-42",
		Type:     TypeHTTPRequest,
		Method:   "GET",
		Path:     "/api/test",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ParentID != "req-42" {
		t.Errorf("ParentID = %q, want req-42", decoded.ParentID)
	}
}

func TestParentIDOmittedWhenEmpty(t *testing.T) {
	msg := Message{
		ID:   "http-1",
		Type: TypeHTTPRequest,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	raw := string(data)
	if contains(raw, "parent_id") {
		t.Errorf("empty ParentID should be omitted, got: %s", raw)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
