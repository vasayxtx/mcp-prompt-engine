package main

import (
	"fmt"

	"github.com/fatih/color"
)

type ColorMode string

const (
	colorModeNever  ColorMode = "never"
	colorModeAlways ColorMode = "always"
	colorModeAuto   ColorMode = "auto"
)

var colorModesCommaSeparatedList = fmt.Sprintf("%s, %s, %s", colorModeAuto, colorModeAlways, colorModeNever)

// Color utility functions for consistent styling
var (
	// Status indicators
	successIcon func(...interface{}) string
	errorIcon   func(...interface{}) string
	warningIcon func(...interface{}) string

	// Text colors
	successText   func(...interface{}) string
	errorText     func(...interface{}) string
	infoText      func(...interface{}) string
	highlightText func(...interface{}) string

	// Specific formatters
	templateText func(...interface{}) string
	pathText     func(...interface{}) string
)

// initializeColors sets up color functions based on color mode
func initializeColors(colorMode ColorMode) {
	switch colorMode {
	case colorModeNever:
		color.NoColor = true
	case colorModeAlways:
		color.NoColor = false
	case colorModeAuto:
		// fatih/color automatically detects TTY using go-isatty
		// NoColor will be set to true if not a TTY
	default:
		// Default to auto
	}

	// Initialize color functions
	successIcon = color.New(color.FgGreen, color.Bold).SprintFunc()
	errorIcon = color.New(color.FgRed, color.Bold).SprintFunc()
	warningIcon = color.New(color.FgYellow, color.Bold).SprintFunc()

	successText = color.New(color.FgGreen).SprintFunc()
	errorText = color.New(color.FgRed).SprintFunc()
	infoText = color.New(color.FgBlue).SprintFunc()
	highlightText = color.New(color.FgCyan, color.Bold).SprintFunc()

	templateText = color.New(color.FgMagenta, color.Bold).SprintFunc()
	pathText = color.New(color.FgBlue).SprintFunc()

	// Apply icons with color
	successIcon = func(args ...interface{}) string {
		return color.New(color.FgGreen, color.Bold).Sprint("✓")
	}
	errorIcon = func(args ...interface{}) string {
		return color.New(color.FgRed, color.Bold).Sprint("✗")
	}
	warningIcon = func(args ...interface{}) string {
		return color.New(color.FgYellow, color.Bold).Sprint("⚠")
	}
}

func init() {
	initializeColors(colorModeAuto)
}
