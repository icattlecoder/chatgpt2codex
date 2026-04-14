package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icattlecoder/chatgpt2codex/internal/config"
	docsasset "github.com/icattlecoder/chatgpt2codex/internal/docsasset"
	"github.com/icattlecoder/chatgpt2codex/internal/gpt"
	"github.com/icattlecoder/chatgpt2codex/internal/proxy"
)

type stubGPTStore struct{}

func (stubGPTStore) Load() (config.File, error) { return config.File{}, nil }
func (stubGPTStore) Save(config.File) error     { return nil }

type stubGPTEnsurer struct {
	result  gpt.EnsureResult
	err     error
	called  bool
	request gpt.CreateRequest
}

func (s *stubGPTEnsurer) Ensure(_ context.Context, request gpt.CreateRequest) (gpt.EnsureResult, error) {
	s.called = true
	s.request = request
	return s.result, s.err
}

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

func TestRunServeStartsCloudflareAndPrintsAPISpecURL(t *testing.T) {
	originalStartProxy := startProxy
	originalNewConfigStore := newConfigStore
	originalNewGPTEnsurer := newGPTEnsurer
	t.Cleanup(func() {
		startProxy = originalStartProxy
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
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
	newConfigStore = func() (gpt.Store, error) {
		return stubGPTStore{}, nil
	}
	ensurer := &stubGPTEnsurer{result: gpt.EnsureResult{Created: true, GPTID: "g-123"}}
	newGPTEnsurer = func(store gpt.Store) (gptEnsurer, error) {
		return ensurer, nil
	}

	var stdout bytes.Buffer
	workspace := t.TempDir()
	if err := runServe(ctx, workspace, gpt.DefaultRecommendedModel, &stdout); err != nil {
		t.Fatalf("runServe returned error: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Public URL: "+publicURL+"\n") {
		t.Fatalf("expected public URL in output, got %q", output)
	}
	if !strings.Contains(output, publicURL+"/api.yaml\n") {
		t.Fatalf("expected api spec URL in output, got %q", output)
	}
	if !strings.Contains(output, "GPT created: g-123\n") {
		t.Fatalf("expected GPT created output, got %q", output)
	}
	if !ensurer.called {
		t.Fatalf("expected GPT ensurer to be called")
	}
	if ensurer.request.OpenAPISchema == "" {
		t.Fatalf("expected OpenAPISchema to be populated")
	}
	if !strings.Contains(ensurer.request.OpenAPISchema, publicURL) {
		t.Fatalf("expected OpenAPISchema to include public URL, got %q", ensurer.request.OpenAPISchema)
	}
	if !strings.Contains(ensurer.request.Instructions, "You are an expert coding assistant") {
		t.Fatalf("expected prompt instructions to be populated")
	}
	if ensurer.request.GPTName != "Codex/"+filepath.Base(workspace) {
		t.Fatalf("unexpected GPT name %q", ensurer.request.GPTName)
	}
}

func TestRunServePrintsUpdateSkippedMessage(t *testing.T) {
	originalStartProxy := startProxy
	originalNewConfigStore := newConfigStore
	originalNewGPTEnsurer := newGPTEnsurer
	t.Cleanup(func() {
		startProxy = originalStartProxy
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
	})

	ctx, cancel := context.WithCancel(context.Background())
	startProxy = func(ctx context.Context, kind, localURL string) (*proxy.Session, error) {
		cancel()
		return &proxy.Session{PublicURL: "https://public.example.com"}, nil
	}
	newConfigStore = func() (gpt.Store, error) {
		return stubGPTStore{}, nil
	}
	newGPTEnsurer = func(store gpt.Store) (gptEnsurer, error) {
		return &stubGPTEnsurer{result: gpt.EnsureResult{GPTID: "g-existing", UpdateSkipped: true}}, nil
	}

	var stdout bytes.Buffer
	if err := runServe(ctx, t.TempDir(), gpt.DefaultRecommendedModel, &stdout); err != nil {
		t.Fatalf("runServe returned error: %v", err)
	}

	if !strings.Contains(stdout.String(), "GPT update skipped (not implemented): g-existing\n") {
		t.Fatalf("expected update skipped message, got %q", stdout.String())
	}
}
