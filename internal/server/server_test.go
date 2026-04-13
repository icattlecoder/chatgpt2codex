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
	request.Header.Set("X-Conversation-Id", "conv-1")
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
