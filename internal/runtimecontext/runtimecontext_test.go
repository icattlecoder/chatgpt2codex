package runtimecontext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildFindsNearestAgentsAndSkills(t *testing.T) {
	workspace := t.TempDir()
	nestedDir := filepath.Join(workspace, "pkg", "feature")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	agentsContent := "# Repo instructions\nUse focused patches."
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte(agentsContent), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	skillPath := filepath.Join(workspace, ".agents", "skills", "go-test-triage", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	skillContent := "# go-test-triage\n\nDiagnose failing Go tests and suggest minimal fixes\n\n## Steps\n- Run the failing test"
	if err := os.WriteFile(skillPath, []byte(skillContent), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	systemInstruct, err := Build(nestedDir)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if !strings.Contains(systemInstruct, "<SystemInstruct>") {
		t.Fatalf("expected SystemInstruct wrapper, got %q", systemInstruct)
	}
	if !strings.Contains(systemInstruct, agentsContent) {
		t.Fatalf("expected AGENTS.md content in response")
	}
	if !strings.Contains(systemInstruct, "1. go-test-triage, Diagnose failing Go tests and suggest minimal fixes, usage reference file: "+skillPath) {
		t.Fatalf("expected skill entry in response, got %q", systemInstruct)
	}
}

func TestBuildWithoutAgentsOrSkillsReturnsEmptySections(t *testing.T) {
	workspace := t.TempDir()

	systemInstruct, err := Build(workspace)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	expected := "<SystemInstruct>\n# AGENTS.md\n\nSkills\n\n</SystemInstruct>"
	if systemInstruct != expected {
		t.Fatalf("expected %q, got %q", expected, systemInstruct)
	}
}

func TestRunRequiresCWD(t *testing.T) {
	workspace := t.TempDir()

	_, err := Run(workspace, json.RawMessage(`{}`))
	if err == nil || err.Error() != "cwd is required" {
		t.Fatalf("expected cwd validation error, got %v", err)
	}
}
