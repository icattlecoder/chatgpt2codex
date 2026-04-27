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

type memoryGPTStore struct {
	cfg       config.File
	saveCalls int
}

func (s *memoryGPTStore) Load() (config.File, error) { return s.cfg, nil }
func (s *memoryGPTStore) Save(cfg config.File) error {
	s.cfg = cfg
	s.saveCalls++
	return nil
}

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

func TestAcquireWorkspaceLockPreventsSecondInstance(t *testing.T) {
	originalDefaultStateDir := defaultStateDir
	baseDir := t.TempDir()
	defaultStateDir = func() (string, error) { return baseDir, nil }
	t.Cleanup(func() {
		defaultStateDir = originalDefaultStateDir
	})

	release, err := acquireWorkspaceLock("/tmp/workspace")
	if err != nil {
		t.Fatalf("acquireWorkspaceLock returned error: %v", err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Fatalf("release returned error: %v", err)
		}
	}()

	_, err = acquireWorkspaceLock("/tmp/workspace")
	if err == nil {
		t.Fatal("expected second lock acquisition to fail")
	}
	if !strings.Contains(err.Error(), "already served by pid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAcquireWorkspaceLockClearsStaleLock(t *testing.T) {
	originalDefaultStateDir := defaultStateDir
	baseDir := t.TempDir()
	defaultStateDir = func() (string, error) { return baseDir, nil }
	t.Cleanup(func() {
		defaultStateDir = originalDefaultStateDir
	})

	lockPath := filepath.Join(baseDir, "locks", workspaceLockFileName("/tmp/workspace"))
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte("999999\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	release, err := acquireWorkspaceLock("/tmp/workspace")
	if err != nil {
		t.Fatalf("acquireWorkspaceLock returned error: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release returned error: %v", err)
	}
}

func TestAcquireNamedTunnelLockPreventsSecondInstance(t *testing.T) {
	originalDefaultStateDir := defaultStateDir
	baseDir := t.TempDir()
	defaultStateDir = func() (string, error) { return baseDir, nil }
	t.Cleanup(func() {
		defaultStateDir = originalDefaultStateDir
	})

	release, err := acquireNamedTunnelLock("token-123")
	if err != nil {
		t.Fatalf("acquireNamedTunnelLock returned error: %v", err)
	}
	defer func() {
		if err := release(); err != nil {
			t.Fatalf("release returned error: %v", err)
		}
	}()

	_, err = acquireNamedTunnelLock("token-123")
	if err == nil {
		t.Fatal("expected second named tunnel lock acquisition to fail")
	}
	if !strings.Contains(err.Error(), "named tunnel is already used by pid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWithoutArgsStartsServeFromCurrentWorkingDirectory(t *testing.T) {
	originalStartProxy := startProxy
	originalGenerateAPIKey := generateAPIKey
	originalNewConfigStore := newConfigStore
	originalNewGPTEnsurer := newGPTEnsurer
	originalDefaultStateDir := defaultStateDir
	t.Cleanup(func() {
		startProxy = originalStartProxy
		generateAPIKey = originalGenerateAPIKey
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
		defaultStateDir = originalDefaultStateDir
	})
	stateDir := t.TempDir()
	defaultStateDir = func() (string, error) { return stateDir, nil }

	workspace := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	publicURL := "https://public.example.com"
	startProxy = func(ctx context.Context, request proxy.StartRequest) (*proxy.Session, error) {
		if request.Kind != "cloudflare" {
			t.Fatalf("unexpected proxy kind: %s", request.Kind)
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
	originalDefaultStateDir := defaultStateDir
	t.Cleanup(func() {
		startProxy = originalStartProxy
		generateAPIKey = originalGenerateAPIKey
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
		defaultStateDir = originalDefaultStateDir
	})
	stateDir := t.TempDir()
	defaultStateDir = func() (string, error) { return stateDir, nil }

	ctx, cancel := context.WithCancel(context.Background())
	publicURL := "https://public.example.com"
	startProxy = func(ctx context.Context, request proxy.StartRequest) (*proxy.Session, error) {
		if request.Kind != "cloudflare" {
			t.Fatalf("unexpected proxy kind: %s", request.Kind)
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
	if err := runServe(ctx, workspace, gpt.DefaultRecommendedModel, false, &stdout); err != nil {
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
	if !strings.Contains(ensurer.request.Instructions, "You are Codex, a pragmatic coding agent based on GPT-5") {
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
	originalDefaultStateDir := defaultStateDir
	t.Cleanup(func() {
		startProxy = originalStartProxy
		generateAPIKey = originalGenerateAPIKey
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
		defaultStateDir = originalDefaultStateDir
	})
	stateDir := t.TempDir()
	defaultStateDir = func() (string, error) { return stateDir, nil }

	ctx, cancel := context.WithCancel(context.Background())
	startProxy = func(ctx context.Context, request proxy.StartRequest) (*proxy.Session, error) {
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
	if err := runServe(ctx, t.TempDir(), gpt.DefaultRecommendedModel, false, &stdout); err != nil {
		t.Fatalf("runServe returned error: %v", err)
	}

	if !strings.Contains(stdout.String(), "GPT update skipped (not implemented): g-existing\n") {
		t.Fatalf("expected update skipped message, got %q", stdout.String())
	}
}

func TestRunServeWithCustomDomainSkipsGPTSyncAndReusesAPIKey(t *testing.T) {
	originalStartProxy := startProxy
	originalGenerateAPIKey := generateAPIKey
	originalNewConfigStore := newConfigStore
	originalNewGPTEnsurer := newGPTEnsurer
	originalDefaultStateDir := defaultStateDir
	t.Cleanup(func() {
		startProxy = originalStartProxy
		generateAPIKey = originalGenerateAPIKey
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
		defaultStateDir = originalDefaultStateDir
	})
	stateDir := t.TempDir()
	defaultStateDir = func() (string, error) { return stateDir, nil }

	ctx, cancel := context.WithCancel(context.Background())
	workspace := t.TempDir()
	expectedHost, err := config.DefaultHostname(workspace, "chatgpt2codex.fun")
	if err != nil {
		t.Fatalf("DefaultHostname returned error: %v", err)
	}
	store := &memoryGPTStore{cfg: config.File{
		Cloudflare: config.CloudflareConfig{
			EnableDomain: true,
			Domain:       "chatgpt2codex.fun",
			APIToken:     "api-token",
		},
		GPTs: map[string]config.GPTConfig{
			workspace: {GPTID: "g-existing", Host: expectedHost, APIKey: "ctc_saved_key"},
		},
	}}
	startProxy = func(ctx context.Context, request proxy.StartRequest) (*proxy.Session, error) {
		if request.Hostname != expectedHost {
			t.Fatalf("expected hostname %q, got %q", expectedHost, request.Hostname)
		}
		if request.Domain != "chatgpt2codex.fun" || request.APIToken != "api-token" {
			t.Fatalf("expected cloudflare config to be forwarded, got %#v", request)
		}
		cancel()
		return &proxy.Session{PublicURL: "https://" + request.Hostname}, nil
	}
	generateAPIKey = func() (string, error) {
		t.Fatalf("did not expect a new api key to be generated")
		return "", nil
	}
	newConfigStore = func() (gpt.Store, error) {
		return store, nil
	}
	ensurer := &stubGPTEnsurer{}
	newGPTEnsurer = func(store gpt.Store) (gptEnsurer, error) {
		return ensurer, nil
	}

	var stdout bytes.Buffer
	if err := runServe(ctx, workspace, gpt.DefaultRecommendedModel, false, &stdout); err != nil {
		t.Fatalf("runServe returned error: %v", err)
	}

	if ensurer.called {
		t.Fatalf("did not expect GPT ensurer to be called without --reset")
	}
	if !strings.Contains(stdout.String(), "API Key: ctc_saved_key\n") {
		t.Fatalf("expected api key output, got %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "GPT sync skipped; use --reset to refresh GPT metadata.\n") {
		t.Fatalf("expected GPT skip message, got %q", stdout.String())
	}
	entry := store.cfg.WorkspaceConfig(workspace)
	if entry.Host != expectedHost {
		t.Fatalf("expected persisted host %q, got %q", expectedHost, entry.Host)
	}
	if entry.APIKey != "ctc_saved_key" {
		t.Fatalf("expected persisted api key, got %q", entry.APIKey)
	}
}

func TestRunServeWithCustomDomainAndMissingWorkspaceEntryCreatesGPT(t *testing.T) {
	originalStartProxy := startProxy
	originalGenerateAPIKey := generateAPIKey
	originalNewConfigStore := newConfigStore
	originalNewGPTEnsurer := newGPTEnsurer
	originalDefaultStateDir := defaultStateDir
	t.Cleanup(func() {
		startProxy = originalStartProxy
		generateAPIKey = originalGenerateAPIKey
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
		defaultStateDir = originalDefaultStateDir
	})
	stateDir := t.TempDir()
	defaultStateDir = func() (string, error) { return stateDir, nil }

	ctx, cancel := context.WithCancel(context.Background())
	workspace := t.TempDir()
	host, err := config.DefaultHostname(workspace, "chatgpt2codex.fun")
	if err != nil {
		t.Fatalf("DefaultHostname returned error: %v", err)
	}
	store := &memoryGPTStore{cfg: config.File{
		Cloudflare: config.CloudflareConfig{
			EnableDomain: true,
			Domain:       "chatgpt2codex.fun",
			APIToken:     "api-token",
		},
	}}
	startProxy = func(ctx context.Context, request proxy.StartRequest) (*proxy.Session, error) {
		if request.Hostname != host {
			t.Fatalf("expected hostname %q, got %q", host, request.Hostname)
		}
		cancel()
		return &proxy.Session{PublicURL: "https://" + request.Hostname}, nil
	}
	generateAPIKey = func() (string, error) {
		return "ctc_generated_key", nil
	}
	newConfigStore = func() (gpt.Store, error) {
		return store, nil
	}
	ensurer := &stubGPTEnsurer{result: gpt.EnsureResult{Created: true, GPTID: "g-created"}}
	newGPTEnsurer = func(store gpt.Store) (gptEnsurer, error) {
		return ensurer, nil
	}

	var stdout bytes.Buffer
	if err := runServe(ctx, workspace, gpt.DefaultRecommendedModel, false, &stdout); err != nil {
		t.Fatalf("runServe returned error: %v", err)
	}

	if !ensurer.called {
		t.Fatalf("expected GPT ensurer to create when workspace config is missing")
	}
	if ensurer.request.ActionAPIKey != "ctc_generated_key" {
		t.Fatalf("expected generated api key to be used, got %q", ensurer.request.ActionAPIKey)
	}
	if !strings.Contains(stdout.String(), "GPT created: g-created\n") {
		t.Fatalf("expected GPT created output, got %q", stdout.String())
	}
	entry := store.cfg.WorkspaceConfig(workspace)
	if entry.Host != host {
		t.Fatalf("expected persisted host %q, got %q", host, entry.Host)
	}
	if entry.APIKey != "ctc_generated_key" {
		t.Fatalf("expected persisted api key, got %q", entry.APIKey)
	}
}

func TestRunServeWithCustomDomainAndMissingGPTIDCreatesGPT(t *testing.T) {
	originalStartProxy := startProxy
	originalGenerateAPIKey := generateAPIKey
	originalNewConfigStore := newConfigStore
	originalNewGPTEnsurer := newGPTEnsurer
	originalDefaultStateDir := defaultStateDir
	t.Cleanup(func() {
		startProxy = originalStartProxy
		generateAPIKey = originalGenerateAPIKey
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
		defaultStateDir = originalDefaultStateDir
	})
	stateDir := t.TempDir()
	defaultStateDir = func() (string, error) { return stateDir, nil }

	ctx, cancel := context.WithCancel(context.Background())
	workspace := t.TempDir()
	host, err := config.DefaultHostname(workspace, "chatgpt2codex.fun")
	if err != nil {
		t.Fatalf("DefaultHostname returned error: %v", err)
	}
	store := &memoryGPTStore{cfg: config.File{
		Cloudflare: config.CloudflareConfig{
			EnableDomain: true,
			Domain:       "chatgpt2codex.fun",
			APIToken:     "api-token",
		},
		GPTs: map[string]config.GPTConfig{
			workspace: {Host: host, APIKey: "ctc_saved_key"},
		},
	}}
	startProxy = func(ctx context.Context, request proxy.StartRequest) (*proxy.Session, error) {
		if request.Hostname != host {
			t.Fatalf("expected hostname %q, got %q", host, request.Hostname)
		}
		cancel()
		return &proxy.Session{PublicURL: "https://" + request.Hostname}, nil
	}
	generateAPIKey = func() (string, error) {
		t.Fatalf("did not expect a new api key to be generated")
		return "", nil
	}
	newConfigStore = func() (gpt.Store, error) {
		return store, nil
	}
	ensurer := &stubGPTEnsurer{result: gpt.EnsureResult{Created: true, GPTID: "g-created"}}
	newGPTEnsurer = func(store gpt.Store) (gptEnsurer, error) {
		return ensurer, nil
	}

	var stdout bytes.Buffer
	if err := runServe(ctx, workspace, gpt.DefaultRecommendedModel, false, &stdout); err != nil {
		t.Fatalf("runServe returned error: %v", err)
	}

	if !ensurer.called {
		t.Fatalf("expected GPT ensurer to create when gpt_id is missing")
	}
	if ensurer.request.ActionAPIKey != "ctc_saved_key" {
		t.Fatalf("expected saved api key to be reused, got %q", ensurer.request.ActionAPIKey)
	}
	if !strings.Contains(stdout.String(), "GPT created: g-created\n") {
		t.Fatalf("expected GPT created output, got %q", stdout.String())
	}
}

func TestRunServeWithCustomDomainAndResetCallsGPTSync(t *testing.T) {
	originalStartProxy := startProxy
	originalGenerateAPIKey := generateAPIKey
	originalNewConfigStore := newConfigStore
	originalNewGPTEnsurer := newGPTEnsurer
	originalDefaultStateDir := defaultStateDir
	t.Cleanup(func() {
		startProxy = originalStartProxy
		generateAPIKey = originalGenerateAPIKey
		newConfigStore = originalNewConfigStore
		newGPTEnsurer = originalNewGPTEnsurer
		defaultStateDir = originalDefaultStateDir
	})
	stateDir := t.TempDir()
	defaultStateDir = func() (string, error) { return stateDir, nil }

	ctx, cancel := context.WithCancel(context.Background())
	workspace := t.TempDir()
	host, err := config.DefaultHostname(workspace, "chatgpt2codex.fun")
	if err != nil {
		t.Fatalf("DefaultHostname returned error: %v", err)
	}
	store := &memoryGPTStore{cfg: config.File{
		Cloudflare: config.CloudflareConfig{
			EnableDomain: true,
			Domain:       "chatgpt2codex.fun",
			APIToken:     "api-token",
		},
		GPTs: map[string]config.GPTConfig{
			workspace: {GPTID: "g-existing", Host: host, APIKey: "ctc_saved_key"},
		},
	}}
	startProxy = func(ctx context.Context, request proxy.StartRequest) (*proxy.Session, error) {
		if request.Hostname != host {
			t.Fatalf("expected hostname %q, got %q", host, request.Hostname)
		}
		if request.Domain != "chatgpt2codex.fun" || request.APIToken != "api-token" {
			t.Fatalf("expected cloudflare config to be forwarded, got %#v", request)
		}
		cancel()
		return &proxy.Session{PublicURL: "https://" + request.Hostname}, nil
	}
	generateAPIKey = func() (string, error) {
		t.Fatalf("did not expect a new api key to be generated")
		return "", nil
	}
	newConfigStore = func() (gpt.Store, error) {
		return store, nil
	}
	ensurer := &stubGPTEnsurer{result: gpt.EnsureResult{Updated: true, GPTID: "g-existing"}}
	newGPTEnsurer = func(store gpt.Store) (gptEnsurer, error) {
		return ensurer, nil
	}

	var stdout bytes.Buffer
	if err := runServe(ctx, workspace, gpt.DefaultRecommendedModel, true, &stdout); err != nil {
		t.Fatalf("runServe returned error: %v", err)
	}

	if !ensurer.called {
		t.Fatalf("expected GPT ensurer to be called with --reset")
	}
	if ensurer.request.ActionAPIKey != "ctc_saved_key" {
		t.Fatalf("expected saved api key to be reused, got %q", ensurer.request.ActionAPIKey)
	}
	if !strings.Contains(ensurer.request.OpenAPISchema, "https://"+host) {
		t.Fatalf("expected OpenAPISchema to use custom host, got %q", ensurer.request.OpenAPISchema)
	}
}

func TestDomainCommandInteractiveSetAndClear(t *testing.T) {
	originalNewConfigStore := newConfigStore
	t.Cleanup(func() {
		newConfigStore = originalNewConfigStore
	})

	store := &memoryGPTStore{cfg: config.File{
		Cloudflare: config.CloudflareConfig{
			EnableDomain: true,
			Domain:       "old.example.com",
			APIToken:     "old-api-token",
		},
		GPTs: map[string]config.GPTConfig{
			"/tmp/workspace": {Host: "old.old.example.com", APIKey: "ctc_existing"},
		},
	}}
	newConfigStore = func() (gpt.Store, error) {
		return store, nil
	}

	var stdout bytes.Buffer
	cmd, err := NewRootCommand(&stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	cmd.SetIn(strings.NewReader("\napi-token-123\n"))
	cmd.SetArgs([]string{"domain", "chatgpt2codex.fun"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}
	if !store.cfg.Cloudflare.EnableDomain {
		t.Fatalf("expected custom domain to be enabled")
	}
	if store.cfg.Cloudflare.Domain != "chatgpt2codex.fun" {
		t.Fatalf("expected domain to be saved, got %q", store.cfg.Cloudflare.Domain)
	}
	if store.cfg.Cloudflare.APIToken != "api-token-123" {
		t.Fatalf("expected api token to be saved, got %q", store.cfg.Cloudflare.APIToken)
	}
	if store.cfg.Cloudflare.ZoneID != "" || store.cfg.Cloudflare.TunnelToken != "" {
		t.Fatalf("did not expect derived cloudflare values to be saved, got %#v", store.cfg.Cloudflare)
	}
	if host := store.cfg.WorkspaceConfig("/tmp/workspace").Host; host != "" {
		t.Fatalf("expected workspace host to be cleared when domain changes, got %q", host)
	}
	if !strings.Contains(stdout.String(), "Saved custom domain configuration:\n") {
		t.Fatalf("expected summary output, got %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "api-token-123") {
		t.Fatalf("expected secrets to be masked in output, got %q", stdout.String())
	}

	stdout.Reset()
	cmd, err = NewRootCommand(&stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("NewRootCommand returned error: %v", err)
	}
	cmd.SetArgs([]string{"domain", "-"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("ExecuteContext returned error: %v", err)
	}
	if store.cfg.Cloudflare.EnableDomain {
		t.Fatalf("expected custom domain to be disabled")
	}
	if store.cfg.Cloudflare.APIToken != "api-token-123" {
		t.Fatalf("expected api token to be preserved, got %q", store.cfg.Cloudflare.APIToken)
	}
	if !strings.Contains(stdout.String(), "Custom domain disabled.\n") {
		t.Fatalf("expected clear output, got %q", stdout.String())
	}
}
