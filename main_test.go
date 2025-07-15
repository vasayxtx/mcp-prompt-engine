package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
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

// removeANSIColors removes ANSI color escape sequences from a string
func removeANSIColors(s string) string {
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiRegex.ReplaceAllString(s, "")
}

// captureStdout captures stdout during function execution
func (s *MainTestSuite) captureStdout(f func() error) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := f()
	_ = err // We handle the error in the calling function

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

// TestListTemplates tests the listTemplates function
func (s *MainTestSuite) TestListTemplates() {
	tests := []struct {
		name          string
		detailed      bool
		expectedLines []string
		shouldError   bool
	}{
		{
			name:     "list templates basic mode",
			detailed: false,
			expectedLines: []string{
				templateText("conditional_greeting.tmpl"),
				templateText("greeting.tmpl"),
				templateText("greeting_with_partials.tmpl"),
				templateText("logical_operators.tmpl"),
				templateText("multiple_partials.tmpl"),
				templateText("range_scalars.tmpl"),
				templateText("range_structs.tmpl"),
				templateText("with_object.tmpl"),
			},
			shouldError: false,
		},
		{
			name:     "list templates verbose mode",
			detailed: true,
			expectedLines: []string{
				templateText("conditional_greeting.tmpl"),
				"  Description: Conditional greeting template",
				"  Variables: ", // Just check that Variables line exists, content may vary
				templateText("greeting.tmpl"),
				"  Description: Greeting standalone template with no partials",
				"  Variables: ",
				templateText("greeting_with_partials.tmpl"),
				"  Description: Greeting template with partial",
				"  Variables: ",
				templateText("logical_operators.tmpl"),
				"  Description: Template with logical operators (and/or) in if blocks",
				"  Variables: ",
				templateText("multiple_partials.tmpl"),
				"  Description: Template with multiple partials",
				"  Variables: ",
				templateText("range_scalars.tmpl"),
				"  Description: Template for testing range with JSON array of scalars",
				"  Variables: ",
				templateText("range_structs.tmpl"),
				"  Description: Template for testing range with JSON array of structs",
				"  Variables: ",
				templateText("with_object.tmpl"),
				"  Description: Template for testing with + JSON object",
				"  Variables: ",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			var buf bytes.Buffer
			err := listTemplates(&buf, "./testdata", tt.detailed)

			if tt.shouldError {
				assert.Error(s.T(), err, "expected error but got none")
			} else {
				require.NoError(s.T(), err, "unexpected error")
			}

			output := buf.String()
			lines := strings.Split(strings.TrimSpace(output), "\n")

			// For basic mode, check exact match
			if !tt.detailed {
				assert.Equal(s.T(), len(tt.expectedLines), len(lines), "number of lines should match")
				for i, expectedLine := range tt.expectedLines {
					if i < len(lines) {
						assert.Equal(s.T(), expectedLine, lines[i], "line %d should match", i)
					}
				}
				return
			}

			// For detailed mode, check structure but be flexible about variable content
			lineIndex := 0
			for _, expectedLine := range tt.expectedLines {
				if lineIndex >= len(lines) {
					s.T().Fatalf("Not enough lines in output. Expected at least %d, got %d", len(tt.expectedLines), len(lines))
				}

				if strings.HasPrefix(expectedLine, "  Variables: ") {
					// Just check that the line starts with "  Variables: " and contains some content
					assert.True(s.T(), strings.HasPrefix(lines[lineIndex], "  Variables: "),
						"line %d should start with '  Variables: ', got: %s", lineIndex, lines[lineIndex])
				} else {
					assert.Equal(s.T(), expectedLine, lines[lineIndex], "line %d should match", lineIndex)
				}
				lineIndex++
			}
		})
	}
}

// TestListTemplatesErrorCases tests error cases for listTemplates
func (s *MainTestSuite) TestListTemplatesErrorCases() {
	var buf bytes.Buffer

	// Test non-existent directory
	err := listTemplates(&buf, "/non/existent/directory", false)
	assert.Error(s.T(), err, "listTemplates() expected error for non-existent directory")

	// Test empty directory
	emptyDir := s.T().TempDir()
	var emptyBuf bytes.Buffer
	err = listTemplates(&emptyBuf, emptyDir, true)
	require.NoError(s.T(), err, "listTemplates() should not error for empty directory")
	output := emptyBuf.String()
	assert.Contains(s.T(), output, "No templates found", "should indicate no templates found")
	emptyBuf.Reset()
	err = listTemplates(&emptyBuf, emptyDir, false)
	require.NoError(s.T(), err, "listTemplates() should not error for empty directory")
	require.Empty(s.T(), emptyBuf.String())
}

