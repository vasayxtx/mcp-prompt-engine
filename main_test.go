package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vasayxtx/mcp-custom-prompts/mcptest"
)

func TestExtractTemplateArguments(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		content     string
		partials    map[string]string
		expected    []string
		description string
		shouldError bool
	}{
		{
			name:        "empty template",
			content:     "{{/* Empty template */}}\nNo arguments here",
			partials:    map[string]string{},
			expected:    []string{},
			description: "Empty template",
			shouldError: false,
		},
		{
			name:        "single argument",
			content:     "{{/* Single argument template */}}\nHello {{.name}}",
			partials:    map[string]string{},
			expected:    []string{"name"},
			description: "Single argument template",
			shouldError: false,
		},
		{
			name:        "multiple arguments",
			content:     "{{/* Multiple arguments template */}}\nHello {{.name}}, your project is {{.project}} and language is {{.language}}",
			partials:    map[string]string{},
			expected:    []string{"name", "project", "language"},
			description: "Multiple arguments template",
			shouldError: false,
		},
		{
			name:        "arguments with built-in date",
			content:     "{{/* Template with date */}}\nToday is {{.date}} and user is {{.username}}",
			partials:    map[string]string{},
			expected:    []string{"username"}, // date is built-in, should be filtered out
			description: "Template with date",
			shouldError: false,
		},
		{
			name:        "template with used partial only",
			content:     "{{/* Template with used partial only */}}\n{{template \"_header\" dict \"role\" .role \"task\" .task}}\nUser: {{.username}}",
			partials:    map[string]string{"header": "You are {{.role}} doing {{.task}}", "footer": "End with {{.conclusion}}"},
			expected:    []string{"role", "task", "username"}, // should NOT include conclusion from unused footer
			description: "Template with used partial only",
			shouldError: false,
		},
		{
			name:        "template with multiple used partials",
			content:     "{{/* Template with multiple partials */}}\n{{template \"_header\" dict \"role\" .role}}\n{{template \"_footer\" dict \"conclusion\" .conclusion}}\nUser: {{.username}}",
			partials:    map[string]string{"header": "You are {{.role}}", "footer": "End with {{.conclusion}}", "unused": "This has {{.unused_var}}"},
			expected:    []string{"role", "conclusion", "username"}, // should NOT include unused_var
			description: "Template with multiple partials",
			shouldError: false,
		},
		{
			name:        "template with no partials used",
			content:     "{{/* Template with no partials */}}\nJust {{.simple}} content",
			partials:    map[string]string{"header": "You are {{.role}}", "footer": "End with {{.conclusion}}"},
			expected:    []string{"simple"}, // should NOT include role or conclusion
			description: "Template with no partials used",
			shouldError: false,
		},
		{
			name:        "duplicate arguments",
			content:     "{{/* Duplicate arguments */}}\n{{.user}} said hello to {{.user}} again",
			partials:    map[string]string{},
			expected:    []string{"user"},
			description: "Duplicate arguments",
			shouldError: false,
		},
		{
			name:        "argument in if statement",
			content:     "{{/* Template with if statement */}}\\n{{if .show_details}}Details: {{.details_text}}{{end}}\\nAlways show: {{.always_visible}}",
			partials:    map[string]string{},
			expected:    []string{"show_details", "details_text", "always_visible"},
			description: "Template with if statement",
			shouldError: false,
		},
		{
			name:    "cyclic partial references",
			content: "{{/* Template with cyclic partials */}}\n{{template \"_a\" .}}\nMain content: {{.main}}",
			partials: map[string]string{
				"_a": "Partial A with {{.a_var}} {{template \"_b\" .}}",
				"_b": "Partial B with {{.b_var}} {{template \"_c\" .}}",
				"_c": "Partial C with {{.c_var}} {{template \"_a\" .}}", // Creates a cycle: a -> b -> c -> a
			},
			expected:    nil,
			description: "Template with cyclic partials",
			shouldError: true,
		},
		{
			name:    "deeply nested partials",
			content: "{{/* Template with deeply nested partials */}}\n{{template \"_level1\" .}}\nMain content: {{.main_var}}",
			partials: map[string]string{
				"_level1": "Level 1 with {{.level1_var}} {{template \"_level2\" .}}",
				"_level2": "Level 2 with {{.level2_var}} {{template \"_level3\" .}}",
				"_level3": "Level 3 with {{.level3_var}} {{template \"_level4\" .}}",
				"_level4": "Level 4 with {{.level4_var}}",
				"_unused": "This partial is not used {{.unused_var}}",
			},
			expected:    []string{"level1_var", "level2_var", "level3_var", "level4_var", "main_var"},
			description: "Template with deeply nested partials",
			shouldError: false,
		},
		{
			name:        "template with or condition",
			content:     "{{/* Template with or condition */}}\n{{if or .show_message .show_alert}}Message: {{.message}}{{end}}\nAlways: {{.name}}",
			partials:    map[string]string{},
			expected:    []string{"show_message", "show_alert", "message", "name"},
			description: "Template with or condition",
			shouldError: false,
		},
		{
			name:        "template with and condition",
			content:     "{{/* Template with and condition */}}\n{{if and .is_enabled .has_permission}}Action: {{.action}}{{end}}\nUser: {{.username}}",
			partials:    map[string]string{},
			expected:    []string{"is_enabled", "has_permission", "action", "username"},
			description: "Template with and condition",
			shouldError: false,
		},
		{
			name:        "template with complex or and conditions",
			content:     "{{/* Template with complex conditions */}}\n{{if or (and .is_admin .has_access) .force_mode}}Admin panel: {{.admin_data}}{{end}}\n{{if and .show_stats .is_premium}}Stats: {{.statistics}}{{end}}\nGeneral: {{.content}}",
			partials:    map[string]string{},
			expected:    []string{"is_admin", "has_access", "force_mode", "admin_data", "show_stats", "is_premium", "statistics", "content"},
			description: "Template with complex conditions",
			shouldError: false,
		},
		{
			name:    "template with or in partials",
			content: "{{/* Template with or in partials */}}\n{{template \"_conditional\" .}}\nMain: {{.main_content}}",
			partials: map[string]string{
				"_conditional": "{{if or .show_warning .show_error}}Alert: {{.alert_message}}{{end}}",
			},
			expected:    []string{"show_warning", "show_error", "alert_message", "main_content"},
			description: "Template with or in partials",
			shouldError: false,
		},
		{
			name:        "template with range node",
			content:     "{{/* Template with range */}}\n{{range .items}}Item: {{.name}} - {{.value}}{{end}}\nTotal: {{.total}}",
			partials:    map[string]string{},
			expected:    []string{"items", "name", "value", "total"},
			description: "Template with range",
			shouldError: false,
		},
		{
			name:        "template with with node",
			content:     "{{/* Template with with */}}\n{{with .user}}Name: {{.name}}, Email: {{.email}}{{end}}\nDefault: {{.default_value}}",
			partials:    map[string]string{},
			expected:    []string{"user", "name", "email", "default_value"},
			description: "Template with with",
			shouldError: false,
		},
		{
			name:        "template with variables",
			content:     "{{/* Template with variables */}}\n{{$name := .user_name}}{{$email := .user_email}}User: {{$name}} ({{$email}}) - Role: {{.role}}",
			partials:    map[string]string{},
			expected:    []string{"user_name", "user_email", "role"},
			description: "Template with variables",
			shouldError: false,
		},
		{
			name:        "template with range and else",
			content:     "{{/* Template with range and else */}}\n{{range .items}}{{.name}}{{else}}No items: {{.empty_message}}{{end}}",
			partials:    map[string]string{},
			expected:    []string{"items", "name", "empty_message"},
			description: "Template with range and else",
			shouldError: false,
		},
		{
			name:        "template with if and else",
			content:     "{{/* Template with if and else */}}\n{{if .show_content}}Content: {{.content}}{{else}}Default: {{.default_content}}{{end}}",
			partials:    map[string]string{},
			expected:    []string{"show_content", "content", "default_content"},
			description: "Template with if and else",
			shouldError: false,
		},
		{
			name:        "template with with and else",
			content:     "{{/* Template with with and else */}}\n{{with .user}}Name: {{.name}}{{else}}No user: {{.default_name}}{{end}}",
			partials:    map[string]string{},
			expected:    []string{"user", "name", "default_name"},
			description: "Template with with and else",
			shouldError: false,
		},
		{
			name:        "template with direct template node",
			content:     "{{/* Template with direct template node */}}\n{{template \"_direct\"}}\nMain: {{.main_var}}",
			partials:    map[string]string{"_direct": "Direct template with {{.direct_var}}"},
			expected:    []string{"direct_var", "main_var"},
			description: "Template with direct template node",
			shouldError: false,
		},
		{
			name:        "template with action node",
			content:     "{{/* Template with action node */}}\n{{ print .message }}\nOther: {{.other}}",
			partials:    map[string]string{},
			expected:    []string{"message", "other"},
			description: "Template with action node",
			shouldError: false,
		},
		{
			name:        "template with template calls",
			content:     "{{/* Template with template calls */}}\n{{template \"_helper\" dict \"param\" .value}}\nMain: {{.main}}",
			partials:    map[string]string{"_helper": "Helper with {{.param}}"},
			expected:    []string{"value", "main", "param"},
			description: "Template with template calls",
			shouldError: false,
		},
		{
			name:        "template with unresolved template calls",
			content:     "{{/* Template with unresolved calls */}}\n{{template \"missing_partial\" .}}\nMain: {{.main}}",
			partials:    map[string]string{},
			expected:    []string{"main"},
			description: "Template with unresolved calls",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for this test
			testDir := filepath.Join(tempDir, tt.name)
			err := os.MkdirAll(testDir, 0755)
			if err != nil {
				t.Fatalf("Failed to create test directory: %v", err)
			}

			// Write the main template file
			testFile := filepath.Join(testDir, tt.name+".tmpl")
			err = os.WriteFile(testFile, []byte(tt.content), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Write partial files
			for partialName, partialContent := range tt.partials {
				partialFile := filepath.Join(testDir, partialName+".tmpl")
				err = os.WriteFile(partialFile, []byte(partialContent), 0644)
				if err != nil {
					t.Fatalf("Failed to write partial file: %v", err)
				}
			}

			// Parse all templates in the test directory
			tmpl, err := parseAllPrompts(testDir)
			if err != nil {
				t.Fatalf("Failed to parse templates: %v", err)
			}

			got, err := extractPromptArguments(tmpl, tt.name)

			if tt.shouldError {
				if err == nil {
					t.Errorf("extractPromptArguments() expected error, but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("extractPromptArguments() error = %v", err)
			}

			// Sort both slices for consistent comparison
			sort.Strings(got)
			sort.Strings(tt.expected)

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("extractPromptArguments() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractPromptDescription(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name                string
		content             string
		expectedDescription string
	}{
		{
			name:                "valid template with description",
			content:             "{{/* Template description */}}",
			expectedDescription: "Template description",
		},
		{
			name:                "valid template with description, comment starts with dash",
			content:             "{{- /* Template description */}}",
			expectedDescription: "Template description",
		},
		{
			name:                "valid template with description, comment ends with dash",
			content:             "{{/* Template description */ -}}",
			expectedDescription: "Template description",
		},
		{
			name:                "valid template with description, comment starts and ends with dash",
			content:             "{{- /* Template description */ -}}",
			expectedDescription: "Template description",
		},
		{
			name:                "template without description",
			content:             "Hello {{.name}}",
			expectedDescription: "",
		},
		{
			name:                "template with valid comment and trim",
			content:             "{{/* Comment */}}",
			expectedDescription: "Comment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFile := filepath.Join(tempDir, tt.name+".tmpl")
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}
			description, err := extractPromptDescription(testFile)
			if err != nil {
				t.Fatalf("parseTemplateFile() error = %v", err)
			}
			if description != tt.expectedDescription {
				t.Errorf("parseTemplateFile() description = %v, want %v", description, tt.expectedDescription)
			}
		})
	}
}

func TestExtractPromptDescriptionErrorCases(t *testing.T) {
	// Test non-existent file
	_, err := extractPromptDescription("/non/existent/file.tmpl")
	if err == nil {
		t.Error("extractPromptDescription() expected error for non-existent file, but got none")
	}
}

func TestExtractPromptArgumentsErrorCases(t *testing.T) {
	tempDir := t.TempDir()

	// Create a valid template file so parseAllPrompts doesn't fail
	testFile := filepath.Join(tempDir, "test.tmpl")
	err := os.WriteFile(testFile, []byte("{{/* Test */}}\nHello {{.name}}"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Test non-existent template
	tmpl, err := parseAllPrompts(tempDir)
	if err != nil {
		t.Fatalf("Failed to parse templates: %v", err)
	}

	_, err = extractPromptArguments(tmpl, "non_existent_template")
	if err == nil {
		t.Error("extractPromptArguments() expected error for non-existent template, but got none")
	}
}

func TestBuildPromptsErrorCases(t *testing.T) {
	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	// Test non-existent directory
	err := addPromptHandlers(srv, "/non/existent/directory", slog.New(slog.DiscardHandler))
	if err == nil {
		t.Error("addPromptHandlers() expected error for non-existent directory, but got none")
	}

	// Test directory that exists but can't be read (permission issue simulation)
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.tmpl")
	err = os.WriteFile(testFile, []byte("{{/* Test */}}\nHello {{.name}}"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create a file instead of directory to simulate ReadDir error
	invalidDir := filepath.Join(tempDir, "not_a_dir.txt")
	err = os.WriteFile(invalidDir, []byte("not a directory"), 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid dir file: %v", err)
	}

	// This should trigger the ReadDir error path in addPromptHandlers
	err = addPromptHandlers(srv, invalidDir, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Error("addPromptHandlers() expected error when ReadDir fails, but got none")
	}

	// Test error case with directory that has templates but parseAllPrompts will fail after ReadDir succeeds
	badTemplateDir := t.TempDir()
	err = os.WriteFile(filepath.Join(badTemplateDir, "good.tmpl"), []byte("{{/* Good */}}\nGood template"), 0644)
	if err != nil {
		t.Fatalf("Failed to write good template: %v", err)
	}
	err = os.WriteFile(filepath.Join(badTemplateDir, "bad.tmpl"), []byte("{{/* Bad */}}\n{{unclosed"), 0644)
	if err != nil {
		t.Fatalf("Failed to write bad template: %v", err)
	}

	err = addPromptHandlers(srv, badTemplateDir, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Error("addPromptHandlers() expected error for bad template syntax, but got none")
	}
}

func TestRenderTemplateErrorCases(t *testing.T) {
	var buf bytes.Buffer

	// Test non-existent directory
	err := renderTemplate(&buf, "/non/existent/directory", "template_name")
	if err == nil {
		t.Error("renderTemplate() expected error for non-existent directory, but got none")
	}

	// Test template execution error with missing template
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "error.tmpl")
	// Create a template that will cause execution error (missing template reference)
	err = os.WriteFile(testFile, []byte("{{/* Error template */}}\n{{template \"missing_template\" .}}"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	var errorBuf bytes.Buffer
	err = renderTemplate(&errorBuf, tempDir, "error")
	if err == nil {
		t.Error("renderTemplate() expected execution error for missing template, but got none")
	}

	// Test error with non-existent template in renderTemplate
	var nonExistentBuf bytes.Buffer
	err = renderTemplate(&nonExistentBuf, tempDir, "does_not_exist")
	if err == nil {
		t.Error("renderTemplate() expected error for non-existent template, but got none")
	}
}

func TestParseAllPromptsErrorCases(t *testing.T) {
	// Test non-existent directory
	_, err := parseAllPrompts("/non/existent/directory")
	if err == nil {
		t.Error("parseAllPrompts() expected error for non-existent directory, but got none")
	}

	// Test directory with invalid template syntax
	tempDir := t.TempDir()
	invalidFile := filepath.Join(tempDir, "invalid.tmpl")
	err = os.WriteFile(invalidFile, []byte("{{/* Invalid template */}}\n{{.unclosed"), 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid template file: %v", err)
	}

	_, err = parseAllPrompts(tempDir)
	if err == nil {
		t.Error("parseAllPrompts() expected error for invalid template syntax, but got none")
	}
}

func TestWalkNodesNilHandling(t *testing.T) {
	// Test walkNodes with nil nodes - this is the path that's not covered
	argsMap := make(map[string]struct{})
	builtInFields := map[string]struct{}{"date": {}}
	processedTemplates := make(map[string]bool)

	// This should return nil immediately for nil node
	err := walkNodes(nil, argsMap, builtInFields, nil, processedTemplates, []string{})
	if err != nil {
		t.Errorf("walkNodes() with nil node should return nil, but got error: %v", err)
	}

	// argsMap should remain empty
	if len(argsMap) != 0 {
		t.Errorf("walkNodes() with nil node should not modify argsMap, but got %d entries", len(argsMap))
	}
}

func TestWalkNodesVariableHandling(t *testing.T) {
	tempDir := t.TempDir()

	// Create a template with a variable (non-$ variable)
	testFile := filepath.Join(tempDir, "test.tmpl")
	err := os.WriteFile(testFile, []byte("{{/* Test template */}}\n{{$var := .input}}{{$var}}"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	tmpl, err := parseAllPrompts(tempDir)
	if err != nil {
		t.Fatalf("Failed to parse templates: %v", err)
	}

	// Test extracting arguments - should handle variable nodes properly
	args, err := extractPromptArguments(tmpl, "test")
	if err != nil {
		t.Fatalf("extractPromptArguments() unexpected error: %v", err)
	}

	// Should only contain "input", not the template variables
	expected := []string{"input"}
	if len(args) != len(expected) {
		t.Errorf("extractPromptArguments() returned %d args, want %d", len(args), len(expected))
	}

	for _, expectedArg := range expected {
		found := false
		for _, arg := range args {
			if arg == expectedArg {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("extractPromptArguments() missing expected arg: %s", expectedArg)
		}
	}
}

func TestPromptHandlerErrorCases(t *testing.T) {
	// Test promptHandler with invalid directory
	handler := promptHandler("/non/existent/directory", "test", "Test", map[string]string{})

	_, err := handler(context.Background(), mcp.GetPromptRequest{})
	if err == nil {
		t.Error("promptHandler() expected error for non-existent directory, but got none")
	}

	// Test promptHandler with template resolution error
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.tmpl")
	err = os.WriteFile(testFile, []byte("{{/* Test */}}\nHello {{.name}}"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create handler for a non-existent template
	handler2 := promptHandler(tempDir, "nonexistent", "Test", map[string]string{})
	_, err = handler2(context.Background(), mcp.GetPromptRequest{})
	if err == nil {
		t.Error("promptHandler() expected error for non-existent template, but got none")
	}

	// Test promptHandler with template execution error
	errorFile := filepath.Join(tempDir, "error.tmpl")
	err = os.WriteFile(errorFile, []byte("{{/* Error */}}\n{{template \"missing\" .}}"), 0644)
	if err != nil {
		t.Fatalf("Failed to write error file: %v", err)
	}

	handler3 := promptHandler(tempDir, "error", "Test", map[string]string{})
	_, err = handler3(context.Background(), mcp.GetPromptRequest{})
	if err == nil {
		t.Error("promptHandler() expected execution error, but got none")
	}
}

func TestDict(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected map[string]interface{}
		hasError bool
	}{
		{
			name:     "empty args",
			args:     []string{},
			expected: map[string]interface{}{},
			hasError: false,
		},
		{
			name:     "single key-value pair",
			args:     []string{"key", "value"},
			expected: map[string]interface{}{"key": "value"},
			hasError: false,
		},
		{
			name:     "multiple key-value pairs",
			args:     []string{"key1", "value1", "key2", "value2"},
			expected: map[string]interface{}{"key1": "value1", "key2": "value2"},
			hasError: false,
		},
		{
			name:     "odd number of arguments",
			args:     []string{"key1", "value1", "key2"},
			expected: nil,
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert string slice to interface slice
			args := make([]interface{}, len(tt.args))
			for i, v := range tt.args {
				args[i] = v
			}

			result := dict(args...)
			if tt.hasError {
				if result != nil {
					t.Error("dict() expected nil result for error case, but got non-nil")
				}
				return
			}
			if result == nil {
				t.Error("dict() unexpected nil result")
				return
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("dict() = %v, want %v", result, tt.expected)
			}
		})
	}

	// Test non-string key
	t.Run("non-string key", func(t *testing.T) {
		result := dict(123, "value")
		if result != nil {
			t.Error("dict() expected nil result for non-string key, but got non-nil")
		}
	})
}

func TestRenderTemplate(t *testing.T) {
	tests := []struct {
		name           string
		templateName   string
		envVars        map[string]string
		expectedOutput string
		shouldError    bool
	}{
		{
			name:           "greeting template, env var not set",
			templateName:   "greeting",
			expectedOutput: "Hello {{ name }}!\nHave a great day!",
			shouldError:    false,
		},
		{
			name:         "greeting template",
			templateName: "greeting",
			envVars: map[string]string{
				"NAME": "John",
			},
			expectedOutput: "Hello John!\nHave a great day!",
			shouldError:    false,
		},
		{
			name:         "template with partials, some env vars not set",
			templateName: "multiple_partials",
			envVars: map[string]string{
				"TITLE":   "Test Document",
				"NAME":    "Bob",
				"VERSION": "1.0.0",
			},
			expectedOutput: "# Test Document\nCreated by: {{ author }}\n## Description\n{{ description }}\n## Details\nThis is a test template with multiple partials.\nHello Bob!\nVersion: 1.0.0",
			shouldError:    false,
		},
		{
			name:         "template with partials",
			templateName: "multiple_partials",
			envVars: map[string]string{
				"TITLE":       "Test Document",
				"AUTHOR":      "Test Author",
				"NAME":        "Bob",
				"DESCRIPTION": "This is a test description",
				"VERSION":     "1.0.0",
			},
			expectedOutput: "# Test Document\nCreated by: Test Author\n## Description\nThis is a test description\n## Details\nThis is a test template with multiple partials.\nHello Bob!\nVersion: 1.0.0",
			shouldError:    false,
		},
		{
			name:         "conditional greeting, show extra message true",
			templateName: "conditional_greeting",
			envVars: map[string]string{
				"NAME":               "Alice",
				"SHOW_EXTRA_MESSAGE": "true",
			},
			expectedOutput: "Hello Alice!\nThis is an extra message just for you.\nHave a good day.",
			shouldError:    false,
		},
		{
			name:         "conditional greeting, show extra message false",
			templateName: "conditional_greeting",
			envVars: map[string]string{
				"NAME":               "Bob",
				"SHOW_EXTRA_MESSAGE": "",
			},
			expectedOutput: "Hello Bob!\nHave a good day.",
			shouldError:    false,
		},
		{
			name:           "non-existent template",
			templateName:   "non_existent_template",
			envVars:        map[string]string{},
			expectedOutput: "",
			shouldError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalEnv := make(map[string]string)
			for k := range tt.envVars {
				originalEnv[k] = os.Getenv(k)
			}
			defer func() {
				for k, v := range originalEnv {
					os.Setenv(k, v)
				}
			}()

			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			var buf bytes.Buffer
			err := renderTemplate(&buf, "./testdata", tt.templateName)

			if tt.shouldError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			output := normalizeNewlines(buf.String())
			if output != tt.expectedOutput {
				t.Errorf("expected output %q, got %q", tt.expectedOutput, output)
			}
		})
	}
}

func TestServerWithPrompt(t *testing.T) {
	ctx := context.Background()

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	if err := addPromptHandlers(srv, "./testdata", slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("addPromptHandlers failed: %v", err)
	}

	err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name                string
		promptName          string
		promptArgs          map[string]string
		expectedDescription string
		expectedMessages    []mcp.PromptMessage
	}{
		{
			name:                "greeting prompt",
			promptName:          "greeting",
			promptArgs:          map[string]string{"name": "John"},
			expectedDescription: "Greeting standalone template with no partials",
			expectedMessages: []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.NewTextContent("Hello John!\nHave a great day!"),
				},
			},
		},
		{
			name:                "greeting with partials",
			promptName:          "greeting_with_partials",
			promptArgs:          map[string]string{"name": "Alice"},
			expectedDescription: "Greeting template with partial",
			expectedMessages: []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.NewTextContent("Hello Alice!\nWelcome to the system.\nHave a great day!"),
				},
			},
		},
		{
			name:       "template with multiple partials",
			promptName: "multiple_partials",
			promptArgs: map[string]string{
				"title":       "Test Document",
				"author":      "Test Author",
				"name":        "Bob",
				"description": "This is a test description",
				"version":     "1.0.0",
			},
			expectedDescription: "Template with multiple partials",
			expectedMessages: []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.NewTextContent("# Test Document\nCreated by: Test Author\n## Description\nThis is a test description\n## Details\nThis is a test template with multiple partials.\nHello Bob!\nVersion: 1.0.0"),
				},
			},
		},
		{
			name:       "conditional greeting, show extra true",
			promptName: "conditional_greeting",
			promptArgs: map[string]string{
				"name":               "Carlos",
				"show_extra_message": "true",
			},
			expectedDescription: "Conditional greeting template",
			expectedMessages: []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.NewTextContent("Hello Carlos!\nThis is an extra message just for you.\nHave a good day."),
				},
			},
		},
		{
			name:       "conditional greeting, show extra false",
			promptName: "conditional_greeting",
			promptArgs: map[string]string{
				"name":               "Diana",
				"show_extra_message": "",
			},
			expectedDescription: "Conditional greeting template",
			expectedMessages: []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.NewTextContent("Hello Diana!\nHave a good day."),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var getReq mcp.GetPromptRequest
			getReq.Params.Name = tt.promptName
			getReq.Params.Arguments = tt.promptArgs
			getResult, err := srv.Client().GetPrompt(ctx, getReq)
			if err != nil {
				t.Fatalf("GetPrompt failed: %v", err)
			}

			if getResult.Description != tt.expectedDescription {
				t.Errorf("Expected prompt description %q, got %q", tt.expectedDescription, getResult.Description)
			}

			if len(getResult.Messages) != len(tt.expectedMessages) {
				t.Fatalf("Expected %d messages, got %d", len(tt.expectedMessages), len(getResult.Messages))
			}

			for i, msg := range getResult.Messages {
				if msg.Role != tt.expectedMessages[i].Role {
					t.Errorf("Expected message role %q, got %q", tt.expectedMessages[i].Role, msg.Role)
				}
				content, ok := msg.Content.(mcp.TextContent)
				if !ok {
					t.Fatalf("Expected TextContent, got %T", msg.Content)
				}
				s := normalizeNewlines(content.Text)
				if s != tt.expectedMessages[i].Content.(mcp.TextContent).Text {
					t.Errorf("Expected message content %q, got %q", tt.expectedMessages[i].Content.(mcp.TextContent).Text, s)
				}
			}
		})
	}
}

var nlRegExp = regexp.MustCompile(`\n+`)

func normalizeNewlines(s string) string {
	s = nlRegExp.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}
