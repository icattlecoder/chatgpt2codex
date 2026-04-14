package gpt

import (
	"context"
	"errors"
	"testing"

	"github.com/icattlecoder/chatgpt2codex/internal/config"
)

type stubStore struct {
	cfg        config.File
	loadErr    error
	saveErr    error
	saved      config.File
	saveCalled bool
}

func (s *stubStore) Load() (config.File, error) {
	if s.loadErr != nil {
		return config.File{}, s.loadErr
	}
	return s.cfg, nil
}

func (s *stubStore) Save(cfg config.File) error {
	s.saveCalled = true
	s.saved = cfg
	return s.saveErr
}

type stubCreator struct {
	result CreateResult
	err    error
	called bool
}

func (s *stubCreator) Create(context.Context, CreateRequest) (CreateResult, error) {
	s.called = true
	return s.result, s.err
}

type stubUpdater struct {
	err    error
	called bool
}

func (s *stubUpdater) Update(context.Context, UpdateRequest) error {
	s.called = true
	return s.err
}

func TestEnsureCreatesGPTAndPersistsMapping(t *testing.T) {
	store := &stubStore{cfg: config.File{}}
	creator := &stubCreator{result: CreateResult{GPTID: "g-123"}}
	manager := NewManager(store, creator, nil)

	result, err := manager.Ensure(context.Background(), CreateRequest{
		Workspace: "/tmp/workspace",
	})
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if !result.Created {
		t.Fatalf("expected Created to be true")
	}
	if result.GPTID != "g-123" {
		t.Fatalf("expected GPTID g-123, got %q", result.GPTID)
	}
	if !creator.called {
		t.Fatalf("expected creator to be called")
	}
	if !store.saveCalled {
		t.Fatalf("expected store save to be called")
	}
	if savedID, ok := store.saved.GPTID("/tmp/workspace"); !ok || savedID != "g-123" {
		t.Fatalf("expected saved mapping g-123, got %#v", store.saved.GPTs)
	}
}

func TestEnsureSkipsCreateWhenUpdateNotImplemented(t *testing.T) {
	cfg := config.File{}
	cfg.SetGPTID("/tmp/workspace", "g-existing")
	store := &stubStore{cfg: cfg}
	creator := &stubCreator{}
	updater := &stubUpdater{err: ErrUpdateNotImplemented}
	manager := NewManager(store, creator, updater)

	result, err := manager.Ensure(context.Background(), CreateRequest{
		Workspace: "/tmp/workspace",
	})
	if err != nil {
		t.Fatalf("Ensure returned error: %v", err)
	}
	if !result.UpdateSkipped {
		t.Fatalf("expected UpdateSkipped to be true")
	}
	if result.GPTID != "g-existing" {
		t.Fatalf("expected GPTID g-existing, got %q", result.GPTID)
	}
	if creator.called {
		t.Fatalf("did not expect creator to be called")
	}
	if store.saveCalled {
		t.Fatalf("did not expect store save to be called")
	}
}

func TestEnsureReturnsCreatorError(t *testing.T) {
	store := &stubStore{cfg: config.File{}}
	creator := &stubCreator{err: errors.New("boom")}
	manager := NewManager(store, creator, nil)

	if _, err := manager.Ensure(context.Background(), CreateRequest{Workspace: "/tmp/workspace"}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestGPTNameForWorkspaceUsesDirectoryName(t *testing.T) {
	if got := GPTNameForWorkspace("/tmp/my-project"); got != "Codex/my-project" {
		t.Fatalf("expected Codex/my-project, got %q", got)
	}
}
