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

		workspace, err := tool.ResolveWorkspace(config.DefaultWorkspace, r.Header.Get("X-Workspace"))
		if err == nil {
			info, statErr := os.Stat(workspace)
			if statErr != nil {
				if os.IsNotExist(statErr) {
					err = tool.ValidationError("workspace not found: " + workspace)
				} else {
					err = tool.ExecutionError(statErr.Error())
				}
			} else if !info.IsDir() {
				err = tool.ValidationError("workspace is not a directory: " + workspace)
			}
		}

		conversationID := strings.TrimSpace(r.Header.Get("X-Conversation-Id"))
		if conversationID == "" {
			conversationID = generateConversationID()
		}

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

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write(responseBody)

		if config.AuditLogger != nil {
			_ = config.AuditLogger.Append(
				conversationID,
				toolName,
				workspace,
				audit.RequestSnapshot{
					Method:  r.Method,
					Path:    r.URL.Path,
					Headers: cloneHeaders(r.Header),
					Query:   r.URL.Query(),
					Body:    parseJSONForLog(body),
				},
				audit.ResponseSnapshot{
					Status: statusCode,
					Body:   parseJSONForLog(responseBody),
				},
			)
		}
	}
}

func writeToolError(w http.ResponseWriter, err error) {
	statusCode := tool.StatusCode(err)
	response := tool.Response{
		Content: []tool.ContentItem{
			{Type: "text", Text: err.Error()},
		},
	}
	body, _ := json.Marshal(response)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
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

func generateConversationID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "conversation"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}
