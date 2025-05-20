# Custom Prompts MCP Server

A Model Control Protocol (MCP) server for managing and serving custom prompt templates.
It allows you to create reusable prompt templates with variable placeholders that can be filled in at runtime.

## Features

- Load prompt templates from markdown files
- Support for template arguments using `{{argument_name}}` syntax
- Environment variable injection into prompts
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

Create a directory to store your prompt templates. Each template should be a markdown file with the following format:

```markdown
Brief description of the prompt
Your prompt text here with {{template_argument}} placeholders.
```

The first line of the file is used as the prompt description, and the rest of the file is the prompt text.

### Example Prompt Template

Here's a piece of prompt template example for reviewing code with 3 template arguments (`programming_language`, `project_root`, and `src_path`):

```markdown
Perform a code review for the provided code

You are an experienced software developer tasked with performing a thorough code review. Your goal is to provide valuable feedback and recommendations for improvement in markdown format.

Here are the details of the code you need to review:

Programming Language:
<programming_language>
{{language}}
</programming_language>

Project Root Directory:
<project_root>
{{project_root}}
</project_root>

File or Directory for Review:
<review_path>
{{src_path}}
</review_path>

Please conduct a comprehensive code review focusing on the following aspects:
1. Code quality
2. Adherence to best practices
3. Potential bugs or logical errors
4. Performance optimization opportunities
5. Security vulnerabilities or concerns

...
```
See the full template example in [prompts/code_review.md](prompts/code_review.md).

### Running the Server

```bash
./mcp-custom-prompts -prompts /path/to/prompts/directory -log-file /path/to/log/file
```

Options:
- `-prompts`: Directory containing prompt markdown files (default: "./prompts")
- `-log-file`: Path to log file (if not specified, logs to stdout)
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
      "ARG": "ARG_VALUE"
    }
  }
}
```

### Environment Variable Injection

The server automatically injects environment variables into your prompts. If an environment variable with the same name as a template argument (in uppercase) is found, it will be used to fill the template.

For example, if your prompt contains `{{username}}` and you set the environment variable `USERNAME=john`, the server will automatically replace `{{username}}` with `john` in the prompt.

In the Claude Desktop configuration above, the `"env"` section allows you to define environment variables that will be injected into your prompts. For example, if you have `"ARG": "ARG_VALUE"` in the `"env"` section, any occurrence of `{{arg}}` in your prompts will be replaced with `"ARG_VALUE"`.

## How It Works

1. The server parses all prompt templates on startup:
   - Loads all `.md` files from the specified prompts directory where each file is treated as a separate MCP prompt
   - Extracts template arguments from each prompt (text in `{{argument}}` format) and use them as arguments for the MCP prompt
   - Note: Adding new prompt templates or template variables requires a server restart
2. When a prompt is requested, the server:
   - Reread the prompt template file to ensure the latest version is used
   - Replaces template arguments with provided values or environment variables
   - Returns the processed prompt to the client

## License

MIT License - see [LICENSE](./LICENSE) file for details.
