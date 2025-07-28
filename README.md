# MCP Prompt Engine

[![Go Report Card](https://goreportcard.com/badge/github.com/vasayxtx/mcp-prompt-engine)](https://goreportcard.com/report/github.com/vasayxtx/mcp-prompt-engine)
[![GitHub release (latest by date)](https://img.shields.io/github/v/release/vasayxtx/mcp-prompt-engine)](https://github.com/vasayxtx/mcp-prompt-engine/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go.Dev reference](https://img.shields.io/badge/go.dev-reference-blue?logo=go&logoColor=white)](https://pkg.go.dev/github.com/vasayxtx/mcp-prompt-engine)

A Model Control Protocol (MCP) server for managing and serving dynamic prompt templates using Go's powerful [text/template engine](https://pkg.go.dev/text/template) engine.
Create reusable, logic-driven prompts with variables, partials, and conditionals that can be served to any [compatible MCP client](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/clients.mdx) (Claude Code, Claude Desktop, VSCode with Copilot, etc.).

## Key Features

-   **MCP Compatible**: Works out-of-the-box with any [MCP client](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/clients.mdx) that supports [prompts](https://modelcontextprotocol.io/docs/concepts/prompts).
-   **Powerful Go Templates**: Utilizes the full power of Go's `text/template` syntax, including variables, conditionals, loops, and more.
-   **Reusable Partials**: Define common components in partial templates (e.g., `_header.tmpl`) and reuse them across your prompts.
-   **Prompt Arguments**: All template variables are automatically exposed as MCP prompt arguments, allowing dynamic input from clients.
-   **Hot-Reload**: Automatically detects changes to your prompt files and reloads them without restarting the server.
-   **Rich CLI**: A modern command-line interface to list, validate, and render templates for easy development and testing.
-   **Smart Argument Handling**:
    -   Automatically parses JSON arguments (booleans, numbers, arrays, objects).
    -   Injects environment variables as fallbacks for template arguments.
-   **Containerized**: Full Docker support for easy deployment and integration.

## Getting Started

### 1. Installation

Install using Go:
```bash
go install github.com/vasayxtx/mcp-prompt-engine@latest
```
(For other methods like Docker or pre-built binaries, see the [Installation section](#installation) below.)

### 2. Create a Prompt

Create a `prompts` directory and add a template file. Let's create a prompt to help write a Git commit message.

First, create a reusable partial named `prompts/_git_commit_role.tmpl`:
    ```go
    {{ define "_git_commit_role" }}
    You are an expert programmer specializing in writing clear, concise, and conventional Git commit messages.
    Commit message must strictly follow the Conventional Commits specification.

    The final commit message you generate must be formatted exactly as follows:

    ```
    <type>: A brief, imperative-tense summary of changes

    [Optional longer description, explaining the "why" of the change. Use dash points for clarity.]
    ```
    {{ if .type -}}
    Use {{.type}} as a type.
    {{ end }}
    {{ end }}
    ```

Now, create a main prompt `prompts/git_stage_commit.tmpl` that uses this partial:
    ```go
    {{- /* Commit currently staged changes */ -}}

    {{- template "_git_commit_role" . -}}

    Your task is to commit all currently staged changes.
    To understand the context, analyze the staged code using the command: `git diff --staged`
    Based on that analysis, commit staged changes using a suitable commit message.
    ```

### 3. Validate Your Prompt

Validate your prompt to ensure it has no syntax errors:
```bash
mcp-prompt-engine validate git_stage_commit
✓ git_stage_commit.tmpl - Valid
```

### 4. Connect MCP Server to Your Client

Connect your MCP client (like Claude Code or Claude Desktop) to the running server. See [Connecting to Clients](#connecting-to-clients) for configuration examples.

### 5. Use Your Prompt

Your `git_stage_commit` prompt will now be available in your client!

For example, in Claude Desktop, you can select the `git_stage_commit` prompt, provide the `type` MCP Prompt argument and get a generated prompt that will help you to do a commit with a perfect message.

In Claude Code, you can start typing `/git_stage_commit` and it will suggest the prompt with the provided arguments that will be executed after you select it.

---

## Installation

### Pre-built Binaries

Download the latest release for your OS from the [GitHub Releases page](https://github.com/vasayxtx/mcp-prompt-engine/releases).

### Build from Source

```bash
git clone https://github.com/vasayxtx/mcp-prompt-engine.git
cd mcp-prompt-engine
make build
```

### Docker

A pre-built Docker image is available. Mount your local `prompts` and `logs` directories to the container.

```bash
# Pull and run the pre-built image from GHCR
docker run -i --rm \
  -v /path/to/your/prompts:/app/prompts:ro \
  -v /path/to/your/logs:/app/logs \
  ghcr.io/vasayxtx/mcp-prompt-engine
```

You can also build the image locally with `make docker-build`.

---

## Usage

### Creating Prompt Templates

Create a directory to store your prompt templates. Each template should be a `.tmpl` file using Go's [text/template](https://pkg.go.dev/text/template) syntax with the following format:

```go
{{/* Brief description of the prompt */}}
Your prompt text here with {{.template_variable}} placeholders.
```

The first line comment (`{{/* description */}}`) is used as the prompt description, and the rest of the file is the prompt template.

Partial templates should be prefixed with an underscore (e.g., `_header.tmpl`) and can be included in other templates using `{{template "partial_name" .}}`.

### Template Syntax

The server uses Go's `text/template` engine, which provides powerful templating capabilities:

- **Variables**: `{{.variable_name}}` - Access template variables
- **Built-in variables**:
    - `{{.date}}` - Current date and time
- **Conditionals**: `{{if .condition}}...{{end}}`, `{{if .condition}}...{{else}}...{{end}}`
- **Logical operators**: `{{if and .condition1 .condition2}}...{{end}}`, `{{if or .condition1 .condition2}}...{{end}}`
- **Loops**: `{{range .items}}...{{end}}`
- **Template inclusion**: `{{template "partial_name" .}}` or `{{template "partial_name" dict "key" "value"}}`

See the [Go text/template documentation](https://pkg.go.dev/text/template) for more details on syntax and features.

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

To disable JSON parsing and treat all arguments as strings, use the `--disable-json-args` flag for the `serve` and `render` commands.

### CLI Commands

The CLI is your main tool for managing and testing templates.
By default, it looks for templates in the `./prompts` directory, but you can specify a different directory with the `--prompts` flag.

**1. List Templates**
```bash
# See a simple list of available prompts
mcp-prompt-engine list

# See a detailed view with descriptions and variables
mcp-prompt-engine list --verbose
```

**2. Render a Template**

Render a prompt directly in your terminal, providing arguments with the `-a` or `--arg` flag.
It will automatically inject environment variables as fallbacks for any missing arguments. For example, if you have an environment variable `TYPE=fix`, it will be injected into the template as `{{.type}}`.

```bash
# Render the git commit prompt, providing the 'type' variable
mcp-prompt-engine render git_stage_commit --arg type=feat
```

**3. Validate Templates**

Check all your templates for syntax errors. The command will return an error if any template is invalid.
```bash
# Validate all templates in the directory
mcp-prompt-engine validate

# Validate a single template
mcp-prompt-engine validate git_stage_commit
```

**4. Start the Server**

Run the MCP server to make your prompts available to clients.
```bash
# Run with default settings (looks for ./prompts)
mcp-prompt-engine serve

# Specify a different prompts directory and a log file
mcp-prompt-engine --prompts /path/to/prompts serve --log-file ./server.log
```

---

## Connecting to Clients

To use this engine with a client like **Claude Desktop**, add a new entry to its MCP servers configuration.

**Example for a local binary:**
```json
{
  "prompts": {
    "command": "/path/to/your/mcp-prompt-engine",
    "args": [
      "--prompts", "/path/to/your/prompts",
      "serve",
      "--quiet"
    ]
  }
}
```

**Example for Docker:**
```json
{
  "mcp-prompt-engine-docker": {
    "command": "docker",
    "args": [
      "run", "-i", "--rm",
      "-v", "/path/to/your/prompts:/app/prompts:ro",
      "-v", "/path/to/your/logs:/app/logs",
      "ghcr.io/vasayxtx/mcp-prompt-engine"
    ]
  }
}
```

## License

This project is licensed under the MIT License - see the [LICENSE](./LICENSE) file for details.
