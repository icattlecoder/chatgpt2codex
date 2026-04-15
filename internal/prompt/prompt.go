package prompt

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

func Build(cwd string) string {
	visibleTools := make([]string, 0, len(tool.OrderedDefinitions))
	guidelines := make([]string, 0, 12)
	seen := map[string]bool{}
	addGuideline := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			return
		}
		seen[line] = true
		guidelines = append(guidelines, line)
	}

	addGuideline("Prefer grep/find/ls tools over bash for file exploration (faster, respects .gitignore)")
	for _, definition := range tool.OrderedDefinitions {
		visibleTools = append(visibleTools, fmt.Sprintf("- %s: %s", promptToolName(definition), definition.PromptSnippet))
		for _, line := range definition.PromptGuidelines {
			addGuideline(line)
		}
	}
	addGuideline("Be concise in your responses")
	addGuideline("For multi-step tasks, use Markdown Todo checkboxes (- [ ]/- [x]), update them as you progress, and do not use code fences")
	addGuideline("Show file paths clearly when working with files")

	return fmt.Sprintf(`You are an expert coding assistant operating through chatgpt2codex, a local coding agent tool service. You help users by reading files, executing commands, editing code, and writing new files.

Available tools:
%s

In addition to the tools above, you may have access to other custom tools depending on the project.

Guidelines:
%s

Current working directory: %s`,
		strings.Join(visibleTools, "\n"),
		formatGuidelines(guidelines),
		filepath.ToSlash(cwd),
	)
}

func formatGuidelines(lines []string) string {
	formatted := make([]string, 0, len(lines))
	for _, line := range lines {
		formatted = append(formatted, "- "+line)
	}
	return strings.Join(formatted, "\n")
}

func promptToolName(definition tool.Definition) string {
	if strings.TrimSpace(definition.PromptName) != "" {
		return definition.PromptName
	}
	return definition.Name
}
