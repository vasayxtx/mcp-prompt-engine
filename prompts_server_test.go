package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type PromptsServerTestSuite struct {
	suite.Suite
	tempDir string
	logger  *slog.Logger
}

func TestTestSuite(t *testing.T) {
	suite.Run(t, new(PromptsServerTestSuite))
}

func (s *PromptsServerTestSuite) SetupTest() {
	s.tempDir = s.T().TempDir()
	s.logger = slog.New(slog.DiscardHandler)
}

// TestServeStdio tests comprehensive server integration with prompts using ServeStdio
func (s *PromptsServerTestSuite) TestServeStdio() {
	ctx := context.Background()

	tests := []struct {
		name            string
		enableJSONArgs  bool
		promptName      string
		arguments       map[string]string
		expectedContent string // If empty, only basic validation is performed
		description     string
	}{
		// Argument parsing mode tests with specific expected content
		{
			name:            "BasicFunctionality",
			enableJSONArgs:  false,
			promptName:      "greeting",
			arguments:       map[string]string{"name": "John"},
			expectedContent: "Hello John!\nHave a great day!",
			description:     "Test basic functionality without JSON argument parsing",
		},
		{
			name:           "WithJSONArgumentParsing",
			enableJSONArgs: true,
			promptName:     "conditional_greeting",
			arguments: map[string]string{
				"name":               "Alice",
				"show_extra_message": "false", // JSON boolean becomes actual boolean
			},
			expectedContent: "Hello Alice!\nHave a good day.",
			description:     "Test JSON boolean parsing - 'false' becomes boolean false",
		},
		{
			name:           "WithDisabledJSONArgumentParsing",
			enableJSONArgs: false,
			promptName:     "conditional_greeting",
			arguments: map[string]string{
				"name":               "Bob",
				"show_extra_message": "false", // Remains string "false" (truthy!)
			},
			expectedContent: "Hello Bob!\nThis is an extra message just for you.\nHave a good day.",
			description:     "Test disabled JSON parsing - 'false' string is truthy",
		},
		// All testdata prompts with JSON parsing enabled (basic validation only)
		{
			name:           "greeting",
			enableJSONArgs: true,
			promptName:     "greeting",
			arguments:      map[string]string{"name": "TestUser"},
			description:    "Test greeting template",
		},
		{
			name:           "conditional_greeting",
			enableJSONArgs: true,
			promptName:     "conditional_greeting",
			arguments:      map[string]string{"name": "TestUser", "show_extra_message": "true"},
			description:    "Test conditional greeting template",
		},
		{
			name:           "greeting_with_partials",
			enableJSONArgs: true,
			promptName:     "greeting_with_partials",
			arguments:      map[string]string{"name": "TestUser"},
			description:    "Test greeting template with partials",
		},
		{
			name:           "logical_operators",
			enableJSONArgs: true,
			promptName:     "logical_operators",
			arguments:      map[string]string{"enabled": "true", "debug": "false", "count": "5"},
			description:    "Test template with logical operators",
		},
		{
			name:           "multiple_partials",
			enableJSONArgs: true,
			promptName:     "multiple_partials",
			arguments:      map[string]string{"name": "TestUser", "title": "Test Title"},
			description:    "Test template with multiple partials",
		},
		{
			name:           "range_scalars",
			enableJSONArgs: true,
			promptName:     "range_scalars",
			arguments:      map[string]string{"items": `["apple", "banana", "cherry"]`},
			description:    "Test template with range over scalars",
		},
		{
			name:           "range_structs",
			enableJSONArgs: true,
			promptName:     "range_structs",
			arguments:      map[string]string{"users": `[{"name": "Alice", "age": 30}, {"name": "Bob", "age": 25}]`},
			description:    "Test template with range over structs",
		},
		{
			name:           "with_object",
			enableJSONArgs: true,
			promptName:     "with_object",
			arguments:      map[string]string{"user": `{"name": "TestUser", "email": "test@example.com", "active": true}`},
			description:    "Test template with object argument",
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			// Create prompts server that will watch ./testdata directory
			_, mcpClient, promptsClose := s.makePromptsServerAndClient(ctx, "./testdata", tc.enableJSONArgs)
			defer promptsClose()

			// List all available prompts to verify prompt exists
			listResult, err := mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
			require.NoError(s.T(), err, "ListPrompts failed for %s", tc.name)

			// Verify prompt exists in list
			var foundPrompt *mcp.Prompt
			for _, prompt := range listResult.Prompts {
				if prompt.Name == tc.promptName {
					foundPrompt = &prompt
					break
				}
			}
			require.NotNil(s.T(), foundPrompt, "Prompt %s not found in list", tc.promptName)

			// Test GetPrompt with specified arguments
			var getReq mcp.GetPromptRequest
			getReq.Params.Name = tc.promptName
			getReq.Params.Arguments = tc.arguments
			getResult, err := mcpClient.GetPrompt(ctx, getReq)
			require.NoError(s.T(), err, "GetPrompt failed for %s", tc.name)

			// Verify basic response structure
			assert.NotEmpty(s.T(), getResult.Description, "Expected non-empty description for %s", tc.name)
			require.Len(s.T(), getResult.Messages, 1, "Expected exactly 1 message for %s", tc.name)

			content, ok := getResult.Messages[0].Content.(mcp.TextContent)
			require.True(s.T(), ok, "Expected TextContent for %s", tc.name)
			assert.NotEmpty(s.T(), content.Text, "Expected non-empty content for %s", tc.name)

			// If expected content is specified, verify exact match
			if tc.expectedContent != "" {
				actualContent := normalizeNewlines(content.Text)
				assert.Equal(s.T(), tc.expectedContent, actualContent, "Unexpected content for %s: %s", tc.name, tc.description)
			}
		})
	}
}

