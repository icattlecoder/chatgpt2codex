package prompt

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildIncludesToolsAndGuidelines(t *testing.T) {
	cwd := filepath.Join(string(filepath.Separator), "tmp", "workspace")
	result := Build(cwd)
	for _, snippet := range []string{
		"- readFile: Read file contents",
		"- executeBash: Execute shell commands",
		"- listDirectory: List directory contents",
		"- get_runtime_context: Load repository instructions and available skills for the current working directory.",
	} {
		if !strings.Contains(result, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
		}
	}
	for _, snippet := range []string{
		"Before starting a repository task, call get_runtime_context",
		"Treat the returned SystemInstruct block as active guidance for the current task.",
		"If a listed skill is relevant, use readFile to open the referenced SKILL.md path before continuing.",
	} {
		if !strings.Contains(result, snippet) {
			t.Fatalf("expected prompt to contain guideline %q", snippet)
		}
	}
	if !strings.Contains(result, "Prefer grep/find/ls tools over bash for file exploration") {
		t.Fatalf("missing exploration guideline")
	}
	if !strings.Contains(result, "For multi-step tasks, use Markdown Todo checkboxes") {
		t.Fatalf("missing todo guideline")
	}
	if !strings.Contains(result, "do not use code fences") {
		t.Fatalf("missing no-code-fence guideline")
	}
	if !strings.Contains(result, "Current date: "+time.Now().Format("2006-01-02")) {
		t.Fatalf("missing current date")
	}
	if !strings.Contains(result, "Current working directory: "+filepath.ToSlash(cwd)) {
		t.Fatalf("missing cwd")
	}
}
