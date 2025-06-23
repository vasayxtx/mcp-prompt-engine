package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type MainTestSuite struct {
	suite.Suite
	tempDir string
}

func TestMainTestSuite(t *testing.T) {
	suite.Run(t, new(MainTestSuite))
}

func (s *MainTestSuite) SetupTest() {
	s.tempDir = s.T().TempDir()
}

// TestRenderTemplateErrorCases tests error cases for template rendering
func (s *MainTestSuite) TestRenderTemplateErrorCases() {
	var buf bytes.Buffer

	// Test non-existent directory
	err := renderTemplate(&buf, "/non/existent/directory", "template_name")
	assert.Error(s.T(), err, "renderTemplate() expected error for non-existent directory")

	// Test template execution error with missing template
	testFile := s.tempDir + "/error.tmpl"
	// Create a template that will cause execution error (missing template reference)
	err = os.WriteFile(testFile, []byte("{{/* Error template */}}\n{{template \"missing_template\" .}}"), 0644)
	require.NoError(s.T(), err, "Failed to write test file")

	var errorBuf bytes.Buffer
	err = renderTemplate(&errorBuf, s.tempDir, "error")
	assert.Error(s.T(), err, "renderTemplate() expected execution error for missing template")

	// Test error with non-existent template in renderTemplate
	var nonExistentBuf bytes.Buffer
	err = renderTemplate(&nonExistentBuf, s.tempDir, "does_not_exist")
	assert.Error(s.T(), err, "renderTemplate() expected error for non-existent template")
}

// TestRenderTemplate tests template rendering with environment variables
func (s *MainTestSuite) TestRenderTemplate() {
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
			name:         "template with logical operators (and/or)",
			templateName: "logical_operators",
			envVars: map[string]string{
				"IS_ADMIN":        "true",
				"HAS_PERMISSION":  "true",
				"RESOURCE":        "server logs",
				"SHOW_WARNING":    "",
				"SHOW_ERROR":      "true",
				"MESSAGE":         "System maintenance scheduled",
				"IS_PREMIUM":      "true",
				"IS_TRIAL":        "",
				"FEATURE_ENABLED": "true",
				"FEATURE_NAME":    "Advanced Analytics",
				"USERNAME":        "admin_user",
			},
			expectedOutput: "Admin Access: You have full access to server logs.\nAlert: System maintenance scheduled\nPremium Feature: Advanced Analytics is available.\nUser: admin_user",
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
		s.Run(tt.name, func() {
			// Save original environment
			originalEnv := make(map[string]string)
			for k := range tt.envVars {
				if v, ok := os.LookupEnv(k); ok {
					originalEnv[k] = v
				}
			}
			defer func() {
				for k := range tt.envVars {
					if v, ok := originalEnv[k]; ok {
						_ = os.Setenv(k, v)
					} else {
						_ = os.Unsetenv(k)
					}
				}
			}()

			// Set test environment variables
			for k, v := range tt.envVars {
				_ = os.Setenv(k, v)
			}

			var buf bytes.Buffer
			err := renderTemplate(&buf, "./testdata", tt.templateName)

			if tt.shouldError {
				assert.Error(s.T(), err, "expected error but got none")
			} else {
				require.NoError(s.T(), err, "unexpected error")
			}

			output := normalizeNewlines(buf.String())
			assert.Equal(s.T(), tt.expectedOutput, output, "unexpected output")
		})
	}
}

// normalizeNewlines is a helper function to normalize newlines in strings
func normalizeNewlines(s string) string {
	// Replace multiple consecutive newlines with single newlines
	for strings.Contains(s, "\n\n") {
		s = strings.ReplaceAll(s, "\n\n", "\n")
	}
	return strings.TrimSpace(s)
}
