package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"text/template/parse"
)

type PromptsParser struct {
}

func (pp *PromptsParser) ParseDir(promptsDir string) (*template.Template, error) {
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

func (pp *PromptsParser) ExtractPromptDescriptionFromFile(filePath string) (string, error) {
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

// ExtractPromptArgumentsFromTemplate analyzes template to find field references using template tree traversal,
// leveraging text/template built-in functionality to automatically resolve partials
func (pp *PromptsParser) ExtractPromptArgumentsFromTemplate(
	tmpl *template.Template, templateName string,
) ([]string, error) {
	targetTemplate := tmpl.Lookup(templateName)
	if targetTemplate == nil {
		if strings.HasSuffix(templateName, templateExt) {
			return nil, fmt.Errorf("template %q not found", templateName)
		}
		if targetTemplate = tmpl.Lookup(templateName + templateExt); targetTemplate == nil {
			return nil, fmt.Errorf("template %q or %q not found", templateName, templateName+templateExt)
		}
	}

	argsMap := make(map[string]struct{})
	builtInFields := map[string]struct{}{"date": {}}
	processedTemplates := make(map[string]bool)

	// Extract arguments from the target template and all referenced templates recursively
	err := pp.walkNodes(targetTemplate.Root, argsMap, builtInFields, tmpl, processedTemplates, []string{})
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
func (pp *PromptsParser) walkNodes(
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
		return pp.walkNodes(n.Pipe, argsMap, builtInFields, tmpl, processedTemplates, path)
	case *parse.IfNode:
		if err := pp.walkNodes(n.Pipe, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		if err := pp.walkNodes(n.List, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		return pp.walkNodes(n.ElseList, argsMap, builtInFields, tmpl, processedTemplates, path)
	case *parse.RangeNode:
		if err := pp.walkNodes(n.Pipe, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		if err := pp.walkNodes(n.List, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		return pp.walkNodes(n.ElseList, argsMap, builtInFields, tmpl, processedTemplates, path)
	case *parse.WithNode:
		if err := pp.walkNodes(n.Pipe, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		if err := pp.walkNodes(n.List, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
			return err
		}
		return pp.walkNodes(n.ElseList, argsMap, builtInFields, tmpl, processedTemplates, path)
	case *parse.ListNode:
		if n != nil {
			for _, child := range n.Nodes {
				if err := pp.walkNodes(child, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
					return err
				}
			}
		}
	case *parse.PipeNode:
		if n != nil {
			for _, cmd := range n.Cmds {
				if err := pp.walkNodes(cmd, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
					return err
				}
			}
		}
	case *parse.CommandNode:
		if n != nil {
			for _, arg := range n.Args {
				if err := pp.walkNodes(arg, argsMap, builtInFields, tmpl, processedTemplates, path); err != nil {
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
			if referencedTemplate = tmpl.Lookup(templateName); referencedTemplate == nil && !strings.HasSuffix(templateName, templateExt) {
				referencedTemplate = tmpl.Lookup(templateName + templateExt)
			}
			if referencedTemplate == nil || referencedTemplate.Tree == nil {
				return fmt.Errorf("referenced template %q not found in %q", templateName, tmpl.Name())
			}
			if err := pp.walkNodes(referencedTemplate.Root, argsMap, builtInFields, tmpl, processedTemplates, append(path, templateName)); err != nil {
				return err
			}
		}
		return pp.walkNodes(n.Pipe, argsMap, builtInFields, tmpl, processedTemplates, path)
	}
	return nil
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
