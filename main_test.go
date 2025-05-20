package main

import (
	"reflect"
	"sort"
	"testing"
)

func TestExtractArguments(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "empty string",
			text:     "",
			expected: []string{},
		},
		{
			name:     "no arguments",
			text:     "This is a text with no arguments",
			expected: []string{},
		},
		{
			name:     "single argument",
			text:     "This is a text with {{one}} argument",
			expected: []string{"one"},
		},
		{
			name:     "multiple unique arguments",
			text:     "This is a text with {{first}} and {{second}} arguments",
			expected: []string{"first", "second"},
		},
		{
			name:     "duplicate arguments",
			text:     "This is a text with {{same}} argument used {{same}} twice",
			expected: []string{"same"},
		},
		{
			name:     "arguments with special characters",
			text:     "Arguments with {{special_chars}} and {{with-dash}} and {{with.dot}}",
			expected: []string{"special_chars", "with-dash", "with.dot"},
		},
		{
			name:     "arguments in different contexts",
			text:     "Argument in {{context}} and at the end {{end}}\n{{newline}} at start",
			expected: []string{"context", "end", "newline"},
		},
		{
			name:     "nested curly braces (not valid)",
			text:     "This has {{outer {{inner}} braces}}",
			expected: []string{"inner"},
		},
		{
			name:     "arguments with whitespace",
			text:     "Arguments with {{  whitespace  }} and {{no_whitespace}}",
			expected: []string{"whitespace", "no_whitespace"},
		},
		{
			name:     "arguments at beginning and end",
			text:     "{{beginning}} of text and end of text {{end}}",
			expected: []string{"beginning", "end"},
		},
		{
			name:     "real-world example from prompt",
			text:     "Project Root Path: {{project_root}}\nService Name: {{service_name}}\nService Path: {{service_path}}",
			expected: []string{"project_root", "service_name", "service_path"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractArguments(tt.text)

			// Sort both slices for consistent comparison
			sort.Strings(got)
			sort.Strings(tt.expected)

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("extractArguments() = %v, want %v", got, tt.expected)
			}
		})
	}
}