// TestParseMCPArgs tests parseMCPArgs function functionality
func (s *PromptsServerTestSuite) TestParseMCPArgs() {
	tests := []struct {
		name           string
		input          map[string]string
		enableJSONArgs bool
		expected       map[string]interface{}
	}{
		{
			name:           "empty arguments with JSON enabled",
			input:          map[string]string{},
			enableJSONArgs: true,
			expected:       map[string]interface{}{},
		},
		{
			name: "string arguments remain strings with JSON enabled",
			input: map[string]string{
				"name":    "John",
				"message": "Hello World",
			},
			enableJSONArgs: true,
			expected: map[string]interface{}{
				"name":    "John",
				"message": "Hello World",
			},
		},
		{
			name: "boolean arguments become booleans with JSON enabled",
			input: map[string]string{
				"enabled":  "true",
				"disabled": "false",
			},
			enableJSONArgs: true,
			expected: map[string]interface{}{
				"enabled":  true,
				"disabled": false,
			},
		},
		{
			name: "number arguments become numbers with JSON enabled",
			input: map[string]string{
				"count":   "42",
				"price":   "19.99",
				"balance": "-100.5",
			},
			enableJSONArgs: true,
			expected: map[string]interface{}{
				"count":   float64(42),
				"price":   19.99,
				"balance": -100.5,
			},
		},
		{
			name: "null argument becomes nil with JSON enabled",
			input: map[string]string{
				"optional": "null",
			},
			enableJSONArgs: true,
			expected: map[string]interface{}{
				"optional": nil,
			},
		},
		{
			name: "array arguments become arrays with JSON enabled",
			input: map[string]string{
				"items":   `["apple", "banana", "cherry"]`,
				"numbers": `[1, 2, 3]`,
			},
			enableJSONArgs: true,
			expected: map[string]interface{}{
				"items":   []interface{}{"apple", "banana", "cherry"},
				"numbers": []interface{}{float64(1), float64(2), float64(3)},
			},
		},
		{
			name: "object arguments become objects with JSON enabled",
			input: map[string]string{
				"user": `{"name": "Alice", "age": 30, "active": true}`,
			},
			enableJSONArgs: true,
			expected: map[string]interface{}{
				"user": map[string]interface{}{
					"name":   "Alice",
					"age":    float64(30),
					"active": true,
				},
			},
		},
		{
			name: "invalid JSON remains as strings with JSON enabled",
			input: map[string]string{
				"invalid_json": `{name: "Alice"}`,  // Missing quotes around key
				"incomplete":   `{"name": "Alice"`, // Missing closing brace
			},
			enableJSONArgs: true,
			expected: map[string]interface{}{
				"invalid_json": `{name: "Alice"}`,
				"incomplete":   `{"name": "Alice"`,
			},
		},
		{
			name: "all arguments remain strings when JSON disabled",
			input: map[string]string{
				"name":     "John",
				"enabled":  "true",
				"count":    "42",
				"optional": "null",
				"items":    `["a", "b"]`,
			},
			enableJSONArgs: false,
			expected: map[string]interface{}{
				"name":     "John",
				"enabled":  "true",
				"count":    "42",
				"optional": "null",
				"items":    `["a", "b"]`,
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			data := make(map[string]interface{})
			parseMCPArgs(tt.input, tt.enableJSONArgs, data)
			assert.Equal(s.T(), tt.expected, data, "parseMCPArgs() returned unexpected result")
		})
	}
}

