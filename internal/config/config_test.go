package config

import (
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
	cfg.SetGPTID("/tmp/workspace", "g-123")
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	gptID, ok := loaded.GPTID("/tmp/workspace")
	if !ok {
		t.Fatalf("expected workspace mapping to exist")
	}
	if gptID != "g-123" {
		t.Fatalf("expected gpt id g-123, got %q", gptID)
	}
}
