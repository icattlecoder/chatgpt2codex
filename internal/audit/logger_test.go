package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoggerAppendsJSONLLines(t *testing.T) {
	tempDir := t.TempDir()
	logger, err := NewLogger(tempDir)
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}

	const entries = 10
	var wait sync.WaitGroup
	for index := 0; index < entries; index++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			if err := logger.Append(
				"conv-1",
				"read",
				"/tmp/workspace",
				RequestSnapshot{Method: "POST", Path: "/tools/read", Headers: map[string][]string{}, Query: map[string][]string{}, Body: map[string]any{"path": "a.txt"}},
				ResponseSnapshot{Status: 200, Body: map[string]any{"ok": true, "index": i}},
			); err != nil {
				t.Errorf("Append returned error: %v", err)
			}
		}(index)
	}
	wait.Wait()

	logPath := filepath.Join(tempDir, time.Now().Format("2006"), time.Now().Format("01"), time.Now().Format("02"), "conv-1.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != entries {
		t.Fatalf("expected %d lines, got %d", entries, len(lines))
	}
	for _, line := range lines {
		var record Record
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("invalid jsonl record: %v", err)
		}
		if record.ConversationID != "conv-1" {
			t.Fatalf("unexpected conversation id %q", record.ConversationID)
		}
	}
}
