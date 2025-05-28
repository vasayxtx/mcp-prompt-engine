package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "Show version and exit")
	promptsDir := flag.String("prompts", "./prompts", "Directory containing prompt template files")
	logFile := flag.String("log-file", "", "Path to log file (if not specified, logs to stdout)")
	flag.Parse()

	if *showVersion {
		fmt.Println("App version: ", version)
		fmt.Println("Go version: ", runtime.Version())
		return
	}

	if err := runServer(*promptsDir, *logFile); err != nil {
		log.Fatal(err)
	}
}

func runServer(promptsDir string, logFile string) error {
	logWriter := os.Stdout
	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		logWriter = file
	}
	logger := slog.New(slog.NewTextHandler(logWriter, nil))

	srvHooks := &server.Hooks{}
	srvHooks.AddBeforeGetPrompt(func(ctx context.Context, id any, message *mcp.GetPromptRequest) {
		logger.Info("Received prompt request",
			"id", id, "params_name", message.Params.Name, "params_args", message.Params.Arguments)
	})
	srvHooks.AddAfterGetPrompt(func(ctx context.Context, id any, message *mcp.GetPromptRequest, result *mcp.GetPromptResult) {
		logger.Info("Processed prompt request",
			"id", id, "params_name", message.Params.Name, "params_args", message.Params.Arguments)

	})

	srv := server.NewMCPServer(
		"Custom Prompts Server",
		"1.0.0",
		server.WithLogging(),
		server.WithRecovery(),
		server.WithHooks(srvHooks),
	)

	if err := buildPrompts(srv, promptsDir, logger); err != nil {
		return fmt.Errorf("build prompts: %w", err)
	}

	logger.Info("Starting stdio server")
	if err := server.ServeStdio(srv); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Error starting stdio server", "error", err)
		return fmt.Errorf("serve stdio: %w", err)
	}

	return nil
}

// dict creates a map from key-value pairs for template usage
func dict(values ...interface{}) map[string]interface{} {
	if len(values)%2 != 0 {
		return nil
	}
	result := make(map[string]interface{})
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return nil
		}
		result[key] = values[i+1]
	}
	return result
}

// buildPrompts builds and registers prompts with the server
func buildPrompts(s *server.MCPServer, promptsDir string, logger *slog.Logger) error {
	// Load all partials first
	partials, err := loadPartials(promptsDir)
	if err != nil {
		return fmt.Errorf("load partials: %w", err)
	}

	files, err := os.ReadDir(promptsDir)
	if err != nil {
		return fmt.Errorf("error reading prompts directory: %v", err)
	}

	for _, file := range files {
		if file.Type().IsRegular() && strings.HasSuffix(file.Name(), ".tmpl") && !strings.HasPrefix(file.Name(), "_") {
			filePath := filepath.Join(promptsDir, file.Name())

			// Parse template and extract description
			_, description, err := parseTemplateFile(filePath, partials)
			if err != nil {
				logger.Error("Error parsing template file", "file", filePath, "error", err)
				continue
			}

			promptName := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))

			// Extract template arguments by analyzing template source
			args, err := extractTemplateArguments(filePath, partials)
			if err != nil {
				logger.Error("Error extracting template arguments", "file", filePath, "error", err)
				continue
			}

			envArgs := make(map[string]string)
			var promptArgs []string
			for _, arg := range args {
				// Convert arg to TITLE_CASE for env var
				envVarName := strings.ToUpper(arg)
				if envValue, exists := os.LookupEnv(envVarName); exists {
					envArgs[arg] = envValue
				} else {
					promptArgs = append(promptArgs, arg)
				}
			}

			promptOpts := []mcp.PromptOption{
				mcp.WithPromptDescription(description),
			}
			for _, promptArg := range promptArgs {
				promptOpts = append(promptOpts, mcp.WithArgument(promptArg, mcp.RequiredArgument()))
			}

			prompt := mcp.NewPrompt(promptName, promptOpts...)

			promptHandler := func(
				filePath string, description string, envArgs map[string]string, partials map[string]string,
			) func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				return func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
					// Re-parse template to get latest version
					tmpl, _, readErr := parseTemplateFile(filePath, partials)
					if readErr != nil {
						return nil, fmt.Errorf("parse template file %s: %w", filePath, readErr)
					}

					// Prepare template data - make a flat map for easy access
					data := make(map[string]interface{})
					data["date"] = time.Now().Format("2006-01-02 15:04:05")

					// Add environment variables
					for arg, value := range envArgs {
						data[arg] = value
					}
					// Add request arguments
					for arg, value := range request.Params.Arguments {
						data[arg] = value
					}

					// Execute template
					var result strings.Builder
					if err := tmpl.Execute(&result, data); err != nil {
						return nil, fmt.Errorf("execute template: %w", err)
					}

					return mcp.NewGetPromptResult(
						description,
						[]mcp.PromptMessage{
							mcp.NewPromptMessage(
								mcp.RoleUser,
								mcp.NewTextContent(result.String()),
							),
						},
					), nil
				}
			}

			s.AddPrompt(prompt, promptHandler(filePath, description, envArgs, partials))

			logger.Info("Prompt registered",
				"name", promptName,
				"description", description,
				"prompt_args", promptArgs,
				"env_args", envArgs)
		}
	}

	return nil
}

