package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/icattlecoder/chatgpt2codex/internal/audit"
	docsasset "github.com/icattlecoder/chatgpt2codex/internal/docsasset"
	"github.com/icattlecoder/chatgpt2codex/internal/runtimecontext"
	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

type Config struct {
	DefaultWorkspace string
	AuditLogger      *audit.Logger
	APIKey           string
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
		mux.HandleFunc("POST /tools/"+name, requireAPIKey(config.APIKey, makeToolHandler(config, executor)))
	}
	mux.HandleFunc("POST /context/runtime", requireAPIKey(config.APIKey, makeRuntimeContextHandler(config)))
	return withAuditLogging(config, mux)
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

func makeToolHandler(config Config, executor Executor) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeToolError(w, tool.ValidationError("failed to read request body"))
			return
		}

		workspace, err := resolveRequestWorkspace(config.DefaultWorkspace)

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
	}
}

func makeRuntimeContextHandler(config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeToolError(w, tool.ValidationError("failed to read request body"))
			return
		}

		workspace, err := resolveRequestWorkspace(config.DefaultWorkspace)

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
	}
}

func BuildAPISpec(publicBaseURL string) string {
	baseURL := normalizeBaseURL(publicBaseURL)
	if baseURL == "" {
		return docsasset.ToolsAPISpec
	}
	return strings.Replace(docsasset.ToolsAPISpec, "http://127.0.0.1:8080", baseURL, 1)
}

func normalizeBaseURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
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

func resolveRequestWorkspace(defaultWorkspace string) (string, error) {
	workspace, err := tool.ResolveWorkspace(defaultWorkspace, "")
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

func withAuditLogging(config Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			responseErr := tool.ValidationError("failed to read request body")
			responseBody := mustMarshalToolResponse(tool.Response{
				Content: []tool.ContentItem{{Type: "text", Text: responseErr.Error()}},
			})
			statusCode := tool.StatusCode(responseErr)
			writeJSONResponse(w, statusCode, responseBody)
			printRequestCallLog(config.DefaultWorkspace, auditToolName(r.URL.Path), r, nil, statusCode, time.Since(startedAt), err)
			appendAuditLog(
				config.AuditLogger,
				conversationIDFromHeader(r.Header.Get("Openai-Conversation-Id")),
				auditToolName(r.URL.Path),
				auditWorkspace(config.DefaultWorkspace),
				r,
				nil,
				statusCode,
				responseBody,
			)
			return
		}

		r.Body = io.NopCloser(bytes.NewReader(requestBody))
		recorder := newAuditResponseWriter(w)
		next.ServeHTTP(recorder, r)

		printRequestCallLog(config.DefaultWorkspace, auditToolName(r.URL.Path), r, requestBody, recorder.StatusCode(), time.Since(startedAt), nil)

		appendAuditLog(
			config.AuditLogger,
			conversationIDFromHeader(r.Header.Get("Openai-Conversation-Id")),
			auditToolName(r.URL.Path),
			auditWorkspace(config.DefaultWorkspace),
			r,
			requestBody,
			recorder.StatusCode(),
			recorder.Body(),
		)
	})
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

func auditToolName(path string) string {
	switch path {
	case "/context/runtime":
		return "get_runtime_context"
	case "/tools/read":
		return "read"
	case "/tools/bash":
		return "bash"
	case "/tools/edit":
		return "edit"
	case "/tools/write":
		return "write"
	case "/tools/grep":
		return "grep"
	case "/tools/find":
		return "find"
	case "/tools/ls":
		return "ls"
	default:
		return "http_access"
	}
}

func auditWorkspace(defaultWorkspace string) string {
	workspace, _ := resolveRequestWorkspace(defaultWorkspace)
	if strings.TrimSpace(workspace) != "" {
		return workspace
	}
	return defaultWorkspace
}

func cloneHeaders(headers http.Header) map[string][]string {
	cloned := make(map[string][]string, len(headers))
	for key, values := range headers {
		cloned[key] = sanitizeHeaderValues(key, values)
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

func mustMarshalToolResponse(response tool.Response) []byte {
	body, err := json.Marshal(response)
	if err != nil {
		return []byte(`{"content":[{"type":"text","text":"failed to marshal response"}]}`)
	}
	return body
}

type auditResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
	body        bytes.Buffer
}

func newAuditResponseWriter(w http.ResponseWriter) *auditResponseWriter {
	return &auditResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

func (w *auditResponseWriter) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *auditResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *auditResponseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_, _ = w.body.Write(body)
	return w.ResponseWriter.Write(body)
}

func (w *auditResponseWriter) StatusCode() int {
	return w.statusCode
}

func (w *auditResponseWriter) Body() []byte {
	return append([]byte(nil), w.body.Bytes()...)
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
