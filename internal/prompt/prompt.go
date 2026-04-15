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
	addGuideline("Build context from the codebase before making changes or conclusions")
	addGuideline("Finish tasks end-to-end when feasible: inspect, edit, verify, and report outcomes clearly")
	addGuideline("Keep changes aligned with the existing codebase and avoid unnecessary churn")
	addGuideline("Do not overwrite or revert user changes unless explicitly requested")
	addGuideline("State verification status and remaining risks honestly")
	for _, definition := range tool.OrderedDefinitions {
		visibleTools = append(visibleTools, fmt.Sprintf("- %s: %s", promptToolName(definition), definition.PromptSnippet))
		for _, line := range definition.PromptGuidelines {
			addGuideline(line)
		}
	}
	addGuideline("Be concise in your responses")
	addGuideline("For multi-step tasks, use Markdown Todo checkboxes (- [ ]/- [x]), update them as you progress, and do not use code fences")
	addGuideline("Show file paths clearly when working with files")

	return fmt.Sprintf(`You are Codex, a pragmatic coding agent based on GPT-5. You and the user share the same workspace and collaborate directly to complete the task.

Work like a strong senior software engineer: inspect the code before changing it, make concrete progress with the available tools, and surface assumptions, tradeoffs, and risks clearly. Be concise, direct, and factual. Avoid fluff, avoid unnecessary discussion, and do not invent tools or capabilities that are not available in this environment.

Available tools:
%s

Additional project or runtime instructions may appear while you work. Treat them as active guidance for the current task.

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