// loadPartials loads all partial templates (files starting with _)
func loadPartials(promptsDir string) (map[string]string, error) {
	partials := make(map[string]string)

	files, err := os.ReadDir(promptsDir)
	if err != nil {
		return nil, fmt.Errorf("read prompts directory: %w", err)
	}

	for _, file := range files {
		if file.Type().IsRegular() && strings.HasSuffix(file.Name(), ".tmpl") && strings.HasPrefix(file.Name(), "_") {
			filePath := filepath.Join(promptsDir, file.Name())
			content, err := os.ReadFile(filePath)
			if err != nil {
				return nil, fmt.Errorf("read partial file %s: %w", filePath, err)
			}

			// Remove the _ prefix and .tmpl suffix to get the partial name
			partialName := strings.TrimSuffix(strings.TrimPrefix(file.Name(), "_"), ".tmpl")
			partials[partialName] = string(content)
		}
	}

	return partials, nil
}

// parseTemplateFile reads and parses a template file, extracting description from comment
func parseTemplateFile(filePath string, partials map[string]string) (*template.Template, string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, "", fmt.Errorf("read file: %w", err)
	}

	contentStr := string(content)

	// Extract description from first line comment
	var description string
	lines := strings.SplitN(contentStr, "\n", 2)
	// TODO:
	// 1) Support {{- /* comment */ -}} style comments
	// 2) Support whitespace trimming around comments ({{- /* comment */ -}})
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "{{/*") && strings.HasSuffix(strings.TrimSpace(lines[0]), "*/}}") {
		// Extract description from comment
		comment := strings.TrimSpace(lines[0])
		comment = strings.TrimPrefix(comment, "{{/*")
		comment = strings.TrimSuffix(comment, "*/}}")
		description = strings.TrimSpace(comment)
	}

	// Create template with custom functions
	tmpl := template.New(filepath.Base(filePath)).Funcs(template.FuncMap{
		"dict": dict,
		"add":  func(a, b int) int { return a + b },
		"sub":  func(a, b int) int { return a - b },
		"mul":  func(a, b int) int { return a * b },
		"div": func(a, b int) int {
			if b != 0 {
				return a / b
			}
			return 0
		},
	})

	// Add partials to template
	for prtName, prtContent := range partials {
		if _, err = tmpl.New(prtName).Parse(prtContent); err != nil {
			return nil, "", fmt.Errorf("parse partial %s: %w", prtName, err)
		}
	}

	// Parse main template
	tmpl, err = tmpl.Parse(contentStr)
	if err != nil {
		return nil, "", fmt.Errorf("parse template: %w", err)
	}

	return tmpl, description, nil
}

var templateArgsRegex = regexp.MustCompile(`{{\s*\.\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*}}`)

// extractTemplateArguments analyzes template source to find field references,
// including only partials that are actually used by the template
func extractTemplateArguments(filePath string, partials map[string]string) ([]string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	mainContent := string(content)

	// Find which partials are actually used by this template
	usedPartials := findUsedPartials(mainContent, partials)

	// Collect content from main template + only used partials
	// TODO:
	// 1) handle cyclic references gracefully (return error if cycle detected)
	// 2) handle nested partials recursively on unlimited depth
	allContent := mainContent
	for _, partialContent := range usedPartials {
		allContent += "\n" + partialContent
		// Also check if used partials reference other partials (recursive)
		nestedPartials := findUsedPartials(partialContent, partials)
		for nestedName, nestedContent := range nestedPartials {
			if _, alreadyIncluded := usedPartials[nestedName]; !alreadyIncluded {
				allContent += "\n" + nestedContent
				usedPartials[nestedName] = nestedContent
			}
		}
	}

	// Extract field references using regex
	// Match patterns like {{.fieldname}} and {{ .fieldname }}
	matches := templateArgsRegex.FindAllStringSubmatch(allContent, -1)

	// Use a map to deduplicate arguments and filter out built-in fields
	argsMap := make(map[string]struct{})
	builtInFields := map[string]struct{}{
		"date": {},
	}

	for _, match := range matches {
		if len(match) > 1 {
			fieldName := strings.ToLower(match[1]) // Normalize to lowercase
			// Skip built-in fields
			if _, isBuiltIn := builtInFields[fieldName]; !isBuiltIn {
				argsMap[fieldName] = struct{}{}
			}
		}
	}

	// Convert map keys to slice
	args := make([]string, 0, len(argsMap))
	for arg := range argsMap {
		args = append(args, arg)
	}

	return args, nil
}

// findUsedPartials analyzes template content to find which partials are referenced
func findUsedPartials(content string, allPartials map[string]string) map[string]string {
	usedPartials := make(map[string]string)

	// Match template calls like {{template "partial_name" ...}} or {{template "_partial" ...}}
	// This regex captures both quoted partial names and bare partial names
	templateCallRegex := regexp.MustCompile(`{{\s*template\s+["']?([a-zA-Z_][a-zA-Z0-9_]*)["']?\s+[^}]*}}`)
	matches := templateCallRegex.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) > 1 {
			partialName := match[1]
			// Remove leading underscore if present (since we store partials without the underscore)
			if strings.HasPrefix(partialName, "_") {
				partialName = strings.TrimPrefix(partialName, "_")
			}

			// Check if this partial exists in our partials map
			if partialContent, exists := allPartials[partialName]; exists {
				usedPartials[partialName] = partialContent
			}
		}
	}

	return usedPartials
}
