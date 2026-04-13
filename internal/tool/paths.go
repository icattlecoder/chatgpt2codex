package tool

import (
	"os"
	"path/filepath"
	"strings"
)

func ExpandPath(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func ResolveToWorkspace(workspace, target string) (string, error) {
	target = ExpandPath(strings.TrimSpace(target))
	if target == "" {
		return "", ValidationError("path is required")
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target), nil
	}
	return filepath.Abs(filepath.Join(workspace, target))
}

func ResolveWorkspace(defaultWorkspace, override string) (string, error) {
	workspace := strings.TrimSpace(defaultWorkspace)
	if value := strings.TrimSpace(override); value != "" {
		workspace = value
	}
	if workspace == "" {
		workspace = "."
	}
	return filepath.Abs(ExpandPath(workspace))
}
