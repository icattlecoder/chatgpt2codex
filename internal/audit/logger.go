package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Logger struct {
	baseDir string
	locks   sync.Map
}

type RequestSnapshot struct {
	Method  string              `json:"method"`
	Path    string              `json:"path"`
	Headers map[string][]string `json:"headers"`
	Query   map[string][]string `json:"query"`
	Body    any                 `json:"body"`
}

type ResponseSnapshot struct {
	Status int `json:"status"`
	Body   any `json:"body"`
}

type Record struct {
	Timestamp      string           `json:"timestamp"`
	ConversationID string           `json:"conversationId"`
	Tool           string           `json:"tool"`
	Workspace      string           `json:"workspace"`
	Request        RequestSnapshot  `json:"request"`
	Response       ResponseSnapshot `json:"response"`
}

func NewLogger(baseDir string) (*Logger, error) {
	if strings.TrimSpace(baseDir) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		baseDir = filepath.Join(home, ".chatgpt2codex")
	}
	return &Logger{baseDir: baseDir}, nil
}

func (l *Logger) Append(conversationID, toolName, workspace string, request RequestSnapshot, response ResponseSnapshot) error {
	now := time.Now()
	path := filepath.Join(
		l.baseDir,
		now.Format("2006"),
		now.Format("01"),
		now.Format("02"),
		sanitizeConversationID(conversationID)+".jsonl",
	)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	record := Record{
		Timestamp:      now.Format(time.RFC3339Nano),
		ConversationID: conversationID,
		Tool:           toolName,
		Workspace:      workspace,
		Request:        request,
		Response:       response,
	}
	line, err := json.Marshal(record)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	value, _ := l.locks.LoadOrStore(path, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(line)
	return err
}

func sanitizeConversationID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "unknown"
	}
	var builder strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteRune('_')
		}
	}
	return builder.String()
}
