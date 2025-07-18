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

	"github.com/fatih/color"
	"github.com/urfave/cli/v3"
)

var (
	version   = "dev"
	commit    = "unknown"
	goVersion = "unknown"
)

const templateExt = ".tmpl"

// Color utility functions for consistent styling
var (
	// Status indicators
	successIcon = color.New(color.FgGreen, color.Bold).SprintFunc()("✓")
	errorIcon   = color.New(color.FgRed, color.Bold).SprintFunc()("✗")
	warningIcon = color.New(color.FgYellow, color.Bold).SprintFunc()("⚠")
	_           = color.New(color.FgBlue, color.Bold).SprintFunc() // infoIcon

	// Text colors
	successText   = color.New(color.FgGreen).SprintFunc()
	errorText     = color.New(color.FgRed).SprintFunc()
	_             = color.New(color.FgYellow).SprintFunc() // warningText
	infoText      = color.New(color.FgBlue).SprintFunc()
	highlightText = color.New(color.FgCyan, color.Bold).SprintFunc()

	// Specific formatters
	templateText = color.New(color.FgMagenta, color.Bold).SprintFunc()
	pathText     = color.New(color.FgBlue).SprintFunc()
)

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

	if !quiet {
		fmt.Printf("%s Loading templates from %s\n", successIcon, pathText(promptsDir))
	}

	if err := runMCPServer(promptsDir, logFile, enableJSONArgs, quiet); err != nil {
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

	if err := renderTemplate(os.Stdout, promptsDir, templateName); err != nil {
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
	fmt.Printf("Version:    %s\n", version)
	fmt.Printf("Commit:     %s\n", commit)
	fmt.Printf("Go Version: %s\n", goVersion)
	return nil
}

func runMCPServer(promptsDir string, logFile string, enableJSONArgs bool, quiet bool) error {
	// Configure logger
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
		fmt.Printf("%s Found %s templates\n", successIcon, highlightText(fmt.Sprintf("%d", len(availableTemplates))))
		fmt.Printf("%s Starting MCP server on %s\n", successIcon, infoText("stdio"))
		fmt.Printf("%s Server ready - waiting for connections\n", successIcon)
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
func renderTemplate(w io.Writer, promptsDir string, templateName string) error {
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

// listTemplates lists all available templates in the prompts directory
func listTemplates(w io.Writer, promptsDir string, verbose bool) error {
	availableTemplates, err := getAvailableTemplates(promptsDir)
	if err != nil {
		return err
	}
	if len(availableTemplates) == 0 {
		if verbose {
			if _, ferr := fmt.Fprintf(w, "No templates found in %s\n", pathText(promptsDir)); ferr != nil {
				return ferr
			}
		}
		return nil
	}

	parser := &PromptsParser{}
	var tmpl *template.Template
	for _, templateName := range availableTemplates {
		if !verbose {
			// Simple list without description and variables
			if _, ferr := fmt.Fprintf(w, "%s\n", templateText(templateName)); ferr != nil {
				return ferr
			}
			continue
		}

		if _, ferr := fmt.Fprintf(w, "%s\n", templateText(templateName)); ferr != nil {
			return ferr
		}

		var description string
		if description, err = parser.ExtractPromptDescriptionFromFile(
			filepath.Join(promptsDir, templateName),
		); err != nil {
			if _, ferr := fmt.Fprintf(w, "%s\n", errorText(fmt.Sprintf("Error: %v", err))); ferr != nil {
				return ferr
			}
		} else {
			if description != "" {
				if _, ferr := fmt.Fprintf(w, "  Description: %s\n", description); ferr != nil {
					return ferr
				}
			} else {
				if _, ferr := fmt.Fprintf(w, "  Description:\n"); ferr != nil {
					return ferr
				}
			}
		}

		if tmpl == nil {
			if tmpl, err = parser.ParseDir(promptsDir); err != nil {
				return fmt.Errorf("parse all prompts: %w", err)
			}
		}
		var args []string
		if args, err = parser.ExtractPromptArgumentsFromTemplate(tmpl, templateName); err != nil {
			if _, ferr := fmt.Fprintf(w, "%s\n", errorText(fmt.Sprintf("Error: %v", err))); ferr != nil {
				return ferr
			}
		} else {
			if len(args) > 0 {
				if _, ferr := fmt.Fprintf(w, "  Variables: %s\n", highlightText(strings.Join(args, ", "))); ferr != nil {
					return ferr
				}
			} else {
				if _, ferr := fmt.Fprintf(w, "  Variables:\n"); ferr != nil {
					return ferr
				}
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
		if _, err := fmt.Fprintf(w, "%s No templates found in %s\n", warningIcon, pathText(promptsDir)); err != nil {
			return err
		}
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
			fmt.Printf("%s %s - %s\n", errorIcon, templateText(name), errorText(fmt.Sprintf("Error: %v", err)))
			hasErrors = true
			continue
		}
		fmt.Printf("%s %s - %s\n", successIcon, templateText(name), successText("Valid"))
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
