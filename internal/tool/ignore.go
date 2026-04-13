package tool

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type ignoreRule struct {
	prefix       string
	pattern      string
	negated      bool
	dirOnly      bool
	basenameOnly bool
}

type ignoreMatcher struct {
	rules []ignoreRule
}

func loadIgnoreMatcher(root string) (*ignoreMatcher, error) {
	matcher := &ignoreMatcher{
		rules: []ignoreRule{
			{pattern: ".git", dirOnly: true, basenameOnly: true},
		},
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && entry.Type()&fs.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != ".gitignore" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dir := filepath.Dir(path)
		relativeDir, err := filepath.Rel(root, dir)
		if err != nil {
			return err
		}
		prefix := ""
		if relativeDir != "." {
			prefix = filepath.ToSlash(relativeDir) + "/"
		}
		for _, line := range strings.Split(string(content), "\n") {
			if rule, ok := parseIgnoreRule(line, prefix); ok {
				matcher.rules = append(matcher.rules, rule)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matcher, nil
}

func parseIgnoreRule(line, prefix string) (ignoreRule, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return ignoreRule{}, false
	}
	if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "\\#") {
		return ignoreRule{}, false
	}

	rule := ignoreRule{prefix: prefix}
	pattern := trimmed
	if strings.HasPrefix(pattern, "\\#") || strings.HasPrefix(pattern, "\\!") {
		pattern = pattern[1:]
	}
	if strings.HasPrefix(pattern, "!") {
		rule.negated = true
		pattern = pattern[1:]
	}
	if strings.HasPrefix(pattern, "/") {
		pattern = pattern[1:]
	}
	if strings.HasSuffix(pattern, "/") {
		rule.dirOnly = true
		pattern = strings.TrimSuffix(pattern, "/")
	}
	if pattern == "" {
		return ignoreRule{}, false
	}
	rule.pattern = filepath.ToSlash(pattern)
	rule.basenameOnly = !strings.Contains(rule.pattern, "/")
	return rule, true
}

func (m *ignoreMatcher) Ignore(rel string, isDir bool) bool {
	rel = strings.TrimPrefix(filepath.ToSlash(rel), "./")
	if rel == "" {
		return false
	}

	ignored := false
	for _, rule := range m.rules {
		if rule.matches(rel, isDir) {
			ignored = !rule.negated
		}
	}
	return ignored
}

func (r ignoreRule) matches(rel string, isDir bool) bool {
	if r.dirOnly && !isDir && !strings.Contains(rel, "/") {
		return false
	}

	target := rel
	if r.prefix != "" {
		if rel != strings.TrimSuffix(r.prefix, "/") && !strings.HasPrefix(rel, r.prefix) {
			return false
		}
		target = strings.TrimPrefix(rel, r.prefix)
	}

	if r.basenameOnly {
		segments := strings.Split(target, "/")
		for index, segment := range segments {
			if segment == "" {
				continue
			}
			if r.dirOnly && index == len(segments)-1 && !isDir {
				continue
			}
			if matchPattern(r.pattern, segment) {
				return true
			}
		}
		return false
	}

	if r.dirOnly {
		return target == r.pattern || strings.HasPrefix(target, r.pattern+"/")
	}
	return matchPattern(r.pattern, target)
}

func matchPattern(pattern, value string) bool {
	matched, err := doublestar.PathMatch(pattern, value)
	if err == nil && matched {
		return true
	}
	return false
}

var errStopWalking = errors.New("stop walking")

func walkWorkspace(root string, fn func(absPath, relPath string, entry fs.DirEntry) error) error {
	matcher, err := loadIgnoreMatcher(root)
	if err != nil {
		return err
	}

	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if matcher.Ignore(relPath, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return fn(path, relPath, entry)
	})
}
