package server

import (
	"bytes"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/icattlecoder/chatgpt2codex/internal/audit"
)

func TestHandlerUsesConfiguredWorkspaceAndWritesAuditLog(t *testing.T) {
	defaultWorkspace := t.TempDir()
	overrideWorkspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(defaultWorkspace, "note.txt"), []byte("default\nworkspace"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overrideWorkspace, "note.txt"), []byte("override\nworkspace"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	logDir := t.TempDir()
	logger, err := audit.NewLogger(logDir)
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}

	handler := NewHandler(Config{DefaultWorkspace: defaultWorkspace, AuditLogger: logger, APIKey: "ctc_secret"})
	request := httptest.NewRequest(http.MethodPost, "/tools/read", strings.NewReader(`{"path":"note.txt"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer ctc_secret")
	request.Header.Set("X-Workspace", overrideWorkspace)
	request.Header.Set("Openai-Conversation-Id", "conv-1")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "default") {
		t.Fatalf("expected read response to use configured workspace, got %q", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "override") {
		t.Fatalf("did not expect X-Workspace override to be honored, got %q", recorder.Body.String())
	}

	logPath := filepath.Join(logDir, time.Now().Format("2006"), time.Now().Format("01"), time.Now().Format("02"), "conv-1.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(lines))
	}
	if !strings.Contains(string(data), "Bearer ctc_secret") {
		t.Fatalf("expected authorization header to be logged in plaintext, got %q", string(data))
	}
	if strings.Contains(string(data), "[REDACTED]") {
		t.Fatalf("did not expect authorization header to be redacted in audit log, got %q", string(data))
	}
	var record struct {
		Workspace string `json:"workspace"`
		Request   struct {
			Body map[string]any `json:"body"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("invalid log record: %v", err)
	}
	if record.Workspace != defaultWorkspace {
		t.Fatalf("expected workspace %q, got %q", defaultWorkspace, record.Workspace)
	}
	if record.Request.Body["path"] != "note.txt" {
		t.Fatalf("expected request body to be audited, got %#v", record.Request.Body)
	}
}

func TestRuntimeContextHandlerReturnsPlainJSONAndWritesAuditLog(t *testing.T) {
	workspace := t.TempDir()
	cwd := filepath.Join(workspace, "pkg", "feature")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("# Repo\nFollow AGENTS."), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	skillPath := filepath.Join(workspace, ".agents", "skills", "release-check", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(skillPath, []byte("# release-check\n\nValidate release pipeline and packaging steps"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	logDir := t.TempDir()
	logger, err := audit.NewLogger(logDir)
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}

	handler := NewHandler(Config{DefaultWorkspace: workspace, AuditLogger: logger})
	request := httptest.NewRequest(http.MethodPost, "/context/runtime", strings.NewReader(`{"cwd":"pkg/feature"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Openai-Conversation-Id", "conv-runtime")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		SystemInstruct string `json:"system_instruct"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("invalid runtime context response: %v", err)
	}
	if !strings.Contains(response.SystemInstruct, "Validate release pipeline and packaging steps") {
		t.Fatalf("expected runtime context response to include skill description, got %q", response.SystemInstruct)
	}
	if !strings.Contains(response.SystemInstruct, filepath.ToSlash(cwd)) && !strings.Contains(response.SystemInstruct, skillPath) {
		t.Fatalf("expected runtime context response to include resolved paths, got %q", response.SystemInstruct)
	}

	logPath := filepath.Join(logDir, time.Now().Format("2006"), time.Now().Format("01"), time.Now().Format("02"), "conv-runtime.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(lines))
	}
	var record struct {
		Tool     string `json:"tool"`
		Response struct {
			Body struct {
				SystemInstruct string `json:"system_instruct"`
			} `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("invalid log record: %v", err)
	}
	if record.Tool != "get_runtime_context" {
		t.Fatalf("expected audit tool name get_runtime_context, got %q", record.Tool)
	}
	if !strings.Contains(record.Response.Body.SystemInstruct, "Follow AGENTS") {
		t.Fatalf("expected audit response body to include system instruct, got %q", record.Response.Body.SystemInstruct)
	}
}

func TestRequestLogSummaryUsesToolSpecificFields(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		path     string
		body     string
		want     []string
		notWant  []string
	}{
		{
			name:     "read",
			toolName: "read",
			path:     "/tools/read",
			body:     `{"path":"README.md","offset":3,"limit":5}`,
			want:     []string{`path="README.md"`, "offset=3", "limit=5"},
		},
		{
			name:     "bash",
			toolName: "bash",
			path:     "/tools/bash",
			body:     `{"command":"go test ./...","timeout":30}`,
			want:     []string{`command="go test ./..."`, "timeout=30"},
		},
		{
			name:     "edit",
			toolName: "edit",
			path:     "/tools/edit",
			body:     `{"path":"main.go","edits":[{"oldText":"secret old","newText":"secret new"},{"oldText":"old 2","newText":"new 2"}]}`,
			want:     []string{`path="main.go"`, "edits=2"},
			notWant:  []string{"secret old", "secret new"},
		},
		{
			name:     "write",
			toolName: "write",
			path:     "/tools/write",
			body:     `{"path":"notes.txt","content":"secret file body"}`,
			want:     []string{`path="notes.txt"`, "bytes=16"},
			notWant:  []string{"secret file body"},
		},
		{
			name:     "grep",
			toolName: "grep",
			path:     "/tools/grep",
			body:     `{"pattern":"TODO","path":"internal","glob":"*.go","ignoreCase":true,"literal":true,"context":2,"limit":9}`,
			want:     []string{`pattern="TODO"`, `path="internal"`, `glob="*.go"`, "ignoreCase=true", "literal=true", "context=2", "limit=9"},
		},
		{
			name:     "find",
			toolName: "find",
			path:     "/tools/find",
			body:     `{"pattern":"*.go","path":"internal","limit":7}`,
			want:     []string{`pattern="*.go"`, `path="internal"`, "limit=7"},
		},
		{
			name:     "ls",
			toolName: "ls",
			path:     "/tools/ls",
			body:     `{"path":"internal","limit":4}`,
			want:     []string{`path="internal"`, "limit=4"},
		},
		{
			name:     "get_runtime_context",
			toolName: "get_runtime_context",
			path:     "/context/runtime",
			body:     `{"cwd":"internal/server"}`,
			want:     []string{`cwd="internal/server"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(requestLogSummary(tc.toolName, tc.path, []byte(tc.body)), " ")
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("expected %q to contain %q", got, want)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(got, notWant) {
					t.Fatalf("expected %q not to contain %q", got, notWant)
				}
			}
		})
	}
}

func TestHandlerPrintsConciseToolRequestLog(t *testing.T) {
	var logBuffer bytes.Buffer
	originalOutput := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logBuffer)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(originalOutput)
		log.SetFlags(originalFlags)
	}()

	workspace := t.TempDir()
	handler := NewHandler(Config{DefaultWorkspace: workspace})
	request := httptest.NewRequest(http.MethodPost, "/tools/write", strings.NewReader(`{"path":"notes.txt","content":"secret file body"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	got := logBuffer.String()
	for _, want := range []string{`tool_call`, `tool="write"`, "status=200", `path="notes.txt"`, "bytes=16"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected log %q to contain %q", got, want)
		}
	}
	if strings.Contains(got, "secret file body") {
		t.Fatalf("expected write content to be omitted from request log, got %q", got)
	}
}

func TestBuildAPISpecUsesConfiguredPublicURL(t *testing.T) {
	body := BuildAPISpec("https://demo.trycloudflare.com")
	if !strings.Contains(body, "https://demo.trycloudflare.com") {
		t.Fatalf("expected api spec to include public URL, got %q", body)
	}
	if strings.Contains(body, "http://127.0.0.1:8080") {
		t.Fatalf("expected default local URL to be replaced, got %q", body)
	}
}

func TestBuildAPISpecFallsBackToEmbeddedSpecWhenEmpty(t *testing.T) {
	body := BuildAPISpec("")
	if !strings.Contains(body, "http://127.0.0.1:8080") {
		t.Fatalf("expected embedded local URL, got %q", body)
	}
}

func TestHandlerRejectsMissingAPIKey(t *testing.T) {
	logDir := t.TempDir()
	logger, err := audit.NewLogger(logDir)
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}

	handler := NewHandler(Config{DefaultWorkspace: t.TempDir(), APIKey: "ctc_secret", AuditLogger: logger})
	request := httptest.NewRequest(http.MethodPost, "/tools/ls", strings.NewReader(`{"path":"."}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Openai-Conversation-Id", "conv-unauthorized")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("WWW-Authenticate"); !strings.Contains(got, "Bearer") {
		t.Fatalf("expected bearer challenge header, got %q", got)
	}
	if !strings.Contains(recorder.Body.String(), "missing or invalid bearer api key") {
		t.Fatalf("expected unauthorized body, got %q", recorder.Body.String())
	}

	logPath := filepath.Join(logDir, time.Now().Format("2006"), time.Now().Format("01"), time.Now().Format("02"), "conv-unauthorized.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(lines))
	}
	var record struct {
		Tool    string `json:"tool"`
		Request struct {
			Body map[string]any `json:"body"`
		} `json:"request"`
		Response struct {
			Status int `json:"status"`
			Body   struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("invalid log record: %v", err)
	}
	if record.Tool != "ls" {
		t.Fatalf("expected tool ls, got %q", record.Tool)
	}
	if record.Response.Status != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, record.Response.Status)
	}
	if record.Request.Body["path"] != "." {
		t.Fatalf("expected audited request body, got %#v", record.Request.Body)
	}
	if len(record.Response.Body.Content) == 0 || !strings.Contains(record.Response.Body.Content[0].Text, "invalid bearer") {
		t.Fatalf("expected audited unauthorized response body, got %#v", record.Response.Body)
	}
}

