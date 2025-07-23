# MCP Prompt Engine

A Model Control Protocol (MCP) server for managing and serving dynamic prompt templates using Go's [text/template engine](https://pkg.go.dev/text/template).
It allows you to create reusable prompt templates with variable placeholders, partials, conditionals, and loops that can be filled in at runtime.

## Features

- **Rich CLI Interface**: Modern command-line interface with subcommands, colored output, and comprehensive help
- **Template Management**: List, validate, and render templates directly from the command line
- **Go Template Engine**: Full `text/template` syntax with variables, partials, conditionals, and loops
- **Automatic JSON Parsing**: Intelligent argument parsing with JSON support and string fallback
- **Environment Variables**: Automatic injection of environment variables into templates
- **Hot-Reload**: Efficient file watching with automatic template reloading using fsnotify
- **MCP Compatible**: Works seamlessly with Claude Desktop, Claude Code, VSCode+Copilot, and other [MCP clients](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/clients.mdx) that support [MCP prompts](https://modelcontextprotocol.io/docs/concepts/prompts)

## Installation

### Pre-built Binaries

Download the latest release from [GitHub Releases](https://github.com/vasayxtx/mcp-prompt-engine/releases) for your platform.

### Go Install

```bash
go install github.com/vasayxtx/mcp-prompt-engine@latest
```

### Building from source

```bash
make build
```

### Docker

Run the MCP server in a Docker container using the pre-built image:

```bash
# Run using the pre-built Docker image
docker run -i --rm \
  -v /path/to/prompts:/app/prompts:ro \
  -v /path/to/logs:/app/logs \
  ghcr.io/vasayxtx/mcp-prompt-engine
```

Or build and run locally:

```bash
# Build the Docker image
make docker-build

# Run the server with mounted volumes
make docker-run
```

The Docker container runs as a non-root user and mounts the `prompts` and `logs` directories from the host system.

## Usage

### Creating Prompt Templates

Create a directory to store your prompt templates. Each template should be a `.tmpl` file using Go's [text/template](https://pkg.go.dev/text/template) syntax with the following format:

```go
{{/* Brief description of the prompt */}}
Your prompt text here with {{.template_variable}} placeholders.
```

The first line comment (`{{/* description */}}`) is used as the prompt description, and the rest of the file is the prompt template.

### Template Syntax

The server uses Go's `text/template` engine, which provides powerful templating capabilities:

- **Variables**: `{{.variable_name}}` - Access template variables
- **Built-in variables**: 
  - `{{.date}}` - Current date and time
- **Conditionals**: `{{if .condition}}...{{end}}`, `{{if .condition}}...{{else}}...{{end}}`
- **Logical operators**: `{{if and .condition1 .condition2}}...{{end}}`, `{{if or .condition1 .condition2}}...{{end}}`
- **Loops**: `{{range .items}}...{{end}}`
- **Template inclusion**: `{{template "partial_name" .}}` or `{{template "partial_name" dict "key" "value"}}`

### JSON Argument Parsing

The server automatically parses argument values as JSON when possible, enabling rich data types in templates:

- **Booleans**: `true`, `false` → Go boolean values
- **Numbers**: `42`, `3.14` → Go numeric values  
- **Arrays**: `["item1", "item2"]` → Go slices for use with `{{range}}`
- **Objects**: `{"key": "value"}` → Go maps for structured data
- **Strings**: Invalid JSON falls back to string values

This allows for advanced template operations like:
```go
{{range .items}}Item: {{.}}{{end}}
{{if .enabled}}Feature is enabled{{end}}
{{.config.timeout}} seconds
```

To disable JSON parsing and treat all arguments as strings, use the `--disable-json-args` flag.

### Partials (Reusable Components)

Create reusable template components by prefixing filenames with `_`. These partials can be included in other templates using the `{{template "partial_name" .}}` syntax. The system automatically detects which partials are used by each template:

**Example partial** (`_header.tmpl`):
```go
{{/* Common header partial */}}
You are an experienced {{.role}} tasked with {{.task}}.
Current date: {{.date}}
{{if .context}}Context: {{.context}}{{end}}
```

**Using partials in main templates**:
```go
{{/* Main prompt using header partial */}}
{{template "_header" dict "role" "software developer" "task" "code review" "context" .context}}

Please review the following code:
{{.code}}
```

### Built-in Functions

The server provides these built-in template functions:

- `dict` - Create a map from key-value pairs: `{{template "partial" dict "key1" "value1" "key2" "value2"}}`

### Example Prompt Template

Here's a complete example of a code review prompt template (`code_review.tmpl`):

```go
{{/* Perform a code review. Optionally include urgency. Args: language, project_root, src_path, [urgency_level], [context] */}}
{{template "_header" dict "role" "software developer" "task" "performing a thorough code review" "date" .date "context" .context}}

Here are the details of the code you need to review:

Programming Language:
<programming_language>
{{.language}}
</programming_language>

Project Root Directory:
<project_root>
{{.project_root}}
</project_root>

File or Directory for Review:
<review_path>
{{.src_path}}
</review_path>

{{if .urgency_level}}
Urgency: Please address this review with {{.urgency_level}} urgency.
{{end}}

Please conduct a comprehensive code review focusing on the following aspects:
1. Code quality
2. Adherence to best practices
3. Potential bugs or logical errors
4. Performance optimization opportunities
5. Security vulnerabilities or concerns

{{template "_analysis_footer" dict "analysis_type" "review"}}

Remember to be specific in your recommendations, providing clear guidance on how to improve the code.
```

## CLI Usage

The MCP Prompt Engine provides a modern command-line interface with multiple subcommands for different operations.

### Basic Commands

```bash
# Show help and available commands
mcp-prompt-engine --help

# Show version information
mcp-prompt-engine --version

# Control colored output (auto, always, never)
mcp-prompt-engine --color=never list
```

### Starting the MCP Server

```bash
# Start the server with default settings
mcp-prompt-engine serve

# Start with custom prompts directory and options
mcp-prompt-engine --prompts /path/to/prompts serve --log-file /path/to/log/file --quiet

# Start with JSON argument parsing disabled
mcp-prompt-engine serve --disable-json-args
```

### Template Management

**List available templates:**
```bash
# Simple list
mcp-prompt-engine list

# Detailed list with descriptions and variables
mcp-prompt-engine --prompts /path/to/prompts list --verbose
```

**Render a template to stdout:**
```bash
# Render a specific template
mcp-prompt-engine render template_name

# Render with custom prompts directory
mcp-prompt-engine --prompts /path/to/prompts render code_review

# Render with CLI arguments
mcp-prompt-engine render greeting --arg name=John --arg show_extra_message=true

# Render with string-only mode (disable JSON parsing)
mcp-prompt-engine render greeting --arg name=John --disable-json-args
```

**Validate template syntax:**
```bash
# Validate all templates
mcp-prompt-engine validate

# Validate a specific template
mcp-prompt-engine validate template_name
```

### Global Options

- `--prompts, -p`: Directory containing prompt template files (default: "./prompts")
  - Can also be set via `MCP_PROMPTS_DIR` environment variable
- `--color`: Control colored output: `auto` (default), `always`, or `never`
  - `auto`: Use colors only when outputting to a terminal
  - `always`: Always use colors regardless of output destination
  - `never`: Never use colors
- `--help, -h`: Show help information
- `--version, -v`: Show version information

### Serve Command Options

- `--log-file`: Path to log file (if not specified, logs to stdout)
- `--disable-json-args`: Disable JSON argument parsing, treat all arguments as strings
- `--quiet`: Suppress non-essential output for cleaner logs

### Render Command Options

- `--arg, -a`: Template argument in name=value format (repeatable)
  - CLI arguments take precedence over environment variables
  - JSON values are automatically parsed when possible (e.g., `true`, `false`, numbers, arrays, objects)
- `--disable-json-args`: Disable JSON parsing for arguments, treat all values as strings

### List Command Options

- `--verbose`: Show detailed information including template descriptions and variables

The CLI provides colored output and helpful error messages to improve the user experience.

## Configuring Claude Desktop

To use this MCP server with Claude Desktop, add the following configuration to your Claude Desktop settings:

```json
{
  "my-prompts": {
    "command": "/path/to/mcp-prompt-engine",
    "args": [
      "--prompts",
      "/path/to/prompts/dir",
      "serve",
      "--log-file",
      "/path/to/log/file",
      "--quiet"
    ]
  }
}
```

If you want to run the server within a Docker container, you can use the following configuration:

```json
{
  "mcp-prompt-engine": {
    "command": "docker",
    "args": [
      "run", "-i", "--rm",
      "-v", "/path/to/prompts/dir:/app/prompts:ro",
      "-v", "/path/to/logs/dir:/app/logs",
      "ghcr.io/vasayxtx/mcp-prompt-engine"
    ]
  }
}
```

### Environment Variable Configuration

The server supports environment variable configuration and injection:

**Configuration via Environment Variables:**
- `MCP_PROMPTS_DIR`: Set the default prompts directory (equivalent to `--prompts` flag)

**Template Variable Injection:**
The server supports multiple ways to provide values for template variables:

1. **CLI Arguments** (highest priority): Use `--arg name=value` when rendering templates
2. **Environment Variables**: Automatically injected if an environment variable with the same name as a template variable (in uppercase) is found

For example:
- CLI argument: `mcp-prompt-engine render greeting --arg username=john`
- Environment variable: If your prompt contains `{{.username}}` and you set `USERNAME=john`, it will be automatically used

CLI arguments take precedence over environment variables, allowing you to override defaults on a per-render basis.

In the Claude Desktop configuration above, the `"env"` section allows you to define environment variables that will be injected into your prompts.

## How It Works

1. **CLI Interface**: The application uses a modern CLI framework:
   - Built with [urfave/cli/v3](https://github.com/urfave/cli) for robust command-line interface
   - Colored output using [fatih/color](https://github.com/fatih/color) for better user experience
   - Hierarchical command structure with global and command-specific options
   - Comprehensive help system and error handling

2. **Server startup**: The server parses all `.tmpl` files on startup:
   - Loads partials (files starting with `_`) for reuse
   - Loads main prompt templates (files not starting with `_`)
   - Extracts template variables by analyzing the template content and its used partials
   - Only partials that are actually referenced by the template are included
   - Template arguments are extracted from patterns like `{{.fieldname}}` and `dict "key" .value`
   - Sets up efficient file watching using fsnotify for hot-reload capabilities
   - Provides startup feedback with template count and status indicators

3. **File watching and hot-reload**: The server automatically detects changes:
   - Monitors the prompts directory for file modifications, additions, and removals
   - Automatically reloads templates when changes are detected
   - No server restart required when adding new templates or modifying existing ones

4. **Prompt request processing**: When a prompt is requested:
   - Uses the latest version of templates (automatically reloaded if changed)
   - Prepares template data with built-in variables (like `date`)
   - Merges environment variables and request parameters
   - Executes the template with all data
   - Returns the processed prompt to the client

## License

MIT License - see [LICENSE](./LICENSE) file for details.
