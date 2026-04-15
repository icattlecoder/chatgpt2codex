package runtimecontext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

type Request struct {
	CWD string `json:"cwd"`
}

type Response struct {
	SystemInstruct string `json:"system_instruct"`
}

type Skill struct {
	Name        string
	Description string
	Path        string
}

func Run(workspace string, raw json.RawMessage) (Response, error) {
	var request Request
	if err := decodeRequest(raw, &request); err != nil {
		return Response{}, err
	}

	cwd, err := resolveCWD(workspace, request.CWD)
	if err != nil {
		return Response{}, err
	}

	systemInstruct, err := Build(cwd)
	if err != nil {
		return Response{}, err
	}

	return Response{SystemInstruct: systemInstruct}, nil
}

func Build(cwd string) (string, error) {
	resolvedCWD, err := filepath.Abs(tool.ExpandPath(strings.TrimSpace(cwd)))
	if err != nil {
		return "", tool.ValidationError("invalid cwd: " + err.Error())
	}

	info, err := os.Stat(resolvedCWD)
	if err != nil {
		if os.IsNotExist(err) {
			return "", tool.ValidationError("cwd not found: " + resolvedCWD)
		}
		return "", tool.ExecutionError(err.Error())
	}
	if !info.IsDir() {
		return "", tool.ValidationError("cwd is not a directory: " + resolvedCWD)
	}

	agentsPath, err := findNearestAncestorFile(resolvedCWD, "AGENTS.md")
	if err != nil {
		return "", tool.ExecutionError(err.Error())
	}
	skillsRoot, err := findNearestAncestorDir(resolvedCWD, filepath.Join(".agents", "skills"))
	if err != nil {
		return "", tool.ExecutionError(err.Error())
	}

	agentsContent := ""
	if agentsPath != "" {
		data, err := os.ReadFile(agentsPath)
		if err != nil {
			return "", tool.ExecutionError(err.Error())
		}
		agentsContent = strings.TrimRight(string(data), "\n")
	}

	skills, err := collectSkills(skillsRoot)
	if err != nil {
		return "", tool.ExecutionError(err.Error())
	}

	return formatSystemInstruct(agentsContent, skills), nil
}

func decodeRequest(raw json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return tool.ValidationError("request body is required")
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return tool.ValidationError("invalid JSON body: " + err.Error())
	}
	return nil
}

func resolveCWD(workspace, requestedCWD string) (string, error) {
	if strings.TrimSpace(requestedCWD) == "" {
		return "", tool.ValidationError("cwd is required")
	}
	return tool.ResolveToWorkspace(workspace, requestedCWD)
}

func findNearestAncestorFile(startDir, fileName string) (string, error) {
	return findNearestAncestor(startDir, func(dir string) (string, error) {
		candidate := filepath.Join(dir, fileName)
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() {
				return "", nil
			}
			return candidate, nil
		}
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	})
}

func findNearestAncestorDir(startDir, dirName string) (string, error) {
	return findNearestAncestor(startDir, func(dir string) (string, error) {
		candidate := filepath.Join(dir, dirName)
		info, err := os.Stat(candidate)
		if err == nil {
			if !info.IsDir() {
				return "", nil
			}
			return candidate, nil
		}
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	})
}

func findNearestAncestor(startDir string, match func(string) (string, error)) (string, error) {
	current := filepath.Clean(startDir)
	for {
		found, err := match(current)
		if err != nil {
			return "", err
		}
		if found != "" {
			return found, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", nil
		}
		current = parent
	}
}

func collectSkills(skillsRoot string) ([]Skill, error) {
	if strings.TrimSpace(skillsRoot) == "" {
		return nil, nil
	}

	skills := make([]Skill, 0)
	err := filepath.WalkDir(skillsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		skills = append(skills, Skill{
			Name:        filepath.Base(filepath.Dir(path)),
			Description: extractSkillDescription(string(data)),
			Path:        path,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name == skills[j].Name {
			return skills[i].Path < skills[j].Path
		}
		return skills[i].Name < skills[j].Name
	})

	return skills, nil
}

func extractSkillDescription(content string) string {
	lines := strings.Split(content, "\n")
	inCodeFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			inCodeFence = !inCodeFence
			continue
		}
		if inCodeFence {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = trimListMarker(trimmed)
		trimmed = strings.Join(strings.Fields(trimmed), " ")
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func trimListMarker(line string) string {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") {
		return strings.TrimSpace(line[2:])
	}

	index := 0
	for index < len(line) && line[index] >= '0' && line[index] <= '9' {
		index++
	}
	if index > 0 && index+1 < len(line) && line[index] == '.' && line[index+1] == ' ' {
		return strings.TrimSpace(line[index+2:])
	}
	return line
}

func formatSystemInstruct(agentsContent string, skills []Skill) string {
	var builder strings.Builder
	builder.WriteString("<SystemInstruct>\n")
	builder.WriteString("# AGENTS.md\n")
	if agentsContent != "" {
		builder.WriteString(agentsContent)
		builder.WriteString("\n")
	}
	builder.WriteString("\nSkills\n\n")
	for index, skill := range skills {
		builder.WriteString(fmt.Sprintf("%d. %s", index+1, skill.Name))
		if skill.Description != "" {
			builder.WriteString(", ")
			builder.WriteString(skill.Description)
		}
		builder.WriteString(", usage reference file: ")
		builder.WriteString(skill.Path)
		builder.WriteString("\n")
	}
	builder.WriteString("</SystemInstruct>")
	return builder.String()
}
