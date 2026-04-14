package server

import (
	"encoding/json"
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

func TestHandlerUsesWorkspaceHeaderAndWritesAuditLog(t *testing.T) {
	defaultWorkspace := t.TempDir()
	overrideWorkspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(overrideWorkspace, "note.txt"), []byte("hello\nworld"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	logDir := t.TempDir()
	logger, err := audit.NewLogger(logDir)
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}

	handler := NewHandler(Config{DefaultWorkspace: defaultWorkspace, AuditLogger: logger})
	request := httptest.NewRequest(http.MethodPost, "/tools/read", strings.NewReader(`{"path":"note.txt"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Workspace", overrideWorkspace)
	request.Header.Set("Openai-Conversation-Id", "conv-1")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "hello") {
		t.Fatalf("expected read response to include file contents")
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
	var record struct {
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("invalid log record: %v", err)
	}
	if record.Workspace != overrideWorkspace {
		t.Fatalf("expected workspace %q, got %q", overrideWorkspace, record.Workspace)
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
