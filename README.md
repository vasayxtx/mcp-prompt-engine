# Custom Prompts MCP Server

A Model Control Protocol (MCP) server for managing and serving custom prompt templates using Go's `text/template` engine.
It allows you to create reusable prompt templates with variable placeholders and partials that can be filled in at runtime.

## Features

- Load prompt templates from `.tmpl` files using Go's `text/template` syntax
- Support for template variables using `{{.variable_name}}` syntax
- Template partials support (files with `_` prefix for reusable components)
- Template comment-based descriptions (`{{/* description */}}` on first line)
- Environment variable injection into prompts
- Built-in template functions and variables
- Compatible with Claude Desktop and other MCP clients
- Simple stdio-based interface

## Installation

```bash
go install github.com/vasayxtx/mcp-custom-prompts@latest
```

### Building from source

```bash
make build
```

## Usage

### Creating Prompt Templates

Create a directory to store your prompt templates. Each template should be a `.tmpl` file using Go's `text/template` syntax with the following format:

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
- **Loops**: `{{range .items}}...{{end}}`
- **Template inclusion**: `{{template "partial_name" .}}` or `{{template "partial_name" dict "key" "value"}}`

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
{{/* Perform a code review for the provided code */}}
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

Please conduct a comprehensive code review focusing on the following aspects:
1. Code quality
2. Adherence to best practices
3. Potential bugs or logical errors
4. Performance optimization opportunities
5. Security vulnerabilities or concerns

{{template "_analysis_footer" dict "analysis_type" "review"}}

Remember to be specific in your recommendations, providing clear guidance on how to improve the code.
```

### Running the Server

```bash
./mcp-custom-prompts -prompts /path/to/prompts/directory -log-file /path/to/log/file
```

### Rendering a Template to Stdout

You can also render a specific template directly to stdout without starting the server:

```bash
./mcp-custom-prompts -prompts /path/to/prompts/directory -template template_name
```

This is useful for testing templates or using them in shell scripts.

Options:
- `-prompts`: Directory containing prompt template files (default: "./prompts")
- `-log-file`: Path to log file (if not specified, logs to stdout)
- `-template`: Template name to render to stdout (bypasses server mode)
- `-version`: Show version and exit

## Configuring Claude Desktop

To use this MCP server with Claude Desktop, add the following configuration to your Claude Desktop settings:

```json
{
  "custom-prompts": {
    "command": "/path/to/mcp-custom-prompts",
    "args": [
      "-prompts",
      "/path/to/directory/with/prompts",
      "-log-file",
      "/path/to/log/file"
    ],
    "env": {
      "CONTEXT": "Default context value",
      "PROJECT_ROOT": "/path/to/project"
    }
  }
}
```

### Environment Variable Injection

The server automatically injects environment variables into your prompts. If an environment variable with the same name as a template variable (in uppercase) is found, it will be used to fill the template.

For example, if your prompt contains `{{.username}}` and you set the environment variable `USERNAME=john`, the server will automatically replace `{{.username}}` with `john` in the prompt.

In the Claude Desktop configuration above, the `"env"` section allows you to define environment variables that will be injected into your prompts.

## How It Works

1. **Server startup**: The server parses all `.tmpl` files on startup:
   - Loads partials (files starting with `_`) for reuse
   - Loads main prompt templates (files not starting with `_`)
   - Extracts template variables by analyzing the template content and its used partials
   - Only partials that are actually referenced by the template are included
   - Template arguments are extracted from patterns like `{{.fieldname}}` and `dict "key" .value`
   - Note: Adding new templates or variables requires a server restart

2. **Prompt request processing**: When a prompt is requested:
   - Re-reads and re-parses the template file to ensure latest version
   - Prepares template data with built-in variables (like `date`)
   - Merges environment variables and request parameters
   - Executes the template with all data
   - Returns the processed prompt to the client

## License

MIT License - see [LICENSE](./LICENSE) file for details.