// TestReloadPromptsNewPromptAdded tests reloadPrompts method with new prompts via ServeStdio
func (s *PromptsServerTestSuite) TestReloadPromptsNewPromptAdded() {
	ctx := context.Background()

	// Create initial prompt file so ParseDir doesn't fail
	initialPromptFile := filepath.Join(s.tempDir, "initial_prompt.tmpl")
	initialPromptContent := `{{/* Initial test prompt */}}
Hello {{.name}}! This is the initial prompt.`
	err := os.WriteFile(initialPromptFile, []byte(initialPromptContent), 0644)
	require.NoError(s.T(), err, "Failed to write initial prompt file")

	// Create prompts server that will watch the temp directory
	_, mcpClient, promptsClose := s.makePromptsServerAndClient(ctx, s.tempDir, true)
	defer promptsClose()

	// Verify initial prompt exists
	listResult, err := mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(s.T(), err, "ListPrompts failed")
	require.Len(s.T(), listResult.Prompts, 1, "Expected 1 prompt initially")
	assert.Equal(s.T(), "initial_prompt", listResult.Prompts[0].Name, "Unexpected initial prompt name")

	// Create a new prompt file on filesystem
	newPromptFile := filepath.Join(s.tempDir, "new_prompt.tmpl")
	newPromptContent := `{{/* New test prompt */}}
Hello {{.name}}! This is a new prompt.`
	err = os.WriteFile(newPromptFile, []byte(newPromptContent), 0644)
	require.NoError(s.T(), err, "Failed to write new prompt file")

	// Give the client-server communication time to process the changes
	time.Sleep(100 * time.Millisecond)

	// Client should now see both prompts
	listResult, err = mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(s.T(), err, "ListPrompts failed after adding prompt")
	require.Len(s.T(), listResult.Prompts, 2, "Expected 2 prompts after adding")

	// Find the new prompt in the list
	var newPrompt *mcp.Prompt
	for _, prompt := range listResult.Prompts {
		if prompt.Name == "new_prompt" {
			newPrompt = &prompt
			break
		}
	}
	require.NotNil(s.T(), newPrompt, "New prompt not found in list")
	assert.Equal(s.T(), "New test prompt", newPrompt.Description, "Unexpected prompt description")

	// Verify the client can call the new prompt
	getReq := mcp.GetPromptRequest{}
	getReq.Params.Name = "new_prompt"
	getReq.Params.Arguments = map[string]string{"name": "Alice"}
	getResult, err := mcpClient.GetPrompt(ctx, getReq)
	require.NoError(s.T(), err, "GetPrompt failed for new prompt")

	require.Len(s.T(), getResult.Messages, 1, "Expected exactly 1 message")
	content, ok := getResult.Messages[0].Content.(mcp.TextContent)
	require.True(s.T(), ok, "Expected TextContent")
	assert.Contains(s.T(), content.Text, "Hello Alice! This is a new prompt.", "Unexpected new prompt content")
}

