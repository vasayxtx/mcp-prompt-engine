package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type PromptsServer struct {
	mcpServer         *server.MCPServer
	parser            *PromptsParser
	promptsDir        string
	enableJSONArgs    bool
	logger            *slog.Logger
	watcher           *fsnotify.Watcher
	registeredPrompts []string
}

// NewPromptsServer creates a new PromptsServer instance that serves prompts from the specified directory.
func NewPromptsServer(
	promptsDir string, enableJSONArgs bool, logger *slog.Logger,
) (promptsServer *PromptsServer, err error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create file watcher: %w", err)
	}
	defer func() {
		if err != nil {
			if closeErr := watcher.Close(); closeErr != nil {
				logger.Error("Failed to close file watcher", "error", closeErr)
			}
		}
	}()

	if err = watcher.Add(promptsDir); err != nil {
		return nil, fmt.Errorf("add prompts directory to watcher: %w", err)
	}

	srvHooks := &server.Hooks{}
	srvHooks.AddBeforeGetPrompt(func(ctx context.Context, id any, message *mcp.GetPromptRequest) {
		logger.Info("Received prompt request",
			"id", id, "params_name", message.Params.Name, "params_args", message.Params.Arguments)
	})
	srvHooks.AddAfterGetPrompt(func(ctx context.Context, id any, message *mcp.GetPromptRequest, result *mcp.GetPromptResult) {
		logger.Info("Processed prompt request",
			"id", id, "params_name", message.Params.Name, "params_args", message.Params.Arguments)

	})
	mcpServer := server.NewMCPServer(
		"Prompts Engine MCP Server",
		"1.0.0",
		server.WithLogging(),
		server.WithRecovery(),
		server.WithHooks(srvHooks),
		server.WithPromptCapabilities(true),
	)

	promptsServer = &PromptsServer{
		mcpServer:      mcpServer,
		parser:         &PromptsParser{},
		promptsDir:     promptsDir,
		enableJSONArgs: enableJSONArgs,
		logger:         logger,
		watcher:        watcher,
	}

	if err = promptsServer.reloadPrompts(); err != nil {
		return nil, fmt.Errorf("reload prompts: %w", err)
	}

	return promptsServer, nil
}

func (ps *PromptsServer) Close() error {
	if ps.watcher != nil {
		if err := ps.watcher.Close(); err != nil {
			return err
		}
		ps.watcher = nil
	}
	return nil
}

// ServeStdio starts the MCP server with stdio transport and file watching.
func (ps *PromptsServer) ServeStdio(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		ps.startWatcher(ctx)
	}()

	srvErrChan := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ps.logger.Info("Starting stdio server")
		srvErrChan <- server.NewStdioServer(ps.mcpServer).Listen(ctx, stdin, stdout)
	}()

	var srvErr error
	select {
	case srvErr = <-srvErrChan:
		if srvErr != nil {
			ps.logger.Error("Stdio server error", "error", srvErr)
		}
	case <-ctx.Done():
		ps.logger.Info("Context cancelled, stopping server")
	}

	wg.Wait()

	return srvErr
}

func (ps *PromptsServer) loadServerPrompts() ([]server.ServerPrompt, error) {
	tmpl, err := ps.parser.ParseDir(ps.promptsDir)
	if err != nil {
		return nil, fmt.Errorf("parse all prompts: %w", err)
	}

	files, err := os.ReadDir(ps.promptsDir)
	if err != nil {
		return nil, fmt.Errorf("read prompts directory: %w", err)
	}

	var serverPrompts []server.ServerPrompt
	for _, file := range files {
		if !isTemplateFile(file) {
			continue
		}

		filePath := filepath.Join(ps.promptsDir, file.Name())

		templateName := file.Name()
		if tmpl.Lookup(templateName) == nil {
			return nil, fmt.Errorf("template %q not found", templateName)
		}

		var description string
		if description, err = ps.parser.ExtractPromptDescriptionFromFile(filePath); err != nil {
			return nil, fmt.Errorf("extract prompt description from %q template file: %w", filePath, err)
		}

		var args []string
		if args, err = ps.parser.ExtractPromptArgumentsFromTemplate(tmpl, templateName); err != nil {
			return nil, fmt.Errorf("extract prompt arguments from %q template file: %w", filePath, err)
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
			promptOpts = append(promptOpts, mcp.WithArgument(promptArg))
		}

		promptName := strings.TrimSuffix(file.Name(), templateExt)

		serverPrompts = append(serverPrompts, server.ServerPrompt{
			Prompt:  mcp.NewPrompt(promptName, promptOpts...),
			Handler: ps.makeMCPHandler(tmpl, templateName, description, envArgs),
		})

		ps.logger.Info("Prompt will be registered",
			"name", promptName,
			"description", description,
			"prompt_args", promptArgs,
			"env_args", envArgs)
	}

	return serverPrompts, nil
}

