package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

const templateExt = ".tmpl"

func main() {
	showVersion := flag.Bool("version", false, "Show version and exit")
	promptsDir := flag.String("prompts", "./prompts", "Directory containing prompt template files")
	logFile := flag.String("log-file", "", "Path to log file (if not specified, logs to stdout)")
	templateFlag := flag.String("template", "", "Template name to render to stdout")
	disableJSONArgs := flag.Bool("disable-json-args", false, "Disable JSON parsing for arguments (use string-only mode)")
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

	if err := runMCPServer(*promptsDir, *logFile, !*disableJSONArgs); err != nil {
		log.Fatal(err)
	}
}

func runMCPServer(promptsDir string, logFile string, enableJSONArgs bool) error {
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
	parser := &PromptsParser{}

	tmpl, err := parser.ParseDir(promptsDir)
	if err != nil {
		return fmt.Errorf("parse all prompts: %w", err)
	}

	if tmpl.Lookup(templateName) == nil {
		if tmpl.Lookup(templateName+templateExt) == nil {
			return fmt.Errorf("template %q or %q not found", templateName, templateName+templateExt)
		}
		templateName = templateName + templateExt
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