// TestReloadPromptsPromptRemoved tests reloadPrompts method with prompt removal via ServeStdio
func (s *PromptsServerTestSuite) TestReloadPromptsPromptRemoved() {
	ctx := context.Background()

	// Create initial prompt file
	promptFile := filepath.Join(s.tempDir, "test_prompt.tmpl")
	promptContent := `{{/* Test prompt to be removed */}}
Hello {{.name}}!`
	err := os.WriteFile(promptFile, []byte(promptContent), 0644)
	require.NoError(s.T(), err, "Failed to write test prompt file")

	// Create prompts server that will watch the temp directory
	_, mcpClient, promptsClose := s.makePromptsServerAndClient(ctx, s.tempDir, true)
	defer promptsClose()

	// Verify prompt exists initially
	listResult, err := mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(s.T(), err, "ListPrompts failed")
	require.Len(s.T(), listResult.Prompts, 1, "Expected 1 prompt initially")
	assert.Equal(s.T(), "test_prompt", listResult.Prompts[0].Name, "Unexpected prompt name")

	// Verify client can call the prompt
	getReq := mcp.GetPromptRequest{}
	getReq.Params.Name = "test_prompt"
	getReq.Params.Arguments = map[string]string{"name": "Bob"}
	_, err = mcpClient.GetPrompt(ctx, getReq)
	require.NoError(s.T(), err, "GetPrompt should work before removal")

	// Create another prompt file to avoid the empty directory issue
	anotherPromptFile := filepath.Join(s.tempDir, "another_prompt.tmpl")
	anotherPromptContent := `{{/* Another prompt that will remain */}}
Greetings {{.name}}!`
	err = os.WriteFile(anotherPromptFile, []byte(anotherPromptContent), 0644)
	require.NoError(s.T(), err, "Failed to write another prompt file")

	// Remove the original prompt file from filesystem
	err = os.Remove(promptFile)
	require.NoError(s.T(), err, "Failed to remove prompt file")

	// Give the client-server communication time to process the changes
	time.Sleep(100 * time.Millisecond)

	// Client should now see only the remaining prompt
	listResult, err = mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(s.T(), err, "ListPrompts failed after removal")
	require.Len(s.T(), listResult.Prompts, 1, "Expected 1 prompt after removal")
	assert.Equal(s.T(), "another_prompt", listResult.Prompts[0].Name, "Expected only another_prompt to remain")

	// Client should get error when trying to call removed prompt
	_, err = mcpClient.GetPrompt(ctx, getReq)
	assert.Error(s.T(), err, "Expected error when getting removed prompt")

	// But should be able to call the remaining prompt
	getReq.Params.Name = "another_prompt"
	_, err = mcpClient.GetPrompt(ctx, getReq)
	require.NoError(s.T(), err, "Should be able to call remaining prompt")
}