// TestListTemplatesWithPartials tests that partials are excluded from listing
func (s *MainTestSuite) TestListTemplatesWithPartials() {
	// Create a temp directory with templates and partials
	tempDir := s.T().TempDir()

	// Create regular template
	err := os.WriteFile(tempDir+"/regular.tmpl", []byte("{{/* Regular template */}}\nHello!"), 0644)
	require.NoError(s.T(), err)

	// Create partial template (should be excluded)
	err = os.WriteFile(tempDir+"/_partial.tmpl", []byte("{{/* Partial template */}}\nThis is a partial"), 0644)
	require.NoError(s.T(), err)

	var buf bytes.Buffer
	err = listTemplates(&buf, tempDir, false)
	require.NoError(s.T(), err)

	output := buf.String()
	assert.Contains(s.T(), output, "regular.tmpl", "should include regular template")
	assert.NotContains(s.T(), output, "_partial.tmpl", "should exclude partial template")
}

// TestValidateTemplates tests the validateTemplates function
func (s *MainTestSuite) TestValidateTemplates() {
	tests := []struct {
		name           string
		templateName   string
		templates      map[string]string
		expectedOutput []string
		shouldError    bool
	}{
		{
			name:         "validate all valid templates",
			templateName: "",
			templates: map[string]string{
				"valid1.tmpl": "{{/* Valid template 1 */}}\nHello {{.name}}!",
				"valid2.tmpl": "{{/* Valid template 2 */}}\nWelcome {{.user}}!",
			},
			expectedOutput: []string{
				"✓ valid1.tmpl - Valid",
				"✓ valid2.tmpl - Valid",
			},
			shouldError: false,
		},
		{
			name:         "validate specific valid template",
			templateName: "valid1.tmpl",
			templates: map[string]string{
				"valid1.tmpl": "{{/* Valid template 1 */}}\nHello {{.name}}!",
				"valid2.tmpl": "{{/* Valid template 2 */}}\nWelcome {{.user}}!",
			},
			expectedOutput: []string{
				"✓ valid1.tmpl - Valid",
			},
			shouldError: false,
		},
		{
			name:         "validate specific valid template without extension",
			templateName: "valid1",
			templates: map[string]string{
				"valid1.tmpl": "{{/* Valid template 1 */}}\nHello {{.name}}!",
				"valid2.tmpl": "{{/* Valid template 2 */}}\nWelcome {{.user}}!",
			},
			expectedOutput: []string{
				"✓ valid1.tmpl - Valid",
			},
			shouldError: false,
		},
		{
			name:         "validate template with missing reference",
			templateName: "",
			templates: map[string]string{
				"valid.tmpl":          "{{/* Valid template */}}\nHello {{.name}}!",
				"missing_ref.tmpl":    "{{/* Template with missing reference */}}\n{{template \"nonexistent\" .}}",
			},
			expectedOutput: []string{
				"✗ missing_ref.tmpl - Error:",
				"✓ valid.tmpl - Valid",
			},
			shouldError: true,
		},
		{
			name:         "validate specific template with missing reference",
			templateName: "missing_ref.tmpl",
			templates: map[string]string{
				"valid.tmpl":          "{{/* Valid template */}}\nHello {{.name}}!",
				"missing_ref.tmpl":    "{{/* Template with missing reference */}}\n{{template \"nonexistent\" .}}",
			},
			expectedOutput: []string{
				"✗ missing_ref.tmpl - Error:",
			},
			shouldError: true,
		},
		{
			name:         "validate template with partials",
			templateName: "",
			templates: map[string]string{
				"main.tmpl":     "{{/* Main template */}}\n{{template \"_partial\" .}}",
				"_partial.tmpl": "{{/* Partial template */}}\nHello {{.name}}!",
			},
			expectedOutput: []string{
				"✓ main.tmpl - Valid",
			},
			shouldError: false,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Create temp directory and test templates
			tempDir := s.T().TempDir()
			for filename, content := range tt.templates {
				err := os.WriteFile(filepath.Join(tempDir, filename), []byte(content), 0644)
				require.NoError(s.T(), err)
			}

			// Capture stdout since validateTemplates prints directly to stdout
			output := s.captureStdout(func() error {
				var buf bytes.Buffer
				return validateTemplates(&buf, tempDir, tt.templateName)
			})

			var err error
			if tt.shouldError {
				// Run again to get the error
				var buf bytes.Buffer
				err = validateTemplates(&buf, tempDir, tt.templateName)
				assert.Error(s.T(), err, "expected error but got none")
			} else {
				var buf bytes.Buffer
				err = validateTemplates(&buf, tempDir, tt.templateName)
				require.NoError(s.T(), err, "unexpected error")
			}

			lines := strings.Split(strings.TrimSpace(output), "\n")

			// Check that all expected output lines are present
			for _, expectedLine := range tt.expectedOutput {
				found := false
				for _, line := range lines {
					// Remove ANSI color codes for comparison
					cleanLine := removeANSIColors(line)
					cleanExpected := removeANSIColors(expectedLine)
					if strings.Contains(cleanLine, cleanExpected) || 
					   (strings.Contains(cleanExpected, "Error:") && strings.Contains(cleanLine, "Error:")) {
						found = true
						break
					}
				}
				assert.True(s.T(), found, "expected line '%s' not found in output: %s", expectedLine, output)
			}
		})
	}
}

