package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/urfave/cli/v3"
)

var (
	version   = "dev"
	commit    = "unknown"
	goVersion = "unknown"
)

const templateExt = ".tmpl"

func main() {
	cmd := &cli.Command{
		Name:    "mcp-prompt-engine",
		Usage:   "A Model Control Protocol server for dynamic prompt templates",
		Version: fmt.Sprintf("%s (commit: %s, go: %s)", version, commit, goVersion),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "prompts",
				Aliases: []string{"p"},
				Value:   "./prompts",
				Usage:   "Directory containing prompt template files",
				Sources: cli.EnvVars("MCP_PROMPTS_DIR"),
			},
			&cli.StringFlag{
				Name:    "color",
				Value:   "auto",
				Usage:   "Colorize output: " + colorModesCommaSeparatedList,
				Sources: cli.EnvVars("NO_COLOR"),
				Action: func(ctx context.Context, cmd *cli.Command, value string) error {
					colorMode := ColorMode(value)
					if colorMode != colorModeAuto && colorMode != colorModeAlways && colorMode != colorModeNever {
						return fmt.Errorf("invalid color value %q, must be one of: "+colorModesCommaSeparatedList, value)
					}
					return nil
				},
			},
		},
		Commands: []*cli.Command{
			{
				Name:   "serve",
				Usage:  "Start the MCP server",
				Action: serveCommand,
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "log-file",
						Usage: "Path to log file (if not specified, logs to stdout)",
					},
					&cli.BoolFlag{
						Name:  "disable-json-args",
						Usage: "Disable JSON parsing for arguments (use string-only mode)",
					},
					&cli.BoolFlag{
						Name:  "quiet",
						Usage: "Suppress non-essential output",
					},
				},
			},
			{
				Name:      "render",
				Usage:     "Render a template to stdout",
				ArgsUsage: "<template_name>",
				Action:    renderCommand,
				Flags: []cli.Flag{
					&cli.StringSliceFlag{
						Name:    "arg",
						Aliases: []string{"a"},
						Usage:   "Template argument in name=value format (repeatable)",
					},
					&cli.BoolFlag{
						Name:  "disable-json-args",
						Usage: "Disable JSON parsing for arguments (use string-only mode)",
					},
				},
			},
			{
				Name:   "list",
				Usage:  "List available templates",
				Action: listCommand,
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "verbose",
						Usage: "Show detailed information about templates",
					},
				},
			},
			{
				Name:      "validate",
				Usage:     "Validate template syntax",
				ArgsUsage: "[template_name]",
				Action:    validateCommand,
			},
			{
				Name:   "version",
				Usage:  "Show version information",
				Action: versionCommand,
			},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			colorMode := ColorMode(cmd.String("color"))
			initializeColors(colorMode)

			// Skip validation for version command
			if cmd.Name == "version" {
				return ctx, nil
			}
			// Validate prompts directory exists
			promptsDir := cmd.String("prompts")
			if _, err := os.Stat(promptsDir); os.IsNotExist(err) {
				return ctx, fmt.Errorf("prompts directory '%s' does not exist", promptsDir)
			}
			return ctx, nil
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

// serveCommand starts the MCP server
func serveCommand(ctx context.Context, cmd *cli.Command) error {
	promptsDir := cmd.String("prompts")
	logFile := cmd.String("log-file")
	enableJSONArgs := !cmd.Bool("disable-json-args")
	quiet := cmd.Bool("quiet")

	if err := runStdioMCPServer(os.Stdout, promptsDir, logFile, enableJSONArgs, quiet); err != nil {
		return fmt.Errorf("%s: %w", errorText("failed to start MCP server"), err)
	}
	return nil
}

// renderCommand renders a template to stdout
func renderCommand(ctx context.Context, cmd *cli.Command) error {
	if cmd.Args().Len() < 1 {
		return fmt.Errorf("template name is required\n\nUsage: %s render <template_name>", cmd.Root().Name)
	}

	promptsDir := cmd.String("prompts")
	templateName := cmd.Args().First()
	args := cmd.StringSlice("arg")
	enableJSONArgs := !cmd.Bool("disable-json-args")

	// Parse args into a map
	argMap := make(map[string]string)
	for _, arg := range args {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid argument format '%s', expected name=value", arg)
		}
		argMap[parts[0]] = parts[1]
	}

	if err := renderTemplate(os.Stdout, promptsDir, templateName, argMap, enableJSONArgs); err != nil {
		return fmt.Errorf("%s '%s': %w", errorText("failed to render template"), templateText(templateName), err)
	}
	return nil
}

