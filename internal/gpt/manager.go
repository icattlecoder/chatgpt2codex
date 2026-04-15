package gpt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/icattlecoder/chatgpt2codex/internal/config"
)

var ErrUpdateNotImplemented = errors.New("gpt update is not implemented")

const DefaultRecommendedModel = "GPT-5.4 Thinking"

type Store interface {
	Load() (config.File, error)
	Save(config.File) error
}

type Creator interface {
	Create(context.Context, CreateRequest) (CreateResult, error)
}

type Updater interface {
	Update(context.Context, UpdateRequest) error
}

type Manager struct {
	store   Store
	creator Creator
	updater Updater
}

type CreateRequest struct {
	Workspace        string
	GPTName          string
	Instructions     string
	OpenAPISchema    string
	RecommendedModel string
	ActionAPIKey     string
	ProgressWriter   io.Writer
}

type CreateResult struct {
	GPTID     string
	EditorURL string
}

type UpdateRequest struct {
	Workspace        string
	GPTID            string
	GPTName          string
	Instructions     string
	OpenAPISchema    string
	RecommendedModel string
	ActionAPIKey     string
	ProgressWriter   io.Writer
}

type EnsureResult struct {
	GPTID         string
	Created       bool
	Updated       bool
	UpdateSkipped bool
}

type noopUpdater struct{}

func NewManager(store Store, creator Creator, updater Updater) *Manager {
	if updater == nil {
		updater = noopUpdater{}
	}
	return &Manager{
		store:   store,
		creator: creator,
		updater: updater,
	}
}

func (m *Manager) Ensure(ctx context.Context, request CreateRequest) (EnsureResult, error) {
	if m == nil {
		return EnsureResult{}, errors.New("gpt manager is nil")
	}
	if m.store == nil {
		return EnsureResult{}, errors.New("gpt store is nil")
	}

	workspace := strings.TrimSpace(request.Workspace)
	if workspace == "" {
		return EnsureResult{}, errors.New("workspace is required")
	}

	cfg, err := m.store.Load()
	if err != nil {
		return EnsureResult{}, err
	}

	if gptID, ok := cfg.GPTID(workspace); ok {
		updateErr := m.updater.Update(ctx, UpdateRequest{
			Workspace:        workspace,
			GPTID:            gptID,
			GPTName:          request.GPTName,
			Instructions:     request.Instructions,
			OpenAPISchema:    request.OpenAPISchema,
			RecommendedModel: request.RecommendedModel,
			ActionAPIKey:     request.ActionAPIKey,
			ProgressWriter:   request.ProgressWriter,
		})
		if updateErr != nil {
			if errors.Is(updateErr, ErrUpdateNotImplemented) {
				return EnsureResult{
					GPTID:         gptID,
					UpdateSkipped: true,
				}, nil
			}
			return EnsureResult{}, updateErr
		}
		return EnsureResult{
			GPTID:   gptID,
			Updated: true,
		}, nil
	}

	if m.creator == nil {
		return EnsureResult{}, errors.New("gpt creator is nil")
	}

	result, err := m.creator.Create(ctx, request)
	if err != nil {
		return EnsureResult{}, err
	}
	if strings.TrimSpace(result.GPTID) == "" {
		return EnsureResult{}, errors.New("created gpt did not return gpt id")
	}

	cfg.SetGPTID(workspace, result.GPTID)
	if err := m.store.Save(cfg); err != nil {
		return EnsureResult{}, err
	}

	return EnsureResult{
		GPTID:   result.GPTID,
		Created: true,
	}, nil
}

func WorkspaceName(workspace string) string {
	cleaned := filepath.Clean(strings.TrimSpace(workspace))
	if cleaned == "." || cleaned == string(filepath.Separator) || cleaned == "" {
		return "workspace"
	}
	name := filepath.Base(cleaned)
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = strings.Trim(cleaned, string(filepath.Separator))
		name = strings.ReplaceAll(name, string(filepath.Separator), "-")
	}
	if name == "" {
		return "workspace"
	}
	return name
}

func GPTNameForWorkspace(workspace string) string {
	return fmt.Sprintf("Codex/%s", WorkspaceName(workspace))
}

func GPTDescriptionForWorkspace(workspace string) string {
	return fmt.Sprintf("Working In %s", WorkspaceName(workspace))
}

func (noopUpdater) Update(context.Context, UpdateRequest) error {
	return ErrUpdateNotImplemented
}
