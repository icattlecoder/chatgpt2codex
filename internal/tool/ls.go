package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LSRequest struct {
	Path  string `json:"path,omitempty"`
	Limit *int   `json:"limit,omitempty"`
}

type LSDetails struct {
	Truncation        *HeadTruncation `json:"truncation,omitempty"`
	EntryLimitReached int             `json:"entryLimitReached,omitempty"`
}

func RunLS(_ context.Context, workspace string, rawBody []byte) (Response, error) {
	var request LSRequest
	if err := decodeJSON(rawBody, &request, false); err != nil {
		return Response{}, err
	}
	effectiveLimit := DefaultLSLimit
	if request.Limit != nil {
		if *request.Limit < 1 {
			return Response{}, ValidationError("limit must be greater than or equal to 1")
		}
		effectiveLimit = *request.Limit
	}

	searchPath, err := ResolveToWorkspace(workspace, defaultPath(request.Path))
	if err != nil {
		return Response{}, err
	}
	info, err := os.Stat(searchPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Response{}, ValidationError("path not found: " + searchPath)
		}
		return Response{}, ExecutionError(err.Error())
	}
	if !info.IsDir() {
		return Response{}, ValidationError("not a directory: " + searchPath)
	}

	entries, err := os.ReadDir(searchPath)
	if err != nil {
		return Response{}, ExecutionError(err.Error())
	}
	sort.Slice(entries, func(left, right int) bool {
		return strings.ToLower(entries[left].Name()) < strings.ToLower(entries[right].Name())
	})

	results := make([]string, 0, len(entries))
	entryLimitReached := false
	for _, entry := range entries {
		if len(results) >= effectiveLimit {
			entryLimitReached = true
			break
		}
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		results = append(results, name)
	}

	if len(results) == 0 {
		return Response{Content: []ContentItem{{Type: "text", Text: "(empty directory)"}}}, nil
	}

	rawOutput := strings.Join(results, "\n")
	truncation := truncateHead(rawOutput, len(results), DefaultMaxBytes)
	output := truncation.Content
	details := LSDetails{}
	notices := make([]string, 0, 2)
	if entryLimitReached {
		details.EntryLimitReached = effectiveLimit
		notices = append(notices, fmt.Sprintf("%d entries limit reached. Use limit=%d for more", effectiveLimit, effectiveLimit*2))
	}
	if truncation.Truncated {
		details.Truncation = &truncation.HeadTruncation
		notices = append(notices, fmt.Sprintf("%d byte limit reached", DefaultMaxBytes))
	}
	if len(notices) > 0 {
		output += "\n\n[" + strings.Join(notices, ". ") + "]"
	}

	response := Response{Content: []ContentItem{{Type: "text", Text: output}}}
	if details.Truncation != nil || details.EntryLimitReached > 0 {
		response.Details = details
	}
	return response, nil
}

func defaultPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return "."
	}
	return path
}

func isSubPath(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(relative, "..")
}
