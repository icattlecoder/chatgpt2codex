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
	for _, snippet := range []string{"- read: Read file contents", "- bash: Execute bash commands (ls, grep, find, etc.)", "- ls: List directory contents"} {
		if !strings.Contains(result, snippet) {
			t.Fatalf("expected prompt to contain %q", snippet)
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
