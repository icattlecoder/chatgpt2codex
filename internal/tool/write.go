package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type WriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func RunWrite(_ context.Context, workspace string, rawBody []byte) (Response, error) {
	var request WriteRequest
	if err := decodeJSON(rawBody, &request, false); err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(request.Path) == "" {
		return Response{}, ValidationError("path is required")
	}

	absolutePath, err := ResolveToWorkspace(workspace, request.Path)
	if err != nil {
		return Response{}, err
	}

	err = withFileLock(absolutePath, func() error {
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(absolutePath, []byte(request.Content), 0o644)
	})
	if err != nil {
		return Response{}, ExecutionError(err.Error())
	}

	return Response{
		Content: []ContentItem{
			{Type: "text", Text: fmt.Sprintf("Successfully wrote %d bytes to %s", len([]byte(request.Content)), request.Path)},
		},
	}, nil
}