// TestReloadPromptsArgumentAdded tests reloadPrompts method with argument changes via ServeStdio
func (s *PromptsServerTestSuite) TestReloadPromptsArgumentAdded() {
	ctx := context.Background()

	// Create initial prompt with one argument
	promptFile := filepath.Join(s.tempDir, "evolving_prompt.tmpl")
	initialContent := `{{/* Prompt that will gain an argument */}}
Hello {{.name}}!`
	err := os.WriteFile(promptFile, []byte(initialContent), 0644)
	require.NoError(s.T(), err, "Failed to write initial prompt file")

	// Create prompts server that will watch the temp directory
	_, mcpClient, promptsClose := s.makePromptsServerAndClient(ctx, s.tempDir, true)
	defer promptsClose()

	// Verify initial prompt has one argument
	listResult, err := mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(s.T(), err, "ListPrompts failed")
	require.Len(s.T(), listResult.Prompts, 1, "Expected 1 prompt initially")
	require.Len(s.T(), listResult.Prompts[0].Arguments, 1, "Expected 1 argument initially")
	assert.Equal(s.T(), "name", listResult.Prompts[0].Arguments[0].Name, "Expected 'name' argument")

	// Update prompt file to add new argument
	updatedContent := `{{/* Prompt that will gain an argument */}}
Hello {{.name}}! Your age is {{.age}}.`
	err = os.WriteFile(promptFile, []byte(updatedContent), 0644)
	require.NoError(s.T(), err, "Failed to update prompt file")

	// Give the client-server communication time to process the changes
	time.Sleep(100 * time.Millisecond)

	// Client should now see the prompt with two arguments
	listResult, err = mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(s.T(), err, "ListPrompts failed after argument addition")
	require.Len(s.T(), listResult.Prompts, 1, "Expected 1 prompt after update")
	require.Len(s.T(), listResult.Prompts[0].Arguments, 2, "Expected 2 arguments after update")

	// Verify both arguments are present
	argNames := make([]string, len(listResult.Prompts[0].Arguments))
	for i, arg := range listResult.Prompts[0].Arguments {
		argNames[i] = arg.Name
	}
	assert.Contains(s.T(), argNames, "name", "Expected 'name' argument")
	assert.Contains(s.T(), argNames, "age", "Expected 'age' argument")

	// Verify client can call the updated prompt with both arguments
	getReq := mcp.GetPromptRequest{}
	getReq.Params.Name = "evolving_prompt"
	getReq.Params.Arguments = map[string]string{"name": "Alice", "age": "25"}
	getResult, err := mcpClient.GetPrompt(ctx, getReq)
	require.NoError(s.T(), err, "GetPrompt failed for updated prompt")

	require.Len(s.T(), getResult.Messages, 1, "Expected exactly 1 message")
	content, ok := getResult.Messages[0].Content.(mcp.TextContent)
	require.True(s.T(), ok, "Expected TextContent")
	assert.Contains(s.T(), content.Text, "Hello Alice! Your age is 25.", "Unexpected updated prompt content")
}

// TestReloadPromptsArgumentRemoved tests reloadPrompts method with argument removal via ServeStdio
func (s *PromptsServerTestSuite) TestReloadPromptsArgumentRemoved() {
	ctx := context.Background()

	// Create initial prompt with two arguments
	promptFile := filepath.Join(s.tempDir, "shrinking_prompt.tmpl")
	initialContent := `{{/* Prompt that will lose an argument */}}
Hello {{.name}}! Your age is {{.age}}.`
	err := os.WriteFile(promptFile, []byte(initialContent), 0644)
	require.NoError(s.T(), err, "Failed to write initial prompt file")

	// Create prompts server that will watch the temp directory
	_, mcpClient, promptsClose := s.makePromptsServerAndClient(ctx, s.tempDir, true)
	defer promptsClose()

	// Verify initial prompt has two arguments
	listResult, err := mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(s.T(), err, "ListPrompts failed")
	require.Len(s.T(), listResult.Prompts, 1, "Expected 1 prompt initially")
	require.Len(s.T(), listResult.Prompts[0].Arguments, 2, "Expected 2 arguments initially")

	// Update prompt file to remove age argument
	updatedContent := `{{/* Prompt that will lose an argument */}}
Hello {{.name}}!`
	err = os.WriteFile(promptFile, []byte(updatedContent), 0644)
	require.NoError(s.T(), err, "Failed to update prompt file")

	// Give the client-server communication time to process the changes
	time.Sleep(100 * time.Millisecond)

	// Client should now see the prompt with only one argument
	listResult, err = mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(s.T(), err, "ListPrompts failed after argument removal")
	require.Len(s.T(), listResult.Prompts, 1, "Expected 1 prompt after update")
	require.Len(s.T(), listResult.Prompts[0].Arguments, 1, "Expected 1 argument after update")
	assert.Equal(s.T(), "name", listResult.Prompts[0].Arguments[0].Name, "Expected only 'name' argument to remain")

	// Verify client can call the updated prompt with only the remaining argument
	getReq := mcp.GetPromptRequest{}
	getReq.Params.Name = "shrinking_prompt"
	getReq.Params.Arguments = map[string]string{"name": "Bob"}
	getResult, err := mcpClient.GetPrompt(ctx, getReq)
	require.NoError(s.T(), err, "GetPrompt failed for updated prompt")

	require.Len(s.T(), getResult.Messages, 1, "Expected exactly 1 message")
	content, ok := getResult.Messages[0].Content.(mcp.TextContent)
	require.True(s.T(), ok, "Expected TextContent")
	assert.Contains(s.T(), content.Text, "Hello Bob!", "Unexpected updated prompt content")
	assert.NotContains(s.T(), content.Text, "age", "Should not contain age reference after removal")
}