func (ps *PromptsServer) reloadPrompts() error {
	newServerPrompts, err := ps.loadServerPrompts()
	if err != nil {
		return fmt.Errorf("load server prompts: %w", err)
	}

	if len(ps.registeredPrompts) > 0 {
		ps.mcpServer.DeletePrompts(ps.registeredPrompts...)
	}
	ps.logger.Info("Removed existing prompts", "count", len(ps.registeredPrompts))

	ps.mcpServer.AddPrompts(newServerPrompts...)
	ps.logger.Info("Added new prompts", "count", len(newServerPrompts))

	ps.registeredPrompts = make([]string, 0, len(newServerPrompts))
	for _, prompt := range newServerPrompts {
		ps.registeredPrompts = append(ps.registeredPrompts, prompt.Prompt.Name)
	}

	return nil
}

func (ps *PromptsServer) makeMCPHandler(
	tmpl *template.Template, templateName string, description string, envArgs map[string]string,
) func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		data := make(map[string]interface{})
		data["date"] = time.Now().Format("2006-01-02 15:04:05")
		for arg, value := range envArgs {
			data[arg] = value
		}
		parseMCPArgs(request.Params.Arguments, ps.enableJSONArgs, data)

		var result strings.Builder
		if err := tmpl.ExecuteTemplate(&result, templateName, data); err != nil {
			return nil, fmt.Errorf("execute template %q: %w", templateName, err)
		}

		return mcp.NewGetPromptResult(
			description,
			[]mcp.PromptMessage{
				mcp.NewPromptMessage(
					mcp.RoleUser,
					mcp.NewTextContent(strings.TrimSpace(result.String())),
				),
			},
		), nil
	}
}

// startWatcher monitors file system changes and reloads prompts
func (ps *PromptsServer) startWatcher(ctx context.Context) {
	ps.logger.Info("Started watching prompts directory for changes", "dir", ps.promptsDir)

	for {
		select {
		case event, ok := <-ps.watcher.Events:
			if !ok {
				return
			}
			if !strings.HasSuffix(event.Name, templateExt) {
				continue
			}
			ps.logger.Info("Prompt template file changed", "file", event.Name, "operation", event.Op.String())
			if err := ps.reloadPrompts(); err != nil {
				ps.logger.Error("Failed to reload prompts", "error", err)
			}

		case err, ok := <-ps.watcher.Errors:
			if !ok {
				return
			}
			ps.logger.Error("File watcher error", "error", err)

		case <-ctx.Done():
			ps.logger.Info("Stopping prompts watcher due to context cancellation")
			return
		}
	}
}

// parseMCPArgs attempts to parse each argument value as JSON when enableJSONArgs is true.
// If parsing succeeds, stores the parsed value (bool, number, nil, object, etc.) in the data map.
// If parsing fails or JSON parsing is disabled, stores the original string value.
func parseMCPArgs(args map[string]string, enableJSONArgs bool, data map[string]interface{}) {
	for key, value := range args {
		if enableJSONArgs {
			var parsed interface{}
			if err := json.Unmarshal([]byte(value), &parsed); err == nil {
				data[key] = parsed
				continue
			}
		}
		data[key] = value
	}
}

func isTemplateFile(file os.DirEntry) bool {
	return file.Type().IsRegular() && strings.HasSuffix(file.Name(), templateExt) && !strings.HasPrefix(file.Name(), "_")
}
