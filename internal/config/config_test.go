package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReturnsEmptyConfigWhenFileIsMissing(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.GPTs) != 0 {
		t.Fatalf("expected empty GPT map, got %#v", cfg.GPTs)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	cfg := File{}
	cfg.Cloudflare = CloudflareConfig{
		EnableDomain: true,
		Domain:       "chatgpt2codex.fun",
		APIToken:     "api-token",
	}
	cfg.SetWorkspace("/tmp/workspace", GPTConfig{
		GPTID:  "g-123",
		Host:   "workspace.chatgpt2codex.fun",
		APIKey: "ctc_saved_key",
	})
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if loaded.Domain != "" {
		t.Fatalf("expected legacy top-level domain to stay empty, got %q", loaded.Domain)
	}
	if !loaded.Cloudflare.EnableDomain {
		t.Fatalf("expected custom domain to round-trip as enabled")
	}
	if loaded.Cloudflare.Domain != "chatgpt2codex.fun" {
		t.Fatalf("expected cloudflare domain to round-trip, got %q", loaded.Cloudflare.Domain)
	}
	if loaded.Cloudflare.APIToken != "api-token" {
		t.Fatalf("expected api token to round-trip, got %q", loaded.Cloudflare.APIToken)
	}
	if loaded.Cloudflare.ZoneID != "" || loaded.Cloudflare.TunnelToken != "" {
		t.Fatalf("did not expect derived cloudflare fields to round-trip, got %#v", loaded.Cloudflare)
	}
	gptID, ok := loaded.GPTID("/tmp/workspace")
	if !ok {
		t.Fatalf("expected workspace mapping to exist")
	}
	if gptID != "g-123" {
		t.Fatalf("expected gpt id g-123, got %q", gptID)
	}
	entry := loaded.WorkspaceConfig("/tmp/workspace")
	if entry.Host != "workspace.chatgpt2codex.fun" {
		t.Fatalf("expected host to round-trip, got %q", entry.Host)
	}
	if entry.APIKey != "ctc_saved_key" {
		t.Fatalf("expected api key to round-trip, got %q", entry.APIKey)
	}
}

func TestLoadBackwardsCompatibleConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{\"gpts\":{\"/tmp/workspace\":{\"gpt_id\":\"g-123\"}}}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if loaded.Domain != "" {
		t.Fatalf("expected empty domain for old config, got %q", loaded.Domain)
	}
	if !loaded.Cloudflare.IsZero() {
		t.Fatalf("expected empty cloudflare config for old config, got %#v", loaded.Cloudflare)
	}
	entry := loaded.WorkspaceConfig("/tmp/workspace")
	if entry.GPTID != "g-123" {
		t.Fatalf("expected gpt id from old config, got %q", entry.GPTID)
	}
	if entry.Host != "" || entry.APIKey != "" {
		t.Fatalf("expected empty host/api key from old config, got %#v", entry)
	}
}

func TestLoadMigratesLegacyCustomDomainConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{
  "domain": "Example.COM.",
  "cloudflare": {
    "zone_id": "zone-123",
    "api_token": "api-token",
    "tunnel_token": "tunnel-token"
  },
  "gpts": {}
}
`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if loaded.Domain != "" {
		t.Fatalf("expected legacy top-level domain to be cleared, got %q", loaded.Domain)
	}
	if !loaded.Cloudflare.EnableDomain {
		t.Fatalf("expected legacy top-level domain to enable custom domain")
	}
	if loaded.Cloudflare.Domain != "example.com" {
		t.Fatalf("expected domain to migrate under cloudflare, got %q", loaded.Cloudflare.Domain)
	}
	if loaded.Cloudflare.APIToken != "api-token" {
		t.Fatalf("expected api token to migrate, got %q", loaded.Cloudflare.APIToken)
	}
	if loaded.Cloudflare.ZoneID != "" || loaded.Cloudflare.TunnelToken != "" {
		t.Fatalf("expected deprecated fields to be dropped, got %#v", loaded.Cloudflare)
	}
}

func TestCloudflareConfigValidateForCustomDomain(t *testing.T) {
	valid := CloudflareConfig{
		EnableDomain: true,
		Domain:       "chatgpt2codex.fun",
		APIToken:     "api-token",
	}
	if err := valid.ValidateForCustomDomain(); err != nil {
		t.Fatalf("expected valid cloudflare config, got %v", err)
	}
	if err := (CloudflareConfig{}).ValidateForCustomDomain(); err == nil {
		t.Fatalf("expected empty cloudflare config to fail")
	}
}

func TestClearWorkspaceHosts(t *testing.T) {
	cfg := File{
		GPTs: map[string]GPTConfig{
			"/tmp/workspace": {Host: "demo.example.com", APIKey: "ctc_test_key"},
		},
	}
	cfg.ClearWorkspaceHosts()
	entry := cfg.WorkspaceConfig("/tmp/workspace")
	if entry.Host != "" {
		t.Fatalf("expected host to be cleared, got %q", entry.Host)
	}
	if entry.APIKey != "ctc_test_key" {
		t.Fatalf("expected api key to be preserved, got %q", entry.APIKey)
	}
}

func TestWorkspaceDNSLabel(t *testing.T) {
	if got := WorkspaceDNSLabel("/tmp/My Demo_Project"); got != "my-demo-project" {
		t.Fatalf("unexpected workspace label %q", got)
	}
}

func TestDefaultHostname(t *testing.T) {
	got, err := DefaultHostname("/tmp/My Demo_Project", "Example.COM.")
	if err != nil {
		t.Fatalf("DefaultHostname returned error: %v", err)
	}
	if got != "my-demo-project.example.com" {
		t.Fatalf("unexpected hostname %q", got)
	}
}

func TestValidateDomain(t *testing.T) {
	if err := ValidateDomain("chatgpt2codex.fun"); err != nil {
		t.Fatalf("expected valid domain, got %v", err)
	}
	if err := ValidateDomain("bad domain"); err == nil {
		t.Fatalf("expected invalid domain to fail")
	}
}
