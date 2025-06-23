package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type PromptsParserTestSuite struct {
	suite.Suite
	parser  *PromptsParser
	tempDir string
}

func TestPromptsParserTestSuite(t *testing.T) {
	suite.Run(t, new(PromptsParserTestSuite))
}

func (s *PromptsParserTestSuite) SetupTest() {
	s.parser = &PromptsParser{}
	s.tempDir = s.T().TempDir()
}

// TestExtractTemplateArgumentsFromTemplate tests template argument extraction with various scenarios
func (s *PromptsParserTestSuite) TestExtractTemplateArgumentsFromTemplate() {
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
			name:        "duplicate arguments",
			content:     "{{/* Duplicate arguments */}}\n{{.user}} said hello to {{.user}} again",
			partials:    map[string]string{},
			expected:    []string{"user"},
			description: "Duplicate arguments",
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
			name:        "template with or condition",
			content:     "{{/* Template with or condition */}}\n{{if or .show_message .show_alert}}Message: {{.message}}{{end}}\nAlways: {{.name}}",
			partials:    map[string]string{},
			expected:    []string{"show_message", "show_alert", "message", "name"},
			description: "Template with or condition",
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
			name:        "template with range node",
			content:     "{{/* Template with range */}}\n{{range .items}}Item: {{.name}} - {{.value}}{{end}}\nTotal: {{.total}}",
			partials:    map[string]string{},
			expected:    []string{"items", "name", "value", "total"},
			description: "Template with range",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Create a temporary directory for this test
			testDir := filepath.Join(s.tempDir, tt.name)
			err := os.MkdirAll(testDir, 0755)
			require.NoError(s.T(), err, "Failed to create test directory")

			// Write the main template file
			testFile := filepath.Join(testDir, tt.name+".tmpl")
			err = os.WriteFile(testFile, []byte(tt.content), 0644)
			require.NoError(s.T(), err, "Failed to write test file")

			// Write partial files
			for partialName, partialContent := range tt.partials {
				partialFile := filepath.Join(testDir, partialName+".tmpl")
				err = os.WriteFile(partialFile, []byte(partialContent), 0644)
				require.NoError(s.T(), err, "Failed to write partial file")
			}

			// Parse all templates in the test directory
			tmpl, err := s.parser.ParseDir(testDir)
			require.NoError(s.T(), err, "Failed to parse templates")

			got, err := s.parser.ExtractPromptArgumentsFromTemplate(tmpl, tt.name)

			if tt.shouldError {
				assert.Error(s.T(), err, "ExtractPromptArgumentsFromTemplate() expected error, but got none")
				return
			}

			require.NoError(s.T(), err, "ExtractPromptArgumentsFromTemplate() unexpected error")

			// Sort both slices for consistent comparison
			sort.Strings(got)
			sort.Strings(tt.expected)

			assert.Equal(s.T(), tt.expected, got, "ExtractPromptArgumentsFromTemplate() returned unexpected arguments")
		})
	}
}

// TestExtractPromptDescriptionFromFile tests description extraction from template comments
func (s *PromptsParserTestSuite) TestExtractPromptDescriptionFromFile() {
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
		s.Run(tt.name, func() {
			testFile := filepath.Join(s.tempDir, tt.name+".tmpl")
			err := os.WriteFile(testFile, []byte(tt.content), 0644)
			require.NoError(s.T(), err, "Failed to write test file")

			description, err := s.parser.ExtractPromptDescriptionFromFile(testFile)
			require.NoError(s.T(), err, "ExtractPromptDescriptionFromFile() unexpected error")
			assert.Equal(s.T(), tt.expectedDescription, description, "ExtractPromptDescriptionFromFile() returned unexpected description")
		})
	}
}

// TestExtractPromptDescriptionFromFileErrorCases tests error cases for description extraction
func (s *PromptsParserTestSuite) TestExtractPromptDescriptionFromFileErrorCases() {
	// Test non-existent file
	_, err := s.parser.ExtractPromptDescriptionFromFile("/non/existent/file.tmpl")
	assert.Error(s.T(), err, "ExtractPromptDescriptionFromFile() expected error for non-existent file, but got none")
}

