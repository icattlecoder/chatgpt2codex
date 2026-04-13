package tool

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type ReadRequest struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
}

type ReadDetails struct {
	Truncation *HeadTruncation `json:"truncation,omitempty"`
}

func RunRead(_ context.Context, workspace string, rawBody []byte) (Response, error) {
	var request ReadRequest
	if err := decodeJSON(rawBody, &request, false); err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(request.Path) == "" {
		return Response{}, ValidationError("path is required")
	}
	if request.Offset != nil && *request.Offset < 1 {
		return Response{}, ValidationError("offset must be greater than or equal to 1")
	}
	if request.Limit != nil && *request.Limit < 1 {
		return Response{}, ValidationError("limit must be greater than or equal to 1")
	}

	absolutePath, err := ResolveToWorkspace(workspace, request.Path)
	if err != nil {
		return Response{}, err
	}
	info, err := os.Stat(absolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			return Response{}, ValidationError("file not found: " + request.Path)
		}
		return Response{}, ExecutionError(err.Error())
	}
	if info.IsDir() {
		return Response{}, ValidationError("path is a directory: " + request.Path)
	}

	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return Response{}, ExecutionError(err.Error())
	}
	if mimeType, ok := detectSupportedImageMimeType(absolutePath, data); ok {
		return Response{
			Content: []ContentItem{
				{Type: "text", Text: fmt.Sprintf("Read image file [%s]", mimeType)},
				{Type: "image", Data: base64.StdEncoding.EncodeToString(data), MimeType: mimeType},
			},
		}, nil
	}

	textContent := string(data)
	allLines := strings.Split(textContent, "\n")
	startLine := 0
	if request.Offset != nil {
		startLine = *request.Offset - 1
	}
	if startLine >= len(allLines) {
		return Response{}, ValidationError(fmt.Sprintf("offset %d is beyond end of file (%d lines total)", *request.Offset, len(allLines)))
	}

	selectedContent := strings.Join(allLines[startLine:], "\n")
	userLimitedLines := 0
	if request.Limit != nil {
		endLine := startLine + *request.Limit
		if endLine > len(allLines) {
			endLine = len(allLines)
		}
		selectedContent = strings.Join(allLines[startLine:endLine], "\n")
		userLimitedLines = endLine - startLine
	}

	truncation := truncateHead(selectedContent, DefaultMaxLines, DefaultMaxBytes)
	startLineDisplay := startLine + 1
	details := ReadDetails{}
	var output string
	switch {
	case truncation.FirstLineExceedsLimit:
		firstLineSize := len([]byte(allLines[startLine]))
		output = fmt.Sprintf("[Line %d is %d bytes, exceeds %d byte limit. Use bash: sed -n '%dp' %s | head -c %d]", startLineDisplay, firstLineSize, DefaultMaxBytes, startLineDisplay, request.Path, DefaultMaxBytes)
		details.Truncation = &truncation.HeadTruncation
	case truncation.Truncated:
		endLineDisplay := startLineDisplay + truncation.OutputLines - 1
		nextOffset := endLineDisplay + 1
		output = truncation.Content
		if truncation.TruncatedBy == "lines" {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", startLineDisplay, endLineDisplay, len(allLines), nextOffset)
		} else {
			output += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%d byte limit). Use offset=%d to continue.]", startLineDisplay, endLineDisplay, len(allLines), DefaultMaxBytes, nextOffset)
		}
		details.Truncation = &truncation.HeadTruncation
	case request.Limit != nil && startLine+userLimitedLines < len(allLines):
		nextOffset := startLine + userLimitedLines + 1
		remaining := len(allLines) - (startLine + userLimitedLines)
		output = truncation.Content + fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", remaining, nextOffset)
	default:
		output = truncation.Content
	}

	response := Response{
		Content: []ContentItem{{Type: "text", Text: output}},
	}
	if details.Truncation != nil {
		response.Details = details
	}
	return response, nil
}

func detectSupportedImageMimeType(path string, data []byte) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".png":
		return "image/png", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	}
	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return mimeType, true
	default:
		return "", false
	}
}
