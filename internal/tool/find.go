package tool

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
)

type FindRequest struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Limit   *int   `json:"limit,omitempty"`
}

type FindDetails struct {
	Truncation         *HeadTruncation `json:"truncation,omitempty"`
	ResultLimitReached int             `json:"resultLimitReached,omitempty"`
}

func RunFind(_ context.Context, workspace string, rawBody []byte) (Response, error) {
	var request FindRequest
	if err := decodeJSON(rawBody, &request, false); err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(request.Pattern) == "" {
		return Response{}, ValidationError("pattern is required")
	}
	effectiveLimit := DefaultFindLimit
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

	results := make([]string, 0, effectiveLimit)
	resultLimitReached := false
	err = walkWorkspace(searchPath, func(_ string, relPath string, entry fs.DirEntry) error {
		if len(results) >= effectiveLimit {
			resultLimitReached = true
			return errStopWalking
		}
		if matchesGlob(request.Pattern, relPath) {
			if entry.IsDir() {
				results = append(results, relPath+"/")
			} else {
				results = append(results, relPath)
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopWalking) {
		return Response{}, ExecutionError(err.Error())
	}

	if len(results) == 0 {
		return Response{Content: []ContentItem{{Type: "text", Text: "No files found matching pattern"}}}, nil
	}

	sort.Strings(results)
	rawOutput := strings.Join(results, "\n")
	truncation := truncateHead(rawOutput, len(results), DefaultMaxBytes)
	output := truncation.Content
	details := FindDetails{}
	notices := make([]string, 0, 2)
	if resultLimitReached || len(results) >= effectiveLimit {
		details.ResultLimitReached = effectiveLimit
		notices = append(notices, fmt.Sprintf("%d results limit reached. Use limit=%d for more, or refine pattern", effectiveLimit, effectiveLimit*2))
	}
	if truncation.Truncated {
		details.Truncation = &truncation.HeadTruncation
		notices = append(notices, fmt.Sprintf("%d byte limit reached", DefaultMaxBytes))
	}
	if len(notices) > 0 {
		output += "\n\n[" + strings.Join(notices, ". ") + "]"
	}

	response := Response{Content: []ContentItem{{Type: "text", Text: output}}}
	if details.Truncation != nil || details.ResultLimitReached > 0 {
		response.Details = details
	}
	return response, nil
}

func matchesGlob(pattern, value string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "./")
	if matchPattern(pattern, value) {
		return true
	}
	if !strings.Contains(pattern, "/") {
		for _, segment := range strings.Split(value, "/") {
			if matchPattern(pattern, segment) {
				return true
			}
		}
	}
	return false
}
