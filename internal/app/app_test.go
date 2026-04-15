package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icattlecoder/chatgpt2codex/internal/config"
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

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd returned error: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir returned error: %v", err)
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restore Chdir returned error: %v", err)
		}
	}()
	fn()
}

func TestRunWithoutArgsStartsServeFromCurrentWorkingDirectory(t *testing.T) {
	originalStartProxy := startProxy
	originalGenerateAPIKey := generateAPIKey
	originalNewConfigStore := newConfigStore
	originalNewGPTEnsurer := newGPTEnsurer
	t.Cleanup(func() {
		startProxy = originalStartProxy
		generateAPIKey = originalGenerateAPIKey
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
	})

	workspace := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	publicURL := "https://public.example.com"
	startProxy = func(ctx context.Context, kind, localURL string) (*proxy.Session, error) {
		if kind != "cloudflare" {
			t.Fatalf("unexpected proxy kind: %s", kind)
		}
		cancel()
		return &proxy.Session{PublicURL: publicURL}, nil
	}
	generateAPIKey = func() (string, error) {
		return "ctc_test_key", nil
	}
	newConfigStore = func() (gpt.Store, error) {
		return stubGPTStore{}, nil
	}
	ensurer := &stubGPTEnsurer{result: gpt.EnsureResult{Created: true, GPTID: "g-123"}}
	newGPTEnsurer = func(store gpt.Store) (gptEnsurer, error) {
		return ensurer, nil
	}

	var stdout bytes.Buffer
	withWorkingDir(t, workspace, func() {
		if err := Run(ctx, nil, &stdout, &bytes.Buffer{}); err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	})

	output := stdout.String()
	if !strings.Contains(output, "Public URL: "+publicURL+"\n") {
		t.Fatalf("expected public URL in output, got %q", output)
	}
	if !strings.Contains(output, "API Key: ctc_test_key\n") {
		t.Fatalf("expected api key in output, got %q", output)
	}
	if !strings.Contains(output, "GPT created: g-123\n") {
		t.Fatalf("expected GPT created output, got %q", output)
	}
	if !ensurer.called {
		t.Fatalf("expected GPT ensurer to be called")
	}
	expectedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	actualWorkspace, err := filepath.EvalSymlinks(ensurer.request.Workspace)
	if err != nil {
		t.Fatalf("EvalSymlinks returned error: %v", err)
	}
	if actualWorkspace != expectedWorkspace {
		t.Fatalf("expected workspace %q, got %q", expectedWorkspace, actualWorkspace)
	}
	if ensurer.request.GPTName != "Codex/"+filepath.Base(workspace) {
		t.Fatalf("unexpected GPT name %q", ensurer.request.GPTName)
	}
}

func TestUnknownCommandIsRejected(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := Run(context.Background(), []string{"serve"}, &stdout, &stderr); err == nil {
		t.Fatalf("expected unknown command error")
	} else if !strings.Contains(err.Error(), "unknown command \"serve\"") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunServeStartsCloudflareAndPrintsPublicURL(t *testing.T) {
	originalStartProxy := startProxy
	originalGenerateAPIKey := generateAPIKey
	originalNewConfigStore := newConfigStore
	originalNewGPTEnsurer := newGPTEnsurer
	t.Cleanup(func() {
		startProxy = originalStartProxy
		generateAPIKey = originalGenerateAPIKey
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
	generateAPIKey = func() (string, error) {
		return "ctc_test_key", nil
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
	if strings.Contains(output, "/api.yaml") {
		t.Fatalf("did not expect api.yaml output, got %q", output)
	}
	if !strings.Contains(output, "API Key: ctc_test_key\n") {
		t.Fatalf("expected api key in output, got %q", output)
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
	if ensurer.request.ActionAPIKey != "ctc_test_key" {
		t.Fatalf("expected action api key to be passed through, got %q", ensurer.request.ActionAPIKey)
	}
}

func TestRunServePrintsUpdateSkippedMessage(t *testing.T) {
	originalStartProxy := startProxy
	originalGenerateAPIKey := generateAPIKey
	originalNewConfigStore := newConfigStore
	originalNewGPTEnsurer := newGPTEnsurer
	t.Cleanup(func() {
		startProxy = originalStartProxy
		generateAPIKey = originalGenerateAPIKey
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
	})

	ctx, cancel := context.WithCancel(context.Background())
	startProxy = func(ctx context.Context, kind, localURL string) (*proxy.Session, error) {
		cancel()
		return &proxy.Session{PublicURL: "https://public.example.com"}, nil
	}
	generateAPIKey = func() (string, error) {
		return "ctc_update_key", nil
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
