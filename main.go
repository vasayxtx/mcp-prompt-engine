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

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "Show version and exit")
	promptsDir := flag.String("prompts", "./prompts", "Directory containing prompt markdown files")
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

	s := server.NewMCPServer(
		"Custom Prompts Server",
		"1.0.0",
		server.WithResourceCapabilities(true, true),
		server.WithLogging(),
		server.WithRecovery(),
	)

	if err := buildPrompts(s, promptsDir, logger); err != nil {
		return fmt.Errorf("build prompts: %w", err)
	}

	logger.Info("Starting stdio server")
	if err := server.ServeStdio(s); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Error starting stdio server", "error", err)
		return fmt.Errorf("serve stdio: %w", err)
	}

	return nil
}

// buildPrompts builds and registers prompts with the server
func buildPrompts(s *server.MCPServer, promptsDir string, logger *slog.Logger) error {
	readPromptFromFile := func(filePath string) (description string, prompt string, err error) {
		var fileBytes []byte
		if fileBytes, err = os.ReadFile(filePath); err != nil {
			return "", "", fmt.Errorf("read file %s: %w", filePath, err)
		}
		fileContent := string(fileBytes)
		idx := strings.IndexByte(fileContent, '\n')
		if idx == -1 {
			return "", "", fmt.Errorf("invalid prompt file format: %s", filePath)
		}
		return strings.TrimSpace(fileContent[:idx]), strings.TrimSpace(fileContent[idx+1:]), nil
	}

	files, err := os.ReadDir(promptsDir)
	if err != nil {
		return fmt.Errorf("error reading prompts directory: %v", err)
	}

	for _, file := range files {
		if file.Type().IsRegular() && strings.HasSuffix(file.Name(), ".md") {
			filePath := filepath.Join(promptsDir, file.Name())
			promptDescription, promptText, err := readPromptFromFile(filePath)
			if err != nil {
				logger.Error("Error reading prompt file", "file", filePath, "error", err)
				continue
			}

			promptName := strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))

			args := extractArguments(promptText)

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
				mcp.WithPromptDescription(promptDescription),
			}
			for _, promptArg := range promptArgs {
				promptOpts = append(promptOpts, mcp.WithArgument(promptArg, mcp.RequiredArgument()))
			}

			prompt := mcp.NewPrompt(promptName, promptOpts...)

			promptHandler := func(
				filePath string, description string, envArgs map[string]string,
			) func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				return func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
					_, userPromptMsg, readErr := readPromptFromFile(filePath)
					if readErr != nil {
						return nil, fmt.Errorf("read file %s: %w", filePath, readErr)
					}
					replaceArg := func(text, arg, value string) string {
						re := regexp.MustCompile(`\{\{\s*` + regexp.QuoteMeta(arg) + `\s*\}\}`)
						return re.ReplaceAllString(text, value)
					}

					for arg, value := range envArgs {
						userPromptMsg = replaceArg(userPromptMsg, arg, value)
					}
					for arg, value := range request.Params.Arguments {
						userPromptMsg = replaceArg(userPromptMsg, arg, value)
					}
					return mcp.NewGetPromptResult(
						description,
						[]mcp.PromptMessage{
							mcp.NewPromptMessage(
								mcp.RoleUser,
								mcp.NewTextContent(userPromptMsg),
							),
						},
					), nil
				}
			}

			s.AddPrompt(prompt, promptHandler(filePath, promptDescription, envArgs))

			logger.Info("Prompt registered",
				"name", promptName,
				"description", promptDescription,
				"prompt_args", promptArgs,
				"env_args", envArgs)
		}
	}

	return nil
}

// extractArguments finds all template arguments in the format {{arg_name}} in the given text
func extractArguments(text string) []string {
	re := regexp.MustCompile(`\{\{([^{}]+)\}\}`)
	matches := re.FindAllStringSubmatch(text, -1)

	// Use a map to deduplicate arguments
	argsMap := make(map[string]struct{})
	for _, match := range matches {
		if len(match) > 1 {
			argsMap[strings.TrimSpace(match[1])] = struct{}{}
		}
	}

	// Convert map keys to slice
	args := make([]string, 0, len(argsMap))
	for arg := range argsMap {
		args = append(args, arg)
	}

	return args
}
