package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
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

type promptServer interface {
	AddPrompt(prompt mcp.Prompt, handler server.PromptHandlerFunc)
}

// buildPrompts builds and registers prompts with the server
func buildPrompts(srv promptServer, promptsDir string, logger *slog.Logger) error {
	_, err := parseAllPrompts(promptsDir)
	if err != nil {
		return fmt.Errorf("parse all prompts: %w", err)
	}

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
			var description string
			if description, err = extractPromptDescription(filePath); err != nil {
				logger.Error("Error parsing template file", "file", filePath, "error", err)
				continue
			}

			promptName := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))

			// Extract template arguments by analyzing template source
			var args []string
			if args, err = extractPromptArguments(filePath, partials); err != nil {
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

			srv.AddPrompt(mcp.NewPrompt(promptName, promptOpts...),
				promptHandler(promptsDir, strings.TrimSuffix(file.Name(), ".tmpl"), description, envArgs))

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

			// Remove the .tmpl suffix to get the partial name
			partialName := strings.TrimSuffix(file.Name(), ".tmpl")
			//partialName = strings.TrimPrefix(partialName, "_")
			partials[partialName] = string(content)
		}
	}

	return partials, nil
}

func extractPromptDescription(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	var firstLine string
	if idx := bytes.IndexByte(content, '\n'); idx != -1 {
		firstLine = string(content[:idx])
	} else {
		firstLine = string(content)
	}
	firstLine = strings.TrimSpace(firstLine)

	for _, c := range [...][2]string{
		{"{{/*", "*/}}"},
		{"{{- /*", "*/}}"},
		{"{{/*", "*/ -}}"},
		{"{{- /*", "*/ -}}"},
	} {
		if strings.HasPrefix(firstLine, c[0]) && strings.HasSuffix(firstLine, c[1]) {
			comment := firstLine
			comment = strings.TrimPrefix(comment, c[0])
			comment = strings.TrimSuffix(comment, c[1])
			return strings.TrimSpace(comment), nil
		}
	}

	return "", nil
}

var templateArgsRegex = regexp.MustCompile(`{{\s*\.\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*}}`)

// extractPromptArguments analyzes template source to find field references,
// including only partials that are actually used by the template
func extractPromptArguments(filePath string, partials map[string]string) ([]string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	mainContent := string(content)

	// Find which partials are actually used by this template
	usedPartials := make(map[string]string)
	processedPartials := make(map[string]bool)

	// Helper function to recursively process partials with cycle detection
	var processPartial func(content string, path []string) error
	processPartial = func(content string, path []string) error {
		// Find direct partials used by this content
		directPartials := findUsedPartials(content, partials)

		// Process each partial
		for partialName, partialContent := range directPartials {
			// Check for cycles
			for _, ancestor := range path {
				if ancestor == partialName {
					cyclePath := append(path, partialName)
					return fmt.Errorf("cyclic partial reference detected: %s", strings.Join(cyclePath, " -> "))
				}
			}

			// Add to used partials if not already processed
			if !processedPartials[partialName] {
				usedPartials[partialName] = partialContent
				processedPartials[partialName] = true

				// Recursively process this partial's dependencies
				newPath := append(append([]string{}, path...), partialName)
				if err = processPartial(partialContent, newPath); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// Start processing from the main content
	if err = processPartial(mainContent, []string{}); err != nil {
		return nil, err
	}

	// Also need to analyze template calls with dict arguments like {{template "_header" dict "role" .role}}
	// to extract arguments passed to partials
	// This regex matches patterns like dict "key" .value or dict "key" .value "key2" .value2
	dictArgRegex := regexp.MustCompile(`dict\s+"([^"]+)"\s+\.([a-zA-Z_][a-zA-Z0-9_]*)(?:\s+"[^"]+"\s+\.([a-zA-Z_][a-zA-Z0-9_]*))*`)

	// Collect content from main template + all used partials
	allContent := mainContent
	for _, partialContent := range usedPartials {
		allContent += "\n" + partialContent
	}

	// Extract field references using regex
	// Match patterns like {{.fieldname}} and {{ .fieldname }}
	matches := templateArgsRegex.FindAllStringSubmatch(allContent, -1)

	// Also extract dict arguments from template calls
	dictMatches := dictArgRegex.FindAllStringSubmatch(allContent, -1)

	// Use a map to deduplicate arguments and filter out built-in fields
	argsMap := make(map[string]struct{})
	builtInFields := map[string]struct{}{
		"date": {},
	}

	// Process regular template arguments
	for _, match := range matches {
		if len(match) > 1 {
			fieldName := strings.ToLower(match[1]) // Normalize to lowercase
			// Skip built-in fields
			if _, isBuiltIn := builtInFields[fieldName]; !isBuiltIn {
				argsMap[fieldName] = struct{}{}
			}
		}
	}

	// Process dict arguments in template calls
	for _, match := range dictMatches {
		for i := 2; i < len(match); i++ {
			if match[i] != "" {
				fieldName := strings.ToLower(match[i]) // Normalize to lowercase
				// Skip built-in fields
				if _, isBuiltIn := builtInFields[fieldName]; !isBuiltIn {
					argsMap[fieldName] = struct{}{}
				}
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
	// This regex captures partial names with or without underscore prefix
	templateCallRegex := regexp.MustCompile(`{{\s*template\s+["']([a-zA-Z_][a-zA-Z0-9_]*)["']\s+[^}]*}}`)
	matches := templateCallRegex.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) > 1 {
			partialName := match[1] // Get the name without the underscore prefix
			// Check if this partial exists in our partials map
			if partialContent, exists := allPartials[partialName]; exists {
				usedPartials[partialName] = partialContent
			}
		}
	}

	return usedPartials
}

func promptHandler(
	promptsDir string, templateName string, description string, envArgs map[string]string,
) func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		tmpl, err := parseAllPrompts(promptsDir)
		if err != nil {
			return nil, fmt.Errorf("parse all prompts: %w", err)
		}

		data := make(map[string]interface{})
		data["date"] = time.Now().Format("2006-01-02 15:04:05")

		for arg, value := range envArgs {
			data[arg] = value
		}
		for arg, value := range request.Params.Arguments {
			data[arg] = value
		}

		// Execute template
		var result strings.Builder
		if err := tmpl.ExecuteTemplate(&result, templateName, data); err != nil {
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

func parseAllPrompts(promptsDir string) (*template.Template, error) {
	tmpl := template.New("").Funcs(template.FuncMap{
		"dict": dict,
	})
	tmpl, err := tmpl.ParseGlob(promptsDir + "/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse template glob %q: %w", promptsDir+"/*.tmpl", err)
	}
	return tmpl, nil
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
