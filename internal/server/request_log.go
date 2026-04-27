package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/icattlecoder/chatgpt2codex/internal/runtimecontext"
	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

const requestLogValueLimit = 160

func printRequestCallLog(defaultWorkspace, toolName string, r *http.Request, requestBody []byte, statusCode int, elapsed time.Duration, readErr error) {
	fields := []string{
		logStringField("tool", toolName),
		fmt.Sprintf("status=%d", statusCode),
		fmt.Sprintf("duration=%s", elapsed.Round(time.Millisecond)),
		logStringField("workspace", auditWorkspace(defaultWorkspace)),
	}
	if readErr != nil {
		fields = append(fields, logStringField("error", readErr.Error()))
	} else {
		fields = append(fields, requestLogSummary(toolName, r.URL.Path, requestBody)...)
	}
	log.Printf("tool_call %s", strings.Join(fields, " "))
}

func requestLogSummary(toolName, path string, requestBody []byte) []string {
	if len(strings.TrimSpace(string(requestBody))) == 0 {
		return []string{logStringField("body", "empty")}
	}

	switch toolName {
	case "read":
		var request tool.ReadRequest
		if err := json.Unmarshal(requestBody, &request); err != nil {
			return invalidRequestLogSummary(path)
		}
		fields := []string{logStringField("path", request.Path)}
		appendOptionalIntField(&fields, "offset", request.Offset)
		appendOptionalIntField(&fields, "limit", request.Limit)
		return fields
	case "bash":
		var request tool.BashRequest
		if err := json.Unmarshal(requestBody, &request); err != nil {
			return invalidRequestLogSummary(path)
		}
		fields := []string{logStringField("command", request.Command)}
		appendOptionalIntField(&fields, "timeout", request.Timeout)
		return fields
	case "edit":
		var request tool.EditRequest
		if err := json.Unmarshal(requestBody, &request); err != nil {
			return invalidRequestLogSummary(path)
		}
		return []string{
			logStringField("path", request.Path),
			fmt.Sprintf("edits=%d", len(request.Edits)),
		}
	case "write":
		var request tool.WriteRequest
		if err := json.Unmarshal(requestBody, &request); err != nil {
			return invalidRequestLogSummary(path)
		}
		return []string{
			logStringField("path", request.Path),
			fmt.Sprintf("bytes=%d", len([]byte(request.Content))),
		}
	case "grep":
		var request tool.GrepRequest
		if err := json.Unmarshal(requestBody, &request); err != nil {
			return invalidRequestLogSummary(path)
		}
		fields := []string{
			logStringField("pattern", request.Pattern),
			logStringField("path", defaultPathForLog(request.Path)),
		}
		if strings.TrimSpace(request.Glob) != "" {
			fields = append(fields, logStringField("glob", request.Glob))
		}
		if request.IgnoreCase {
			fields = append(fields, "ignoreCase=true")
		}
		if request.Literal {
			fields = append(fields, "literal=true")
		}
		appendOptionalIntField(&fields, "context", request.Context)
		appendOptionalIntField(&fields, "limit", request.Limit)
		return fields
	case "find":
		var request tool.FindRequest
		if err := json.Unmarshal(requestBody, &request); err != nil {
			return invalidRequestLogSummary(path)
		}
		fields := []string{
			logStringField("pattern", request.Pattern),
			logStringField("path", defaultPathForLog(request.Path)),
		}
		appendOptionalIntField(&fields, "limit", request.Limit)
		return fields
	case "ls":
		var request tool.LSRequest
		if err := json.Unmarshal(requestBody, &request); err != nil {
			return invalidRequestLogSummary(path)
		}
		fields := []string{logStringField("path", defaultPathForLog(request.Path))}
		appendOptionalIntField(&fields, "limit", request.Limit)
		return fields
	case "get_runtime_context":
		var request runtimecontext.Request
		if err := json.Unmarshal(requestBody, &request); err != nil {
			return invalidRequestLogSummary(path)
		}
		return []string{logStringField("cwd", request.CWD)}
	default:
		return []string{logStringField("path", path)}
	}
}

func invalidRequestLogSummary(path string) []string {
	return []string{
		logStringField("path", path),
		logStringField("body", "invalid_json"),
	}
}

func defaultPathForLog(path string) string {
	if strings.TrimSpace(path) == "" {
		return "."
	}
	return path
}

func appendOptionalIntField(fields *[]string, key string, value *int) {
	if value == nil {
		return
	}
	*fields = append(*fields, fmt.Sprintf("%s=%d", key, *value))
}

func logStringField(key, value string) string {
	return fmt.Sprintf("%s=%q", key, compactLogValue(value))
}

func compactLogValue(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= requestLogValueLimit {
		return value
	}
	runes := []rune(value)
	return string(runes[:requestLogValueLimit-3]) + "..."
}
