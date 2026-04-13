package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWriteAndLS(t *testing.T) {
	workspace := t.TempDir()
	if _, err := RunWrite(context.Background(), workspace, []byte(`{"path":"nested/file.txt","content":"hello"}`)); err != nil {
		t.Fatalf("RunWrite returned error: %v", err)
	}
	response, err := RunLS(context.Background(), workspace, []byte(`{"path":"nested"}`))
	if err != nil {
		t.Fatalf("RunLS returned error: %v", err)
	}
	body, _ := json.Marshal(response)
	if !strings.Contains(string(body), "file.txt") {
		t.Fatalf("expected ls output to include file.txt")
	}
}

func TestRunReadHonorsLimitAndContinuationNotice(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	response, err := RunRead(context.Background(), workspace, []byte(`{"path":"note.txt","limit":1}`))
	if err != nil {
		t.Fatalf("RunRead returned error: %v", err)
	}
	body, _ := json.Marshal(response)
	if !strings.Contains(string(body), "1 more lines") && !strings.Contains(string(body), "2 more lines") {
		t.Fatalf("expected continuation notice, got %s", string(body))
	}
}

func TestRunEditAppliesDisjointEdits(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	_, err := RunEdit(context.Background(), workspace, []byte(`{"path":"note.txt","edits":[{"oldText":"hello","newText":"hi"},{"oldText":"world","newText":"earth"}]}`))
	if err != nil {
		t.Fatalf("RunEdit returned error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(data) != "hi\nearth\n" {
		t.Fatalf("unexpected file content %q", string(data))
	}
}

func TestRunEditRejectsOverlappingMatches(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(path, []byte("abcde"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	_, err := RunEdit(context.Background(), workspace, []byte(`{"path":"note.txt","edits":[{"oldText":"abc","newText":"x"},{"oldText":"bcd","newText":"y"}]}`))
	if err == nil {
		t.Fatalf("expected overlapping edits to fail")
	}
}

func TestRunFindRespectsGitignore(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "visible.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "ignored.txt"), []byte("skip"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	response, err := RunFind(context.Background(), workspace, []byte(`{"pattern":"*.txt"}`))
	if err != nil {
		t.Fatalf("RunFind returned error: %v", err)
	}
	body, _ := json.Marshal(response)
	if !strings.Contains(string(body), "visible.txt") {
		t.Fatalf("expected visible.txt in output")
	}
	if strings.Contains(string(body), "ignored.txt") {
		t.Fatalf("did not expect ignored.txt in output")
	}
}

func TestRunGrepRespectsGitignore(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "visible.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "ignored.txt"), []byte("needle\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	response, err := RunGrep(context.Background(), workspace, []byte(`{"pattern":"needle"}`))
	if err != nil {
		t.Fatalf("RunGrep returned error: %v", err)
	}
	body, _ := json.Marshal(response)
	if !strings.Contains(string(body), "visible.txt:1: needle") {
		t.Fatalf("expected visible.txt match, got %s", string(body))
	}
	if strings.Contains(string(body), "ignored.txt") {
		t.Fatalf("did not expect ignored.txt in output")
	}
}

func TestRunBashReturnsTimeoutError(t *testing.T) {
	workspace := t.TempDir()
	response, err := RunBash(context.Background(), workspace, []byte(`{"command":"sleep 2","timeout":1}`))
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	body, _ := json.Marshal(response)
	if !strings.Contains(string(body), "timed out") {
		t.Fatalf("expected timeout text, got %s", string(body))
	}
}
