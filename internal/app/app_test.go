package app

import (
	"bytes"
	"context"
	"testing"

	docsasset "github.com/icattlecoder/chatgpt2codex/docs"
)

func TestToolsCommandPrintsEmbeddedSpec(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"tools"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if stdout.String() != docsasset.ToolsAPISpec {
		t.Fatalf("tools output mismatch")
	}
}

func TestPromptAliasPrintsPrompt(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"prompt"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected prompt output")
	}
}
