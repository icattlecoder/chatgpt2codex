package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	dirName          = ".chatgpt2codex"
	configFileName   = "config.json"
	chromeProfileDir = "chrome-profile"
	defaultFilePerm  = 0o600
	defaultDirPerm   = 0o755
)

type File struct {
	Domain     string               `json:"domain,omitempty"`
	Cloudflare CloudflareConfig     `json:"cloudflare,omitempty"`
	GPTs       map[string]GPTConfig `json:"gpts"`
}

type CloudflareConfig struct {
	EnableDomain bool   `json:"enableDomain"`
	Domain       string `json:"domain,omitempty"`
	APIToken     string `json:"api_token,omitempty"`

	ZoneID      string `json:"zone_id,omitempty"`
	TunnelToken string `json:"tunnel_token,omitempty"`

	enableDomainSet bool
}

type GPTConfig struct {
	GPTID  string `json:"gpt_id,omitempty"`
	Host   string `json:"host,omitempty"`
	APIKey string `json:"api_key,omitempty"`
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
	legacyDomain := normalizeDomain(f.Domain)
	f.Cloudflare.normalize()
	if f.Cloudflare.Domain == "" && legacyDomain != "" {
		f.Cloudflare.Domain = legacyDomain
		if !f.Cloudflare.enableDomainSet {
			f.Cloudflare.EnableDomain = true
		}
	}
	f.Domain = ""
	if f.GPTs == nil {
		f.GPTs = map[string]GPTConfig{}
	}
	for workspace, entry := range f.GPTs {
		entry.GPTID = strings.TrimSpace(entry.GPTID)
		entry.Host = strings.TrimSpace(entry.Host)
		entry.APIKey = strings.TrimSpace(entry.APIKey)
		if entry.GPTID == "" && entry.Host == "" && entry.APIKey == "" {
			delete(f.GPTs, workspace)
			continue
		}
		f.GPTs[workspace] = entry
	}
}

func (c *CloudflareConfig) normalize() {
	c.Domain = normalizeDomain(c.Domain)
	c.APIToken = strings.TrimSpace(c.APIToken)
	c.ZoneID = ""
	c.TunnelToken = ""
}

func (c *CloudflareConfig) UnmarshalJSON(data []byte) error {
	var raw struct {
		EnableDomain *bool  `json:"enableDomain"`
		Domain       string `json:"domain"`
		APIToken     string `json:"api_token"`
		ZoneID       string `json:"zone_id"`
		TunnelToken  string `json:"tunnel_token"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.EnableDomain != nil {
		c.EnableDomain = *raw.EnableDomain
		c.enableDomainSet = true
	} else {
		c.EnableDomain = false
		c.enableDomainSet = false
	}
	c.Domain = raw.Domain
	c.APIToken = raw.APIToken
	c.ZoneID = raw.ZoneID
	c.TunnelToken = raw.TunnelToken
	return nil
}

func (c CloudflareConfig) IsZero() bool {
	return !c.EnableDomain &&
		strings.TrimSpace(c.Domain) == "" &&
		strings.TrimSpace(c.APIToken) == ""
}

func (c CloudflareConfig) Enabled() bool {
	return c.EnableDomain && strings.TrimSpace(c.Domain) != ""
}

func (c CloudflareConfig) ValidateForCustomDomain() error {
	if !c.EnableDomain {
		return fmt.Errorf("cloudflare custom domain is disabled")
	}
	if strings.TrimSpace(c.Domain) == "" {
		return fmt.Errorf("cloudflare domain is required")
	}
	if err := ValidateDomain(c.Domain); err != nil {
		return err
	}
	if strings.TrimSpace(c.APIToken) == "" {
		return fmt.Errorf("cloudflare api token is required")
	}
	return nil
}

func (f File) GPTID(workspace string) (string, bool) {
	entry, ok := f.Workspace(workspace)
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
	entry := f.WorkspaceConfig(workspace)
	entry.GPTID = strings.TrimSpace(gptID)
	f.SetWorkspace(workspace, entry)
}

func (f File) Workspace(workspace string) (GPTConfig, bool) {
	if f.GPTs == nil {
		return GPTConfig{}, false
	}
	entry, ok := f.GPTs[workspace]
	if !ok {
		return GPTConfig{}, false
	}
	return entry, true
}

func (f File) WorkspaceConfig(workspace string) GPTConfig {
	entry, _ := f.Workspace(workspace)
	return entry
}

func (f *File) SetWorkspace(workspace string, entry GPTConfig) {
	f.normalize()
	entry.GPTID = strings.TrimSpace(entry.GPTID)
	entry.Host = strings.TrimSpace(entry.Host)
	entry.APIKey = strings.TrimSpace(entry.APIKey)
	if entry.GPTID == "" && entry.Host == "" && entry.APIKey == "" {
		delete(f.GPTs, workspace)
		return
	}
	f.GPTs[workspace] = entry
}

func (f *File) ClearWorkspaceHosts() {
	f.normalize()
	for workspace, entry := range f.GPTs {
		entry.Host = ""
		f.GPTs[workspace] = entry
	}
}

func normalizeDomain(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.Trim(domain, ".")
	return domain
}

var dnsLabelPattern = regexp.MustCompile(`[^a-z0-9-]+`)

func WorkspaceDNSLabel(workspace string) string {
	name := filepath.Base(filepath.Clean(strings.TrimSpace(workspace)))
	name = strings.ToLower(name)
	name = dnsLabelPattern.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-")
	if name == "" {
		return "workspace"
	}
	return name
}

func DefaultHostname(workspace, domain string) (string, error) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return "", fmt.Errorf("domain is required")
	}
	return WorkspaceDNSLabel(workspace) + "." + domain, nil
}

func ValidateDomain(domain string) error {
	normalized := normalizeDomain(domain)
	if normalized == "" {
		return fmt.Errorf("domain is required")
	}
	parts := strings.Split(normalized, ".")
	if len(parts) < 2 {
		return fmt.Errorf("domain must contain at least one dot")
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("domain contains an empty label")
		}
		if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
			return fmt.Errorf("domain label %q cannot start or end with '-'", part)
		}
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("domain contains invalid character %q", r)
			}
		}
	}
	return nil
}
