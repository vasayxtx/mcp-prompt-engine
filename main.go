package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"text/template/parse"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var version = "dev"

const templateExt = ".tmpl"

func main() {
	showVersion := flag.Bool("version", false, "Show version and exit")
	promptsDir := flag.String("prompts", "./prompts", "Directory containing prompt template files")
	logFile := flag.String("log-file", "", "Path to log file (if not specified, logs to stdout)")
	templateFlag := flag.String("template", "", "Template name to render to stdout")
	flag.Parse()

	if *showVersion {
		fmt.Println("App version: ", version)
		fmt.Println("Go version: ", runtime.Version())
		return
	}

	// If template flag is provided, render the template to stdout
	if *templateFlag != "" {
		if err := renderTemplate(os.Stdout, *promptsDir, *templateFlag); err != nil {
			log.Fatal(err)
		}
		return
	}

	if err := runServer(*promptsDir, *logFile); err != nil {
		log.Fatal(err)
	}
}

func runServer(promptsDir string, logFile string) error {
	logWriter := os.Stdout
	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		defer func() { _ = file.Close() }()
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

	if err := addPromptHandlers(srv, promptsDir, logger); err != nil {
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

// addPromptHandlers scans the prompts directory for template files, extracts their descriptions and arguments,
// and registers prompt handlers with the provided server instance.
func addPromptHandlers(srv promptServer, promptsDir string, logger *slog.Logger) error {
	tmpl, err := parseAllPrompts(promptsDir)
	if err != nil {
		return fmt.Errorf("parse all prompts: %w", err)
	}

	files, err := os.ReadDir(promptsDir)
	if err != nil {
		return fmt.Errorf("read prompts directory: %w", err)
	}

	for _, file := range files {
		if !file.Type().IsRegular() || !strings.HasSuffix(file.Name(), templateExt) || strings.HasPrefix(file.Name(), "_") {
			continue
		}

		filePath := filepath.Join(promptsDir, file.Name())

		var description string
		if description, err = extractPromptDescription(filePath); err != nil {
			return fmt.Errorf("extract prompt description from %q template file: %w", filePath, err)
		}

		promptName := strings.TrimSuffix(file.Name(), templateExt)

		var args []string
		if args, err = extractPromptArguments(tmpl, promptName); err != nil {
			return fmt.Errorf("extract prompt arguments from %q template file: %w", filePath, err)
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
			promptHandler(promptsDir, promptName, description, envArgs))

		logger.Info("Prompt registered",
			"name", promptName,
			"description", description,
			"prompt_args", promptArgs,
			"env_args", envArgs)
	}

	return nil
}

func extractPromptDescription(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	content = bytes.TrimSpace(content)

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

// extractPromptArguments analyzes template to find field references using template tree traversal,
// leveraging text/template built-in functionality to automatically resolve partials
func extractPromptArguments(tmpl *template.Template, templateName string) ([]string, error) {
	targetTemplate := tmpl.Lookup(templateName)
	if targetTemplate == nil {
		if targetTemplate = tmpl.Lookup(templateName + templateExt); targetTemplate == nil {
			return nil, fmt.Errorf("template %q or %q not found", templateName, templateName+templateExt)
		}
	}

	argsMap := make(map[string]struct{})
	builtInFields := map[string]struct{}{"date": {}}
	processedTemplates := make(map[string]bool)

	// Extract arguments from the target template and all referenced templates recursively
	err := walkNodes(targetTemplate.Root, argsMap, builtInFields, tmpl, processedTemplates, []string{})
	if err != nil {
		return nil, err
	}

	args := make([]string, 0, len(argsMap))
	for arg := range argsMap {
		args = append(args, arg)
	}

	return args, nil
}

// walkNodes recursively walks the template parse tree to find variable references,
// automatically resolving template calls to include variables from referenced templates
func walkNodes(
	node parse.Node,
	argsMap map[string]struct{},
	builtInFields map[string]struct{},
	tmpl *template.Template,
	processedTemplates map[string]bool,
	path []string,
) error {
	if node == nil {
		return nil
	}

	switch n := node.(type) {
	case *parse.ActionNode:
		return walkNodes(n.Pipe, argsMap, builtInFields, tmpl, processedTemplates, path)
	case *parse.IfNode:
		if err := walkNodes(n.Pipe, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		if err := walkNodes(n.List, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		return walkNodes(n.ElseList, argsMap, builtInFields, tmpl, processedTemplates, path)
	case *parse.RangeNode:
		if err := walkNodes(n.Pipe, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		if err := walkNodes(n.List, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		return walkNodes(n.ElseList, argsMap, builtInFields, tmpl, processedTemplates, path)
	case *parse.WithNode:
		if err := walkNodes(n.Pipe, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		if err := walkNodes(n.List, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		return walkNodes(n.ElseList, argsMap, builtInFields, tmpl, processedTemplates, path)
	case *parse.ListNode:
		if n != nil {
			for _, child := range n.Nodes {
				if err := walkNodes(child, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
					return err
				}
			}
		}
	case *parse.PipeNode:
		if n != nil {
			for _, cmd := range n.Cmds {
				if err := walkNodes(cmd, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
					return err
				}
			}
		}
	case *parse.CommandNode:
		if n != nil {
			for _, arg := range n.Args {
				if err := walkNodes(arg, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
					return err
				}
			}
		}
	case *parse.FieldNode:
		if len(n.Ident) > 0 {
			fieldName := strings.ToLower(n.Ident[0])
			if _, isBuiltIn := builtInFields[fieldName]; !isBuiltIn {
				argsMap[fieldName] = struct{}{}
			}
		}
	case *parse.VariableNode:
		if len(n.Ident) > 0 {
			fieldName := strings.ToLower(n.Ident[0])
			// Skip variable names that start with $ (template variables)
			if !strings.HasPrefix(fieldName, "$") {
				if _, isBuiltIn := builtInFields[fieldName]; !isBuiltIn {
					argsMap[fieldName] = struct{}{}
				}
			}
		}
	case *parse.TemplateNode:
		templateName := n.Name
		// Check for cycles
		for _, ancestor := range path {
			if ancestor == templateName {
				return fmt.Errorf("cyclic partial reference detected: %s", strings.Join(append(path, templateName), " -> "))
			}
		}
		if !processedTemplates[templateName] {
			processedTemplates[templateName] = true
			// Try to find the template by name or name + extension
			var referencedTemplate *template.Template
			if referencedTemplate = tmpl.Lookup(templateName); referencedTemplate == nil {
				referencedTemplate = tmpl.Lookup(templateName + templateExt)
			}
			if referencedTemplate != nil && referencedTemplate.Tree != nil {
				if err := walkNodes(referencedTemplate.Root, argsMap, builtInFields, tmpl, processedTemplates, append(path, templateName)); err != nil {
					return err
				}
			}
		}
		return walkNodes(n.Pipe, argsMap, builtInFields, tmpl, processedTemplates, path)
	}
	return nil
}

func promptHandler(
	promptsDir string, promptName string, description string, envArgs map[string]string,
) func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	return func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		tmpl, err := parseAllPrompts(promptsDir)
		if err != nil {
			return nil, fmt.Errorf("parse all prompts: %w", err)
		}

		templateName := promptName
		if tmpl.Lookup(templateName) == nil {
			if tmpl.Lookup(templateName+templateExt) == nil {
				return nil, fmt.Errorf("template %q or %q not found", templateName, templateName+templateExt)
			}
			templateName = templateName + templateExt
		}

		data := make(map[string]interface{})
		data["date"] = time.Now().Format("2006-01-02 15:04:05")
		for arg, value := range envArgs {
			data[arg] = value
		}
		for arg, value := range request.Params.Arguments {
			data[arg] = value
		}

		var result strings.Builder
		if err = tmpl.ExecuteTemplate(&result, templateName, data); err != nil {
			return nil, fmt.Errorf("execute template %q: %w", templateName, err)
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
	tmpl := template.New("base").Funcs(template.FuncMap{
		"dict": dict,
	})
	var err error
	tmpl, err = tmpl.ParseGlob(filepath.Join(promptsDir, "*"+templateExt))
	if err != nil {
		return nil, fmt.Errorf("parse template glob %q: %w", filepath.Join(promptsDir, "*"+templateExt), err)
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

// renderTemplate renders a specified template to stdout with resolved partials and environment variables
func renderTemplate(w io.Writer, promptsDir string, templateName string) error {
	tmpl, err := parseAllPrompts(promptsDir)
	if err != nil {
		return fmt.Errorf("parse all prompts: %w", err)
	}

	if tmpl.Lookup(templateName) == nil {
		if tmpl.Lookup(templateName+templateExt) == nil {
			return fmt.Errorf("template %q or %q not found", templateName, templateName+templateExt)
		}
		templateName = templateName + templateExt
	}

	args, err := extractPromptArguments(tmpl, templateName)
	if err != nil {
		return fmt.Errorf("extract template arguments: %w", err)
	}

	data := make(map[string]interface{})
	data["date"] = time.Now().Format("2006-01-02 15:04:05")

	// Add environment variables to data map
	for _, arg := range args {
		// Convert arg to TITLE_CASE for env var
		envVarName := strings.ToUpper(arg)
		if envValue, exists := os.LookupEnv(envVarName); exists {
			data[arg] = envValue
		} else {
			data[arg] = "{{ " + arg + " }}"
		}
	}

	var result bytes.Buffer
	if err = tmpl.ExecuteTemplate(&result, templateName, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}
	_, err = w.Write(result.Bytes())
	return err
}