// listCommand lists available templates
func listCommand(ctx context.Context, cmd *cli.Command) error {
	promptsDir := cmd.String("prompts")
	verbose := cmd.Bool("verbose")

	if err := listTemplates(os.Stdout, promptsDir, verbose); err != nil {
		return fmt.Errorf("failed to list templates: %w", err)
	}
	return nil
}

// validateCommand validates template syntax
func validateCommand(ctx context.Context, cmd *cli.Command) error {
	promptsDir := cmd.String("prompts")

	var templateName string
	if cmd.Args().Len() > 0 {
		templateName = cmd.Args().First()
	}

	if err := validateTemplates(os.Stdout, promptsDir, templateName); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	return nil
}

// versionCommand shows detailed version information
func versionCommand(ctx context.Context, cmd *cli.Command) error {
	mustFprintf(os.Stdout, "Version:    %s\n", version)
	mustFprintf(os.Stdout, "Commit:     %s\n", commit)
	mustFprintf(os.Stdout, "Go Version: %s\n", goVersion)
	return nil
}

func runStdioMCPServer(w io.Writer, promptsDir string, logFile string, enableJSONArgs bool, quiet bool) error {
	// Configure logger
	logWriter := w
	if logFile != "" {
		file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		defer func() { _ = file.Close() }()
		logWriter = file
	}
	logger := slog.New(slog.NewTextHandler(logWriter, nil))

	// Create PromptsServer instance
	promptsSrv, err := NewPromptsServer(promptsDir, enableJSONArgs, logger)
	if err != nil {
		return fmt.Errorf("new prompts server: %w", err)
	}

	if !quiet {
		// Count templates for feedback
		var availableTemplates []string
		if availableTemplates, err = getAvailableTemplates(promptsDir); err != nil {
			return fmt.Errorf("get available templates: %w", err)
		}
		mustFprintf(w, "%s Found %s templates\n", successIcon(), highlightText(fmt.Sprintf("%d", len(availableTemplates))))
		mustFprintf(w, "%s Starting MCP server on %s\n", successIcon(), infoText("stdio"))
		mustFprintf(w, "%s Server ready - waiting for connections\n", successIcon())
	}

	defer func() {
		if closeErr := promptsSrv.Close(); closeErr != nil {
			logger.Error("Failed to close prompts server", "error", closeErr)
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigChan
		logger.Info("Received shutdown signal, stopping server")
		cancel()
	}()

	return promptsSrv.ServeStdio(ctx, os.Stdin, os.Stdout)
}

// renderTemplate renders a specified template to stdout with resolved partials and environment variables
func renderTemplate(w io.Writer, promptsDir string, templateName string, cliArgs map[string]string, enableJSONArgs bool) error {
	templateName = strings.TrimSpace(templateName)
	if templateName == "" {
		return fmt.Errorf("template name is required")
	}
	if !strings.HasSuffix(templateName, templateExt) {
		templateName += templateExt
	}
	availableTemplates, err := getAvailableTemplates(promptsDir)
	if err != nil {
		return err
	}
	// Check if specific template exists
	found := false
	for _, name := range availableTemplates {
		if name == templateName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("template %s not found\n\n%s:\n  %s",
			errorText(templateName),
			infoText("Available templates"), strings.Join(availableTemplates, "\n  "))
	}

	parser := &PromptsParser{}

	tmpl, err := parser.ParseDir(promptsDir)
	if err != nil {
		return fmt.Errorf("parse all prompts: %w", err)
	}

	args, err := parser.ExtractPromptArgumentsFromTemplate(tmpl, templateName)
	if err != nil {
		return fmt.Errorf("extract template arguments: %w", err)
	}

	data := make(map[string]interface{})
	data["date"] = time.Now().Format("2006-01-02 15:04:05")

	// Parse CLI args with JSON support if enabled
	parseMCPArgs(cliArgs, enableJSONArgs, data)

	// Resolve variables from CLI args and environment variables
	for _, arg := range args {
		// Check if already set by CLI args (highest priority)
		if _, exists := data[arg]; !exists {
			// Fall back to environment variables
			envVarName := strings.ToUpper(arg)
			if envValue, envExists := os.LookupEnv(envVarName); envExists {
				data[arg] = envValue
			}
		}
	}

	var result bytes.Buffer
	if err = tmpl.ExecuteTemplate(&result, templateName, data); err != nil {
		return fmt.Errorf("execute template: %w", err)
	}
	_, err = w.Write(result.Bytes())
	return err
}

// listTemplates lists all available templates in the prompts directory
func listTemplates(w io.Writer, promptsDir string, verbose bool) error {
	availableTemplates, err := getAvailableTemplates(promptsDir)
	if err != nil {
		return err
	}
	if len(availableTemplates) == 0 {
		if verbose {
			mustFprintf(w, "No templates found in %s\n", pathText(promptsDir))
		}
		return nil
	}

	parser := &PromptsParser{}
	var tmpl *template.Template
	for _, templateName := range availableTemplates {
		if !verbose {
			// Simple list without description and variables
			mustFprintf(w, "%s\n", templateText(templateName))
			continue
		}

		mustFprintf(w, "%s\n", templateText(templateName))

		var description string
		if description, err = parser.ExtractPromptDescriptionFromFile(
			filepath.Join(promptsDir, templateName),
		); err != nil {
			mustFprintf(w, "%s\n", errorText(fmt.Sprintf("Error: %v", err)))
		} else {
			if description != "" {
				mustFprintf(w, "  Description: %s\n", description)
			} else {
				mustFprintf(w, "  Description:\n")
			}
		}

		if tmpl == nil {
			if tmpl, err = parser.ParseDir(promptsDir); err != nil {
				return fmt.Errorf("parse all prompts: %w", err)
			}
		}
		var args []string
		if args, err = parser.ExtractPromptArgumentsFromTemplate(tmpl, templateName); err != nil {
			mustFprintf(w, "%s\n", errorText(fmt.Sprintf("Error: %v", err)))
		} else {
			if len(args) > 0 {
				mustFprintf(w, "  Variables: %s\n", highlightText(strings.Join(args, ", ")))
			} else {
				mustFprintf(w, "  Variables:\n")
			}
		}
	}

	return nil
}

// validateTemplates validates template syntax
func validateTemplates(w io.Writer, promptsDir string, templateName string) error {
	templateName = strings.TrimSpace(templateName)
	if templateName != "" && !strings.HasSuffix(templateName, templateExt) {
		templateName += templateExt
	}

	availableTemplates, err := getAvailableTemplates(promptsDir)
	if err != nil {
		return err
	}
	if templateName != "" {
		// Check if specific template exists
		found := false
		for _, name := range availableTemplates {
			if name == templateName {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("template %q not found in %s", templateName, promptsDir)
		}
	}
	if len(availableTemplates) == 0 {
		mustFprintf(w, "%s No templates found in %s\n", warningIcon(), pathText(promptsDir))
		return nil
	}

	parser := &PromptsParser{}

	tmpl, err := parser.ParseDir(promptsDir)
	if err != nil {
		return fmt.Errorf("parse prompts directory: %w", err)
	}

	hasErrors := false
	for _, name := range availableTemplates {
		if templateName != "" && name != templateName {
			continue // Skip if not validating this template
		}
		// Try to extract arguments (this validates basic syntax)
		if _, err = parser.ExtractPromptArgumentsFromTemplate(tmpl, name); err != nil {
			mustFprintf(w, "%s %s - %s\n", errorIcon(), templateText(name), errorText(fmt.Sprintf("Error: %v", err)))
			hasErrors = true
			continue
		}
		mustFprintf(w, "%s %s - %s\n", successIcon(), templateText(name), successText("Valid"))
	}

	if hasErrors {
		return fmt.Errorf("some templates have validation errors")
	}

	return nil
}

func getAvailableTemplates(promptsDir string) ([]string, error) {
	files, err := os.ReadDir(promptsDir)
	if err != nil {
		return nil, fmt.Errorf("read prompts directory: %w", err)
	}
	var templateFiles []string
	for _, file := range files {
		if !isTemplateFile(file) {
			continue
		}
		templateFiles = append(templateFiles, file.Name())
	}
	sort.Strings(templateFiles)
	return templateFiles, nil
}

func mustFprintf(w io.Writer, format string, a ...interface{}) {
	if _, err := fmt.Fprintf(w, format, a...); err != nil {
		panic(fmt.Sprintf("Failed to write output: %v", err))
	}
}
