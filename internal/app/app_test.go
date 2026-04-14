package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	docsasset "github.com/icattlecoder/chatgpt2codex/internal/docsasset"
	"github.com/icattlecoder/chatgpt2codex/internal/proxy"
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

func TestPromptCommandPrintsPrompt(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run(context.Background(), []string{"prompt"}, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected prompt output")
	}
}

func TestRunWithoutArgsPrintsHelp(t *testing.T) {
	var stdout bytes.Buffer
	if err := Run(context.Background(), nil, &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	output := stdout.String()
	if !strings.Contains(output, "prompt") {
		t.Fatalf("expected help output to mention prompt command")
	}
}

func TestUnknownCommandIsRejected(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := Run(context.Background(), []string{"unknown"}, &stdout, &stderr); err == nil {
		t.Fatalf("expected unknown command error")
	} else if !strings.Contains(err.Error(), "unknown command \"unknown\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServeWithProxyPrintsAPISpecURL(t *testing.T) {
	originalStartProxy := startProxy
	t.Cleanup(func() {
		startProxy = originalStartProxy
	})

	ctx, cancel := context.WithCancel(context.Background())
	publicURL := "https://public.example.com"
	startProxy = func(ctx context.Context, kind, localURL string) (*proxy.Session, error) {
		if kind != "cloudflare" {
			t.Fatalf("unexpected proxy kind: %s", kind)
		}
		cancel()
		return &proxy.Session{PublicURL: publicURL}, nil
	}

	var stdout bytes.Buffer
	if err := runServe(ctx, t.TempDir(), "cloudflare", &stdout); err != nil {
		t.Fatalf("runServe returned error: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Public URL: "+publicURL+"\n") {
		t.Fatalf("expected public URL in output, got %q", output)
	}
	if !strings.Contains(output, publicURL+"/api.yaml\n") {
		t.Fatalf("expected api spec URL in output, got %q", output)
	}
}
