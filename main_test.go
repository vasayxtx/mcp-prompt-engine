package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestExtractTemplateArguments(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		content     string
		partials    map[string]string
		expected    []string
		description string
	}{
		{
			name:        "empty template",
			content:     "{{/* Empty template */}}\nNo arguments here",
			partials:    map[string]string{},
			expected:    []string{},
			description: "Empty template",
		},
		{
			name:        "single argument",
			content:     "{{/* Single argument template */}}\nHello {{.name}}",
			partials:    map[string]string{},
			expected:    []string{"name"},
			description: "Single argument template",
		},
		{
			name:        "multiple arguments",
			content:     "{{/* Multiple arguments template */}}\nHello {{.name}}, your project is {{.project}} and language is {{.language}}",
			partials:    map[string]string{},
			expected:    []string{"name", "project", "language"},
			description: "Multiple arguments template",
		},
		{
			name:        "arguments with built-in date",
			content:     "{{/* Template with date */}}\nToday is {{.date}} and user is {{.username}}",
			partials:    map[string]string{},
			expected:    []string{"username"}, // date is built-in, should be filtered out
			description: "Template with date",
		},
		{
			name:        "template with used partial only",
			content:     "{{/* Template with used partial only */}}\n{{template \"_header\" dict \"role\" .role \"task\" .task}}\nUser: {{.username}}",
			partials:    map[string]string{"header": "You are {{.role}} doing {{.task}}", "footer": "End with {{.conclusion}}"},
			expected:    []string{"role", "task", "username"}, // should NOT include conclusion from unused footer
			description: "Template with used partial only",
		},
		{
			name:        "template with multiple used partials",
			content:     "{{/* Template with multiple partials */}}\n{{template \"_header\" dict \"role\" .role}}\n{{template \"_footer\" dict \"conclusion\" .conclusion}}\nUser: {{.username}}",
			partials:    map[string]string{"header": "You are {{.role}}", "footer": "End with {{.conclusion}}", "unused": "This has {{.unused_var}}"},
			expected:    []string{"role", "conclusion", "username"}, // should NOT include unused_var
			description: "Template with multiple partials",
		},
		{
			name:        "template with no partials used",
			content:     "{{/* Template with no partials */}}\nJust {{.simple}} content",
			partials:    map[string]string{"header": "You are {{.role}}", "footer": "End with {{.conclusion}}"},
			expected:    []string{"simple"}, // should NOT include role or conclusion
			description: "Template with no partials used",
		},
		{
			name:        "duplicate arguments",
			content:     "{{/* Duplicate arguments */}}\n{{.user}} said hello to {{.user}} again",
			partials:    map[string]string{},
			expected:    []string{"user"},
			description: "Duplicate arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tempDir, tt.name+".tmpl")
			err := os.WriteFile(testFile, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			got, err := extractTemplateArguments(testFile, tt.partials)
			if err != nil {
				t.Fatalf("extractTemplateArguments() error = %v", err)
			}

			// Sort both slices for consistent comparison
			sort.Strings(got)
			sort.Strings(tt.expected)

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("extractTemplateArguments() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseTemplateFile(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name                string
		content             string
		partials            map[string]string
		expectedDescription string
		shouldError         bool
	}{
		{
			name:                "valid template with description",
			content:             "{{/* This is a test template */}}\nHello {{.name}}!",
			partials:            map[string]string{},
			expectedDescription: "This is a test template",
			shouldError:         false,
		},
		{
			name:                "template without description",
			content:             "Hello {{.name}}!",
			partials:            map[string]string{},
			expectedDescription: "",
			shouldError:         false,
		},
		{
			name:                "template with partial",
			content:             "{{/* Template with partial */}}\n{{template \"test\" .}}",
			partials:            map[string]string{"test": "Hello {{.name}}"},
			expectedDescription: "Template with partial",
			shouldError:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(tempDir, tt.name+".tmpl")
			err := os.WriteFile(testFile, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			tmpl, description, err := parseTemplateFile(testFile, tt.partials)

			if tt.shouldError {
				if err == nil {
					t.Errorf("parseTemplateFile() expected error, but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseTemplateFile() error = %v", err)
			}

			if description != tt.expectedDescription {
				t.Errorf("parseTemplateFile() description = %v, want %v", description, tt.expectedDescription)
			}

			if tmpl == nil {
				t.Errorf("parseTemplateFile() template is nil")
			}
		})
	}
}

func TestFindUsedPartials(t *testing.T) {
	partials := map[string]string{
		"header": "Header content with {{.role}}",
		"footer": "Footer content with {{.conclusion}}",
		"unused": "Unused content with {{.unused_var}}",
	}

	tests := []struct {
		name     string
		content  string
		expected map[string]string
	}{
		{
			name:     "no partials used",
			content:  "Just some {{.simple}} content",
			expected: map[string]string{},
		},
		{
			name:     "single partial used",
			content:  "{{template \"_header\" dict \"role\" .role}}",
			expected: map[string]string{"header": "Header content with {{.role}}"},
		},
		{
			name:    "multiple partials used",
			content: "{{template \"_header\" .}} and {{template \"_footer\" .}}",
			expected: map[string]string{
				"header": "Header content with {{.role}}",
				"footer": "Footer content with {{.conclusion}}",
			},
		},
		{
			name:     "partial without underscore prefix",
			content:  "{{template \"header\" .}}",
			expected: map[string]string{"header": "Header content with {{.role}}"},
		},
		{
			name:    "mixed partial references",
			content: "{{template \"_header\" .}} and {{template \"footer\" .}}",
			expected: map[string]string{
				"header": "Header content with {{.role}}",
				"footer": "Footer content with {{.conclusion}}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findUsedPartials(tt.content, partials)

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("findUsedPartials() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestLoadPartials(t *testing.T) {
	tempDir := t.TempDir()

	// Create test partials
	partialFiles := map[string]string{
		"_header.tmpl": "{{/* Header partial */}}\nYou are {{.role}}",
		"_footer.tmpl": "{{/* Footer partial */}}\nEnd of prompt",
		"regular.tmpl": "{{/* Not a partial */}}\nRegular template",
	}

	for filename, content := range partialFiles {
		err := os.WriteFile(filepath.Join(tempDir, filename), []byte(content), 0644)
		if err != nil {
			t.Fatalf("Failed to write test file %s: %v", filename, err)
		}
	}

	partials, err := loadPartials(tempDir)
	if err != nil {
		t.Fatalf("loadPartials() error = %v", err)
	}

	expected := map[string]string{
		"header": "{{/* Header partial */}}\nYou are {{.role}}",
		"footer": "{{/* Footer partial */}}\nEnd of prompt",
	}

	if !reflect.DeepEqual(partials, expected) {
		t.Errorf("loadPartials() = %v, want %v", partials, expected)
	}
}
