package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/icattlecoder/chatgpt2codex/internal/audit"
	"github.com/icattlecoder/chatgpt2codex/internal/runtimecontext"
	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

type Config struct {
	DefaultWorkspace string
	AuditLogger      *audit.Logger
}

type Executor func(context.Context, string, []byte) (tool.Response, error)

func NewHandler(config Config) http.Handler {
	executors := map[string]Executor{
		"read":  tool.RunRead,
		"bash":  tool.RunBash,
		"edit":  tool.RunEdit,
		"write": tool.RunWrite,
		"grep":  tool.RunGrep,
		"find":  tool.RunFind,
		"ls":    tool.RunLS,
	}

	mux := http.NewServeMux()
	for name, executor := range executors {
		mux.HandleFunc("POST /tools/"+name, makeToolHandler(config, name, executor))
	}
	mux.HandleFunc("POST /context/runtime", makeRuntimeContextHandler(config))
	return mux
}

func ListenFirstAvailable(startPort int) (net.Listener, error) {
	for port := startPort; port < startPort+tool.MaxPortScanAttempt; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return listener, nil
		}
	}
	return nil, fmt.Errorf("no available port found starting at %d", startPort)
}

func makeToolHandler(config Config, toolName string, executor Executor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeToolError(w, tool.ValidationError("failed to read request body"))
			return
		}

		workspace, err := resolveRequestWorkspace(config.DefaultWorkspace, r.Header.Get("X-Workspace"))
		conversationID := conversationIDFromHeader(r.Header.Get("Openai-Conversation-Id"))

		statusCode := http.StatusOK
		var response tool.Response
		if err == nil {
			response, err = executor(r.Context(), workspace, body)
		}
		if err != nil {
			statusCode = tool.StatusCode(err)
			if len(response.Content) == 0 && response.Details == nil {
				response = tool.Response{
					Content: []tool.ContentItem{
						{Type: "text", Text: err.Error()},
					},
				}
			}
		}

		responseBody, marshalErr := json.Marshal(response)
		if marshalErr != nil {
			writeToolError(w, tool.ExecutionError(marshalErr.Error()))
			return
		}

		writeJSONResponse(w, statusCode, responseBody)
		appendAuditLog(config.AuditLogger, conversationID, toolName, workspace, r, body, statusCode, responseBody)
	}
}

func makeRuntimeContextHandler(config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeToolError(w, tool.ValidationError("failed to read request body"))
			return
		}

		workspace, err := resolveRequestWorkspace(config.DefaultWorkspace, r.Header.Get("X-Workspace"))
		conversationID := conversationIDFromHeader(r.Header.Get("Openai-Conversation-Id"))

		statusCode := http.StatusOK
		responseBody, err := buildRuntimeContextResponseBody(workspace, body, err)
		if err != nil {
			statusCode = tool.StatusCode(err)
			responseBody, err = json.Marshal(tool.Response{
				Content: []tool.ContentItem{{Type: "text", Text: err.Error()}},
			})
			if err != nil {
				writeToolError(w, tool.ExecutionError(err.Error()))
				return
			}
		}

		writeJSONResponse(w, statusCode, responseBody)
		appendAuditLog(config.AuditLogger, conversationID, "get_runtime_context", workspace, r, body, statusCode, responseBody)
	}
}

func buildRuntimeContextResponseBody(workspace string, body []byte, priorErr error) ([]byte, error) {
	if priorErr != nil {
		return nil, priorErr
	}

	response, err := runtimecontext.Run(workspace, body)
	if err != nil {
		return nil, err
	}

	responseBody, err := json.Marshal(response)
	if err != nil {
		return nil, tool.ExecutionError(err.Error())
	}
	return responseBody, nil
}

func resolveRequestWorkspace(defaultWorkspace, override string) (string, error) {
	workspace, err := tool.ResolveWorkspace(defaultWorkspace, override)
	if err != nil {
		return "", err
	}

	info, statErr := os.Stat(workspace)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return workspace, tool.ValidationError("workspace not found: " + workspace)
		}
		return workspace, tool.ExecutionError(statErr.Error())
	}
	if !info.IsDir() {
		return workspace, tool.ValidationError("workspace is not a directory: " + workspace)
	}
	return workspace, nil
}

func writeToolError(w http.ResponseWriter, err error) {
	statusCode := tool.StatusCode(err)
	response := tool.Response{
		Content: []tool.ContentItem{
			{Type: "text", Text: err.Error()},
		},
	}
	body, _ := json.Marshal(response)
	writeJSONResponse(w, statusCode, body)
}

func writeJSONResponse(w http.ResponseWriter, statusCode int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

func appendAuditLog(logger *audit.Logger, conversationID, toolName, workspace string, r *http.Request, requestBody []byte, statusCode int, responseBody []byte) {
	if logger == nil {
		return
	}
	_ = logger.Append(
		conversationID,
		toolName,
		workspace,
		audit.RequestSnapshot{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: cloneHeaders(r.Header),
			Query:   r.URL.Query(),
			Body:    parseJSONForLog(requestBody),
		},
		audit.ResponseSnapshot{
			Status: statusCode,
			Body:   parseJSONForLog(responseBody),
		},
	)
}

func cloneHeaders(headers http.Header) map[string][]string {
	cloned := make(map[string][]string, len(headers))
	for key, values := range headers {
		copied := make([]string, len(values))
		copy(copied, values)
		cloned[key] = copied
	}
	return cloned
}

func parseJSONForLog(body []byte) any {
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(body, &value); err == nil {
		return value
	}
	return string(body)
}

func conversationIDFromHeader(raw string) string {
	conversationID := strings.TrimSpace(raw)
	if conversationID == "" {
		conversationID = generateConversationID()
	}
	return conversationID
}

func generateConversationID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "conversation"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
