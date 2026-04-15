package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	dirName          = ".chatgpt2codex"
	configFileName   = "config.json"
	chromeProfileDir = "chrome-profile"
	defaultFilePerm  = 0o644
	defaultDirPerm   = 0o755
)

type File struct {
	GPTs map[string]GPTConfig `json:"gpts"`
}

type GPTConfig struct {
	GPTID string `json:"gpt_id"`
}

type Store struct {
	path string
}

func DefaultBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, dirName), nil
}

func DefaultConfigPath() (string, error) {
	baseDir, err := DefaultBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, configFileName), nil
}

func DefaultChromeProfileDir() (string, error) {
	baseDir, err := DefaultBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, chromeProfileDir), nil
}

func NewStore(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultConfigPath()
		if err != nil {
			return nil, err
		}
	}
	return &Store{path: path}, nil
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) Load() (File, error) {
	if s == nil {
		return File{GPTs: map[string]GPTConfig{}}, nil
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return File{GPTs: map[string]GPTConfig{}}, nil
		}
		return File{}, err
	}

	var cfg File
	if err := json.Unmarshal(data, &cfg); err != nil {
		return File{}, err
	}
	cfg.normalize()
	return cfg, nil
}

func (s *Store) Save(cfg File) error {
	if s == nil {
		return nil
	}

	cfg.normalize()

	if err := os.MkdirAll(filepath.Dir(s.path), defaultDirPerm); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tempFile, err := os.CreateTemp(filepath.Dir(s.path), "config-*.json")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := tempFile.Chmod(defaultFilePerm); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}

	if err := os.Rename(tempPath, s.path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func (f *File) normalize() {
	if f.GPTs == nil {
		f.GPTs = map[string]GPTConfig{}
	}
}

func (f File) GPTID(workspace string) (string, bool) {
	entry, ok := f.GPTs[workspace]
	if !ok {
		return "", false
	}
	gptID := strings.TrimSpace(entry.GPTID)
	if gptID == "" {
		return "", false
	}
	return gptID, true
}

func (f *File) SetGPTID(workspace, gptID string) {
	f.normalize()
	f.GPTs[workspace] = GPTConfig{GPTID: strings.TrimSpace(gptID)}
}