// TestReloadPromptsDescriptionChanged tests reloadPrompts method with description changes via ServeStdio
func (s *PromptsServerTestSuite) TestReloadPromptsDescriptionChanged() {
	ctx := context.Background()

	// Create initial prompt with original description
	promptFile := filepath.Join(s.tempDir, "descriptive_prompt.tmpl")
	initialContent := `{{/* Original description */}}
Hello {{.name}}!`
	err := os.WriteFile(promptFile, []byte(initialContent), 0644)
	require.NoError(s.T(), err, "Failed to write initial prompt file")

	// Create prompts server that will watch the temp directory
	_, mcpClient, promptsClose := s.makePromptsServerAndClient(ctx, s.tempDir, true)
	defer promptsClose()

	// Verify initial description
	listResult, err := mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(s.T(), err, "ListPrompts failed")
	require.Len(s.T(), listResult.Prompts, 1, "Expected 1 prompt initially")
	assert.Equal(s.T(), "Original description", listResult.Prompts[0].Description, "Expected original description")

	// Update prompt file with new description
	updatedContent := `{{/* Updated description with more details */}}
Hello {{.name}}!`
	err = os.WriteFile(promptFile, []byte(updatedContent), 0644)
	require.NoError(s.T(), err, "Failed to update prompt file")

	// Give the client-server communication time to process the changes
	time.Sleep(100 * time.Millisecond)

	// Client should now see the updated description
	listResult, err = mcpClient.ListPrompts(ctx, mcp.ListPromptsRequest{})
	require.NoError(s.T(), err, "ListPrompts failed after description change")
	require.Len(s.T(), listResult.Prompts, 1, "Expected 1 prompt after update")
	assert.Equal(s.T(), "Updated description with more details", listResult.Prompts[0].Description, "Expected updated description")

	// Verify client can still call the prompt and gets updated description
	getReq := mcp.GetPromptRequest{}
	getReq.Params.Name = "descriptive_prompt"
	getReq.Params.Arguments = map[string]string{"name": "Charlie"}
	getResult, err := mcpClient.GetPrompt(ctx, getReq)
	require.NoError(s.T(), err, "GetPrompt failed for updated prompt")

	require.Len(s.T(), getResult.Messages, 1, "Expected exactly 1 message")
	content, ok := getResult.Messages[0].Content.(mcp.TextContent)
	require.True(s.T(), ok, "Expected TextContent")
	assert.Contains(s.T(), content.Text, "Hello Charlie!", "Prompt functionality should remain the same")
	assert.Equal(s.T(), "Updated description with more details", getResult.Description, "GetPrompt should return updated description")
}

func (s *PromptsServerTestSuite) makePromptsServerAndClient(
	ctx context.Context, promptsDir string, enableJSONArgs bool,
) (*PromptsServer, *client.Client, func()) {
	var ctxCancel context.CancelFunc
	ctx, ctxCancel = context.WithCancel(ctx)

	// Create prompts server that will watch the temp directory
	promptsServer, err := NewPromptsServer(promptsDir, enableJSONArgs, s.logger)
	require.NoError(s.T(), err, "Failed to create prompts server")

	// Set up pipes for client-server communication
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	// Start the server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- promptsServer.ServeStdio(ctx, serverReader, serverWriter)
	}()

	// Create transport and client
	var logBuffer bytes.Buffer
	transp := transport.NewIO(clientReader, clientWriter, io.NopCloser(&logBuffer))
	err = transp.Start(ctx)
	require.NoError(s.T(), err, "Failed to start transport")

	mcpClient := client.NewClient(transp)

	// Initialize the client
	var initReq mcp.InitializeRequest
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	_, err = mcpClient.Initialize(ctx, initReq)
	require.NoError(s.T(), err, "Failed to initialize client")

	return promptsServer, mcpClient, func() {
		ctxCancel()
		s.Require().NoError(<-errChan)
		s.Require().NoError(transp.Close())
		s.Require().NoError(promptsServer.Close())
	}
}