func TestHandlerAuditsNotFoundRequests(t *testing.T) {
	logDir := t.TempDir()
	logger, err := audit.NewLogger(logDir)
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}

	handler := NewHandler(Config{DefaultWorkspace: t.TempDir(), APIKey: "ctc_secret", AuditLogger: logger})
	request := httptest.NewRequest(http.MethodPost, "/missing", strings.NewReader(`{"foo":"bar"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Openai-Conversation-Id", "conv-404")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	logPath := filepath.Join(logDir, time.Now().Format("2006"), time.Now().Format("01"), time.Now().Format("02"), "conv-404.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d", len(lines))
	}
	var record struct {
		Tool    string `json:"tool"`
		Request struct {
			Path string         `json:"path"`
			Body map[string]any `json:"body"`
		} `json:"request"`
		Response struct {
			Status int    `json:"status"`
			Body   string `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("invalid log record: %v", err)
	}
	if record.Tool != "http_access" {
		t.Fatalf("expected tool http_access, got %q", record.Tool)
	}
	if record.Request.Path != "/missing" {
		t.Fatalf("expected path /missing, got %q", record.Request.Path)
	}
	if record.Request.Body["foo"] != "bar" {
		t.Fatalf("expected request body to be audited, got %#v", record.Request.Body)
	}
	if record.Response.Status != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, record.Response.Status)
	}
	if !strings.Contains(record.Response.Body, "404 page not found") {
		t.Fatalf("expected 404 body to be audited, got %q", record.Response.Body)
	}
}

func TestAPISpecIncludesBearerSecurityScheme(t *testing.T) {
	body := BuildAPISpec("")
	if !strings.Contains(body, "bearerAuth") {
		t.Fatalf("expected api spec to include bearer auth scheme, got %q", body)
	}
	if !strings.Contains(body, "scheme: bearer") {
		t.Fatalf("expected api spec to declare bearer scheme, got %q", body)
	}
	if !strings.Contains(body, "'401':") {
		t.Fatalf("expected api spec to include 401 responses, got %q", body)
	}
}

func TestListenFirstAvailableSkipsBusyPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen returned error: %v", err)
	}
	defer busy.Close()

	startPort := busy.Addr().(*net.TCPAddr).Port
	listener, err := ListenFirstAvailable(startPort)
	if err != nil {
		t.Fatalf("ListenFirstAvailable returned error: %v", err)
	}
	defer listener.Close()

	if listener.Addr().(*net.TCPAddr).Port == startPort {
		t.Fatalf("expected a different port than the busy port")
	}
}