// TestValidateTemplatesErrorCases tests error cases for validateTemplates
func (s *MainTestSuite) TestValidateTemplatesErrorCases() {
	tests := []struct {
		name         string
		promptsDir   string
		templateName string
		setupFunc    func(string) error
		expectedError string
	}{
		{
			name:          "non-existent directory",
			promptsDir:    "/non/existent/directory",
			templateName:  "",
			expectedError: "read prompts directory",
		},
		{
			name:         "non-existent specific template",
			promptsDir:   "",
			templateName: "does_not_exist.tmpl",
			setupFunc: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "exists.tmpl"), []byte("{{/* Exists */}}\nHello!"), 0644)
			},
			expectedError: "not found",
		},
		{
			name:         "non-existent specific template without extension",
			promptsDir:   "",
			templateName: "does_not_exist",
			setupFunc: func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "exists.tmpl"), []byte("{{/* Exists */}}\nHello!"), 0644)
			},
			expectedError: "not found",
		},
		{
			name:       "empty directory",
			promptsDir: "",
			setupFunc:  func(dir string) error { return nil },
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			var tempDir string
			if tt.promptsDir == "" {
				tempDir = s.T().TempDir()
				if tt.setupFunc != nil {
					err := tt.setupFunc(tempDir)
					require.NoError(s.T(), err)
				}
			} else {
				tempDir = tt.promptsDir
			}

			var buf bytes.Buffer
			err := validateTemplates(&buf, tempDir, tt.templateName)

			if tt.expectedError != "" {
				assert.Error(s.T(), err)
				assert.Contains(s.T(), err.Error(), tt.expectedError)
			} else {
				// For empty directory case, should not error but output warning
				require.NoError(s.T(), err)
				output := buf.String()
				assert.Contains(s.T(), output, "No templates found")
			}
		})
	}
}

// TestValidateTemplatesOutput tests the output formatting of validateTemplates
func (s *MainTestSuite) TestValidateTemplatesOutput() {
	// Test with syntax error that occurs during parsing
	tempDir := s.T().TempDir()
	
	// Invalid template with syntax error
	err := os.WriteFile(filepath.Join(tempDir, "invalid.tmpl"), 
		[]byte("{{/* Invalid template */}}\nHello {{.name}"), 0644)
	require.NoError(s.T(), err)

	var buf bytes.Buffer
	err = validateTemplates(&buf, tempDir, "")
	assert.Error(s.T(), err)
	assert.Contains(s.T(), err.Error(), "parse prompts directory")

	// Test with valid templates to verify successful output formatting
	tempDir2 := s.T().TempDir()
	
	// Valid template
	err = os.WriteFile(filepath.Join(tempDir2, "valid.tmpl"), 
		[]byte("{{/* Valid template */}}\nHello {{.name}}!"), 0644)
	require.NoError(s.T(), err)

	// Capture stdout output
	output := s.captureStdout(func() error {
		var buf bytes.Buffer
		return validateTemplates(&buf, tempDir2, "")
	})

	cleanOutput := removeANSIColors(output)
	
	// Check that output contains the template
	assert.Contains(s.T(), cleanOutput, "valid.tmpl")
	
	// Check formatting - should contain success icon
	assert.Contains(s.T(), cleanOutput, "✓") // Success icon
	
	// Check status message
	assert.Contains(s.T(), cleanOutput, "Valid")
}