// TestExtractPromptArgumentsFromTemplateErrorCases tests error cases for argument extraction
func (s *PromptsParserTestSuite) TestExtractPromptArgumentsFromTemplateErrorCases() {
	// Create a valid template file so ParseDir doesn't fail
	testFile := filepath.Join(s.tempDir, "test.tmpl")
	err := os.WriteFile(testFile, []byte("{{/* Test */}}\nHello {{.name}}"), 0644)
	require.NoError(s.T(), err, "Failed to write test file")

	// Test non-existent template
	tmpl, err := s.parser.ParseDir(s.tempDir)
	require.NoError(s.T(), err, "Failed to parse templates")

	_, err = s.parser.ExtractPromptArgumentsFromTemplate(tmpl, "non_existent_template")
	assert.Error(s.T(), err, "ExtractPromptArgumentsFromTemplate() expected error for non-existent template, but got none")
}

// TestParseDirErrorCases tests error cases for template parsing
func (s *PromptsParserTestSuite) TestParseDirErrorCases() {
	// Test non-existent directory
	_, err := s.parser.ParseDir("/non/existent/directory")
	assert.Error(s.T(), err, "ParseDir() expected error for non-existent directory, but got none")

	// Test directory with invalid template syntax
	invalidFile := filepath.Join(s.tempDir, "invalid.tmpl")
	err = os.WriteFile(invalidFile, []byte("{{/* Invalid template */}}\n{{.unclosed"), 0644)
	require.NoError(s.T(), err, "Failed to write invalid template file")

	_, err = s.parser.ParseDir(s.tempDir)
	assert.Error(s.T(), err, "ParseDir() expected error for invalid template syntax, but got none")
}

// TestWalkNodesNilHandling tests nil node handling in walkNodes
func (s *PromptsParserTestSuite) TestWalkNodesNilHandling() {
	argsMap := make(map[string]struct{})
	builtInFields := map[string]struct{}{"date": {}}
	processedTemplates := make(map[string]bool)

	// This should return nil immediately for nil node
	err := s.parser.walkNodes(nil, argsMap, builtInFields, nil, processedTemplates, []string{})
	assert.NoError(s.T(), err, "walkNodes() with nil node should return nil")

	// argsMap should remain empty
	assert.Empty(s.T(), argsMap, "walkNodes() with nil node should not modify argsMap")
}

// TestWalkNodesVariableHandling tests variable node handling in walkNodes
func (s *PromptsParserTestSuite) TestWalkNodesVariableHandling() {
	// Create a template with a variable (non-$ variable)
	testFile := filepath.Join(s.tempDir, "test.tmpl")
	err := os.WriteFile(testFile, []byte("{{/* Test template */}}\n{{$var := .input}}{{$var}}"), 0644)
	require.NoError(s.T(), err, "Failed to write test file")

	tmpl, err := s.parser.ParseDir(s.tempDir)
	require.NoError(s.T(), err, "Failed to parse templates")

	// Test extracting arguments - should handle variable nodes properly
	args, err := s.parser.ExtractPromptArgumentsFromTemplate(tmpl, "test")
	require.NoError(s.T(), err, "ExtractPromptArgumentsFromTemplate() unexpected error")

	// Should only contain "input", not the template variables
	expected := []string{"input"}
	assert.Equal(s.T(), expected, args, "ExtractPromptArgumentsFromTemplate() should only return template data arguments, not dollar variables")
}

// TestDict tests the dict helper function
func (s *PromptsParserTestSuite) TestDict() {
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
		s.Run(tt.name, func() {
			// Convert string slice to interface slice
			args := make([]interface{}, len(tt.args))
			for i, v := range tt.args {
				args[i] = v
			}

			result := dict(args...)
			if tt.hasError {
				assert.Nil(s.T(), result, "dict() expected nil result for error case")
				return
			}
			assert.NotNil(s.T(), result, "dict() unexpected nil result")
			assert.Equal(s.T(), tt.expected, result, "dict() returned unexpected result")
		})
	}

	// Test non-string key
	s.Run("non-string key", func() {
		result := dict(123, "value")
		assert.Nil(s.T(), result, "dict() expected nil result for non-string key")
	})
}
