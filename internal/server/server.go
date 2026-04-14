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

	docsasset "github.com/icattlecoder/chatgpt2codex/internal/docsasset"
	"github.com/icattlecoder/chatgpt2codex/internal/audit"
	"github.com/icattlecoder/chatgpt2codex/internal/runtimecontext"
	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

type Config struct {
	DefaultWorkspace string
	AuditLogger      *audit.Logger
	PublicBaseURL    func() string
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
	mux.HandleFunc("GET /api.yaml", makeAPISpecHandler(config))
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

func makeAPISpecHandler(config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conversationID := conversationIDFromHeader(r.Header.Get("Openai-Conversation-Id"))
		workspace, _ := resolveRequestWorkspace(config.DefaultWorkspace, r.Header.Get("X-Workspace"))
		body := []byte(buildAPISpec(config, r))

		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		appendAuditLog(config.AuditLogger, conversationID, "api_yaml", workspace, r, nil, http.StatusOK, body)
	}
}

func buildAPISpec(config Config, r *http.Request) string {
	baseURL := inferPublicBaseURL(r)
	if config.PublicBaseURL != nil {
		if configured := strings.TrimSpace(config.PublicBaseURL()); configured != "" {
			baseURL = normalizeBaseURL(configured)
		}
	}
	if baseURL == "" {
		return docsasset.ToolsAPISpec
	}
	return strings.Replace(docsasset.ToolsAPISpec, "http://127.0.0.1:8080", baseURL, 1)
}

func inferPublicBaseURL(r *http.Request) string {
	forwarded := strings.TrimSpace(r.Header.Get("Forwarded"))
	if forwarded != "" {
		proto, host := parseForwardedHeader(forwarded)
		if host != "" {
			if proto == "" {
				proto = "https"
			}
			return normalizeBaseURL(proto + "://" + host)
		}
	}

	proto := firstHeaderValue(r.Header.Get("X-Forwarded-Proto"))
	host := firstHeaderValue(r.Header.Get("X-Forwarded-Host"))
	if host != "" {
		if proto == "" {
			proto = "https"
		}
		return normalizeBaseURL(proto + "://" + host)
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto != "" {
		scheme = proto
	}
	if strings.TrimSpace(r.Host) == "" {
		return ""
	}
	return normalizeBaseURL(scheme + "://" + r.Host)
}

func parseForwardedHeader(value string) (string, string) {
	first := strings.TrimSpace(strings.Split(value, ",")[0])
	if first == "" {
		return "", ""
	}
	var proto string
	var host string
	for _, part := range strings.Split(first, ";") {
		key, rawValue, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "proto":
			proto = trimForwardedToken(rawValue)
		case "host":
			host = trimForwardedToken(rawValue)
		}
	}
	return proto, host
}

func trimForwardedToken(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}

func firstHeaderValue(value string) string {
	return strings.TrimSpace(strings.Split(value, ",")[0])
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
