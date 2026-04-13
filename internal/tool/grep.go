package tool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type GrepRequest struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	IgnoreCase bool   `json:"ignoreCase,omitempty"`
	Literal    bool   `json:"literal,omitempty"`
	Context    *int   `json:"context,omitempty"`
	Limit      *int   `json:"limit,omitempty"`
}

type GrepDetails struct {
	Truncation        *HeadTruncation `json:"truncation,omitempty"`
	MatchLimitReached int             `json:"matchLimitReached,omitempty"`
	LinesTruncated    bool            `json:"linesTruncated,omitempty"`
}

type grepMatcher func(string) bool

type grepMatch struct {
	absPath    string
	relPath    string
	lineNumber int
}

func RunGrep(_ context.Context, workspace string, rawBody []byte) (Response, error) {
	var request GrepRequest
	if err := decodeJSON(rawBody, &request, false); err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(request.Pattern) == "" {
		return Response{}, ValidationError("pattern is required")
	}

	effectiveLimit := DefaultGrepLimit
	if request.Limit != nil {
		if *request.Limit < 1 {
			return Response{}, ValidationError("limit must be greater than or equal to 1")
		}
		effectiveLimit = *request.Limit
	}
	contextLines := 0
	if request.Context != nil {
		if *request.Context < 0 {
			return Response{}, ValidationError("context must be greater than or equal to 0")
		}
		contextLines = *request.Context
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

	matcher, err := compileGrepMatcher(request.Pattern, request.Literal, request.IgnoreCase)
	if err != nil {
		return Response{}, ValidationError(err.Error())
	}

	matches := make([]grepMatch, 0, effectiveLimit)
	matchLimitReached := false
	if info.IsDir() {
		err = walkWorkspace(searchPath, func(absPath, relPath string, entry fs.DirEntry) error {
			if entry.IsDir() {
				return nil
			}
			if request.Glob != "" && !matchesGlob(request.Glob, relPath) {
				return nil
			}
			found, err := grepFile(absPath, relPath, matcher, effectiveLimit, &matches)
			if err != nil {
				return err
			}
			if found {
				matchLimitReached = len(matches) >= effectiveLimit
			}
			if len(matches) >= effectiveLimit {
				return errStopWalking
			}
			return nil
		})
		if err != nil && !errors.Is(err, errStopWalking) {
			return Response{}, ExecutionError(err.Error())
		}
	} else {
		relPath := filepath.Base(searchPath)
		if request.Glob == "" || matchesGlob(request.Glob, relPath) {
			if _, err := grepFile(searchPath, relPath, matcher, effectiveLimit, &matches); err != nil {
				return Response{}, ExecutionError(err.Error())
			}
		}
	}

	if len(matches) == 0 {
		return Response{Content: []ContentItem{{Type: "text", Text: "No matches found"}}}, nil
	}

	outputLines := make([]string, 0, len(matches))
	linesTruncated := false
	for _, match := range matches {
		block, truncated, err := formatGrepBlock(match.absPath, match.relPath, match.lineNumber, contextLines, info.IsDir())
		if err != nil {
			return Response{}, ExecutionError(err.Error())
		}
		if truncated {
			linesTruncated = true
		}
		outputLines = append(outputLines, block...)
	}

	rawOutput := strings.Join(outputLines, "\n")
	truncation := truncateHead(rawOutput, len(outputLines), DefaultMaxBytes)
	output := truncation.Content
	details := GrepDetails{}
	notices := make([]string, 0, 3)
	if matchLimitReached || len(matches) >= effectiveLimit {
		details.MatchLimitReached = effectiveLimit
		notices = append(notices, fmt.Sprintf("%d matches limit reached. Use limit=%d for more, or refine pattern", effectiveLimit, effectiveLimit*2))
	}
	if truncation.Truncated {
		details.Truncation = &truncation.HeadTruncation
		notices = append(notices, fmt.Sprintf("%d byte limit reached", DefaultMaxBytes))
	}
	if linesTruncated {
		details.LinesTruncated = true
		notices = append(notices, fmt.Sprintf("Some lines truncated to %d chars. Use read tool to see full lines", GrepMaxLineLength))
	}
	if len(notices) > 0 {
		output += "\n\n[" + strings.Join(notices, ". ") + "]"
	}

	response := Response{Content: []ContentItem{{Type: "text", Text: output}}}
	if details.Truncation != nil || details.MatchLimitReached > 0 || details.LinesTruncated {
		response.Details = details
	}
	return response, nil
}

func compileGrepMatcher(pattern string, literal, ignoreCase bool) (grepMatcher, error) {
	if literal {
		if ignoreCase {
			pattern = strings.ToLower(pattern)
			return func(value string) bool {
				return strings.Contains(strings.ToLower(value), pattern)
			}, nil
		}
		return func(value string) bool {
			return strings.Contains(value, pattern)
		}, nil
	}
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return regex.MatchString, nil
}

func grepFile(absPath, relPath string, matcher grepMatcher, limit int, matches *[]grepMatch) (bool, error) {
	if len(*matches) >= limit {
		return true, nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return false, err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false, nil
	}
	lines := strings.Split(normalizeLineEndings(string(data)), "\n")
	found := false
	for index, line := range lines {
		if matcher(line) {
			*matches = append(*matches, grepMatch{
				absPath:    absPath,
				relPath:    relPath,
				lineNumber: index + 1,
			})
			found = true
			if len(*matches) >= limit {
				return true, nil
			}
		}
	}
	return found, nil
}

func formatGrepBlock(absPath, relPath string, lineNumber, contextLines int, searchedDirectory bool) ([]string, bool, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, false, err
	}
	lines := strings.Split(normalizeLineEndings(string(data)), "\n")
	start := lineNumber
	end := lineNumber
	if contextLines > 0 {
		start = lineNumber - contextLines
		if start < 1 {
			start = 1
		}
		end = lineNumber + contextLines
		if end > len(lines) {
			end = len(lines)
		}
	}

	displayPath := relPath
	if !searchedDirectory {
		displayPath = filepath.Base(absPath)
	}
	block := make([]string, 0, end-start+1)
	linesTruncated := false
	for current := start; current <= end; current++ {
		text, truncated := truncateLine(lines[current-1])
		linesTruncated = linesTruncated || truncated
		if current == lineNumber {
			block = append(block, fmt.Sprintf("%s:%d: %s", displayPath, current, text))
		} else {
			block = append(block, fmt.Sprintf("%s-%d- %s", displayPath, current, text))
		}
	}
	return block, linesTruncated, nil
}
