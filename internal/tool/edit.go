package tool

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

type EditRequest struct {
	Path  string          `json:"path"`
	Edits []EditOperation `json:"edits"`
}

type EditOperation struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

type EditDetails struct {
	Diff             string `json:"diff"`
	FirstChangedLine int    `json:"firstChangedLine,omitempty"`
}

type matchedEdit struct {
	start   int
	end     int
	newText string
}

func RunEdit(_ context.Context, workspace string, rawBody []byte) (Response, error) {
	var request EditRequest
	if err := decodeJSON(rawBody, &request, true); err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(request.Path) == "" {
		return Response{}, ValidationError("path is required")
	}
	if len(request.Edits) == 0 {
		return Response{}, ValidationError("edits must contain at least one replacement")
	}

	absolutePath, err := ResolveToWorkspace(workspace, request.Path)
	if err != nil {
		return Response{}, err
	}

	var response Response
	err = withFileLock(absolutePath, func() error {
		originalBytes, err := os.ReadFile(absolutePath)
		if err != nil {
			if os.IsNotExist(err) {
				return ValidationError("file not found: " + request.Path)
			}
			return err
		}

		bom := []byte{}
		workingBytes := originalBytes
		if bytes.HasPrefix(originalBytes, []byte{0xEF, 0xBB, 0xBF}) {
			bom = originalBytes[:3]
			workingBytes = originalBytes[3:]
		}

		originalText := string(workingBytes)
		lineEnding := detectLineEnding(originalText)
		normalizedOriginal := normalizeLineEndings(originalText)

		matches := make([]matchedEdit, 0, len(request.Edits))
		for _, edit := range request.Edits {
			if edit.OldText == "" {
				return ValidationError("edits[].oldText must not be empty")
			}
			normalizedOld := normalizeLineEndings(edit.OldText)
			normalizedNew := normalizeLineEndings(edit.NewText)
			start, end, err := findUniqueMatch(normalizedOriginal, normalizedOld)
			if err != nil {
				return err
			}
			matches = append(matches, matchedEdit{
				start:   start,
				end:     end,
				newText: normalizedNew,
			})
		}

		sort.Slice(matches, func(left, right int) bool {
			return matches[left].start < matches[right].start
		})
		for index := 1; index < len(matches); index++ {
			if matches[index].start < matches[index-1].end {
				return ValidationError("edits contain overlapping oldText matches")
			}
		}

		var builder strings.Builder
		lastIndex := 0
		for _, match := range matches {
			builder.WriteString(normalizedOriginal[lastIndex:match.start])
			builder.WriteString(match.newText)
			lastIndex = match.end
		}
		builder.WriteString(normalizedOriginal[lastIndex:])
		normalizedNewContent := builder.String()

		restored := restoreLineEndings(normalizedNewContent, lineEnding)
		finalBytes := append(bom, []byte(restored)...)
		if err := os.WriteFile(absolutePath, finalBytes, 0o644); err != nil {
			return err
		}

		diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A:        difflib.SplitLines(normalizedOriginal),
			B:        difflib.SplitLines(normalizedNewContent),
			FromFile: request.Path,
			ToFile:   request.Path,
			Context:  3,
		})
		if err != nil {
			return err
		}

		response = Response{
			Content: []ContentItem{
				{Type: "text", Text: fmt.Sprintf("Successfully replaced text in %s.", request.Path)},
			},
			Details: EditDetails{
				Diff:             diff,
				FirstChangedLine: firstChangedLine(normalizedOriginal, normalizedNewContent),
			},
		}
		return nil
	})
	if err != nil {
		if toolErr, ok := err.(*Error); ok {
			return Response{}, toolErr
		}
		return Response{}, ExecutionError(err.Error())
	}

	return response, nil
}

func normalizeLineEndings(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

func detectLineEnding(text string) string {
	switch {
	case strings.Contains(text, "\r\n"):
		return "\r\n"
	case strings.Contains(text, "\r"):
		return "\r"
	default:
		return "\n"
	}
}

func restoreLineEndings(text, lineEnding string) string {
	if lineEnding == "\n" {
		return text
	}
	return strings.ReplaceAll(text, "\n", lineEnding)
}

func findUniqueMatch(content, oldText string) (int, int, error) {
	start := strings.Index(content, oldText)
	if start < 0 {
		return 0, 0, ValidationError("oldText does not match the file exactly")
	}
	if second := strings.Index(content[start+1:], oldText); second >= 0 {
		return 0, 0, ValidationError("oldText must match a unique region in the file")
	}
	return start, start + len(oldText), nil
}

func firstChangedLine(original, updated string) int {
	originalLines := strings.Split(original, "\n")
	updatedLines := strings.Split(updated, "\n")
	maxLines := len(originalLines)
	if len(updatedLines) > maxLines {
		maxLines = len(updatedLines)
	}
	for index := 0; index < maxLines; index++ {
		var originalLine, updatedLine string
		if index < len(originalLines) {
			originalLine = originalLines[index]
		}
		if index < len(updatedLines) {
			updatedLine = updatedLines[index]
		}
		if originalLine != updatedLine {
			return index + 1
		}
	}
	return 0
}
