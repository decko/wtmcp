package highlighter

import (
	"strings"
	"testing"

	"google.golang.org/api/docs/v1"
)

func TestHighlightCode(t *testing.T) {
	// Load Python config for testing
	cfg, err := LoadConfig("python")
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	code := `def hello(name):
    """Say hello."""
    print(f"Hello, {name}!")
    return True
`

	segments, err := HighlightCode(code, "python", cfg)
	if err != nil {
		t.Fatalf("HighlightCode() failed: %v", err)
	}

	if len(segments) == 0 {
		t.Errorf("HighlightCode() returned no segments")
	}

	// Verify some segments have color applied
	hasColor := false
	for _, seg := range segments {
		if seg.Color.Red > 0 || seg.Color.Green > 0 || seg.Color.Blue > 0 {
			hasColor = true
			break
		}
	}
	if !hasColor {
		t.Errorf("HighlightCode() no segments have color applied")
	}

	// Verify text reconstruction
	var reconstructed string
	for _, seg := range segments {
		reconstructed += seg.Text
	}
	if reconstructed != code {
		t.Errorf("HighlightCode() text mismatch\ngot:  %q\nwant: %q", reconstructed, code)
	}
}

func TestMergeSegments(t *testing.T) {
	red := &docs.RgbColor{Red: 1.0, Green: 0.0, Blue: 0.0}
	redCopy := &docs.RgbColor{Red: 1.0, Green: 0.0, Blue: 0.0}
	blue := &docs.RgbColor{Red: 0.0, Green: 0.0, Blue: 1.0}

	tests := []struct {
		name     string
		input    []FormattedSegment
		wantLen  int
		wantText string
	}{
		{
			name:     "empty input",
			input:    []FormattedSegment{},
			wantLen:  0,
			wantText: "",
		},
		{
			name:     "single segment",
			input:    []FormattedSegment{{Text: "hello", Color: red, Bold: false}},
			wantLen:  1,
			wantText: "hello",
		},
		{
			name: "same style merges",
			input: []FormattedSegment{
				{Text: "hel", Color: red, Bold: true},
				{Text: "lo", Color: red, Bold: true},
			},
			wantLen:  1,
			wantText: "hello",
		},
		{
			name: "value-equal colors merge",
			input: []FormattedSegment{
				{Text: "hel", Color: red, Bold: false},
				{Text: "lo", Color: redCopy, Bold: false},
			},
			wantLen:  1,
			wantText: "hello",
		},
		{
			name: "different color stays separate",
			input: []FormattedSegment{
				{Text: "red", Color: red, Bold: false},
				{Text: "blue", Color: blue, Bold: false},
			},
			wantLen:  2,
			wantText: "redblue",
		},
		{
			name: "different bold stays separate",
			input: []FormattedSegment{
				{Text: "normal", Color: red, Bold: false},
				{Text: "bold", Color: red, Bold: true},
			},
			wantLen:  2,
			wantText: "normalbold",
		},
		{
			name: "nil colors merge",
			input: []FormattedSegment{
				{Text: "a", Color: nil, Bold: false},
				{Text: "b", Color: nil, Bold: false},
			},
			wantLen:  1,
			wantText: "ab",
		},
		{
			name: "nil vs non-nil stays separate",
			input: []FormattedSegment{
				{Text: "a", Color: nil, Bold: false},
				{Text: "b", Color: red, Bold: false},
			},
			wantLen:  2,
			wantText: "ab",
		},
		{
			name: "merge-split-merge pattern",
			input: []FormattedSegment{
				{Text: "a", Color: red, Bold: false},
				{Text: "b", Color: red, Bold: false},
				{Text: "c", Color: blue, Bold: false},
				{Text: "d", Color: blue, Bold: false},
			},
			wantLen:  2,
			wantText: "abcd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeSegments(tt.input)
			if len(result) != tt.wantLen {
				t.Errorf("MergeSegments() len = %d, want %d", len(result), tt.wantLen)
			}
			var text string
			for _, seg := range result {
				text += seg.Text
			}
			if text != tt.wantText {
				t.Errorf("MergeSegments() text = %q, want %q", text, tt.wantText)
			}
		})
	}
}

func TestHighlightCodeSizeLimit(t *testing.T) {
	cfg, err := LoadConfig("python")
	if err != nil {
		t.Fatalf("LoadConfig() failed: %v", err)
	}

	// Exactly at the limit should succeed
	atLimit := strings.Repeat("x", maxHighlightSize)
	_, err = HighlightCode(atLimit, "python", cfg)
	if err != nil {
		t.Errorf("HighlightCode() at limit (%d bytes) should succeed, got: %v", len(atLimit), err)
	}

	// One byte over should fail
	overLimit := strings.Repeat("x", maxHighlightSize+1)
	_, err = HighlightCode(overLimit, "python", cfg)
	if err == nil {
		t.Errorf("HighlightCode() over limit (%d bytes) should fail", len(overLimit))
	}
}

func TestHighlightCodeUnsupportedLanguage(t *testing.T) {
	cfg := &Config{
		Language: "unknown",
		Styles: map[string]Style{
			"default": {Color: "#24292E", Bold: false, Italic: false},
		},
	}

	_, err := HighlightCode("some code", "unknownlang", cfg)
	if err == nil {
		t.Errorf("HighlightCode() expected error for unsupported language, got nil")
	}
}
