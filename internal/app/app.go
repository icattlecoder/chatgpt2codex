package app

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/icattlecoder/chatgpt2codex/internal/audit"
	"github.com/icattlecoder/chatgpt2codex/internal/config"
	"github.com/icattlecoder/chatgpt2codex/internal/gpt"
	"github.com/icattlecoder/chatgpt2codex/internal/prompt"
	"github.com/icattlecoder/chatgpt2codex/internal/proxy"
	"github.com/icattlecoder/chatgpt2codex/internal/server"
	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

var startProxy = func(ctx context.Context, request proxy.StartRequest) (*proxy.Session, error) {
	return proxy.Start(ctx, request)
}

var generateAPIKey = server.GenerateAPIKey

type gptEnsurer interface {
	Ensure(context.Context, gpt.CreateRequest) (gpt.EnsureResult, error)
}

var newConfigStore = func() (gpt.Store, error) {
	return config.NewStore("")
}

var defaultStateDir = config.DefaultBaseDir

var newGPTEnsurer = func(store gpt.Store) (gptEnsurer, error) {
	profileDir, err := config.DefaultChromeProfileDir()
	if err != nil {
		return nil, err
	}
	creator, err := gpt.NewChromeCreator(profileDir)
	if err != nil {
		return nil, err
	}
	return gpt.NewManager(store, creator, creator), nil
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cmd, err := NewRootCommand(stdout, stderr)
	if err != nil {
		return err
	}
	cmd.SetArgs(args)
	return cmd.ExecuteContext(ctx)
}

func NewRootCommand(stdout, stderr io.Writer) (*cobra.Command, error) {
	defaultWorkspace, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	var model string
	var reset bool
	rootCmd := &cobra.Command{
		Use:           "chatgpt2codex",
		Short:         "Expose local tool APIs for code assistants",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd.Context(), defaultWorkspace, model, reset, stdout)
		},
	}
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)
	rootCmd.Flags().StringVar(&model, "model", gpt.DefaultRecommendedModel, "recommended GPT model")
	rootCmd.Flags().BoolVar(&reset, "reset", false, "force GPT metadata sync")

	rootCmd.AddCommand(newDomainCommand(stdout))

	return rootCmd, nil
}

func newDomainCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:           "domain [domain|-]",
		Short:         "Configure the global custom domain",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := newConfigStore()
			if err != nil {
				return err
			}
			cfg, err := store.Load()
			if err != nil {
				return err
			}
			value := ""
			if len(args) == 1 {
				value = strings.TrimSpace(args[0])
			}
			if value == "-" {
				cfg.Cloudflare.EnableDomain = false
				cfg.Domain = ""
				if err := store.Save(cfg); err != nil {
					return err
				}
				_, err := fmt.Fprintln(stdout, "Custom domain disabled.")
				return err
			}

			updated, err := runDomainWizard(cmd.InOrStdin(), stdout, cfg, value)
			if err != nil {
				return err
			}
			if updated.Cloudflare.Domain != cfg.Cloudflare.Domain {
				updated.ClearWorkspaceHosts()
			}
			if err := store.Save(updated); err != nil {
				return err
			}
			if err := printDomainConfigSummary(stdout, updated); err != nil {
				return err
			}
			return nil
		},
	}
}

func runDomainWizard(input io.Reader, stdout io.Writer, cfg config.File, presetDomain string) (config.File, error) {
	reader := bufio.NewReader(input)

	if _, err := fmt.Fprintln(stdout, "Configure Cloudflare custom domain"); err != nil {
		return cfg, err
	}

	domain, err := promptRequiredValue(reader, stdout, "Base domain", firstNonEmpty(strings.TrimSpace(presetDomain), cfg.Cloudflare.Domain), config.ValidateDomain)
	if err != nil {
		return cfg, err
	}
	apiToken, err := promptSecretValue(reader, stdout, "Cloudflare API token", cfg.Cloudflare.APIToken)
	if err != nil {
		return cfg, err
	}

	updated := cfg
	updated.Domain = ""
	updated.Cloudflare = config.CloudflareConfig{
		EnableDomain: true,
		Domain:       strings.ToLower(strings.Trim(strings.TrimSpace(domain), ".")),
		APIToken:     strings.TrimSpace(apiToken),
	}
	return updated, nil
}

func promptRequiredValue(reader *bufio.Reader, stdout io.Writer, label, current string, validate func(string) error) (string, error) {
	for {
		if current != "" {
			if _, err := fmt.Fprintf(stdout, "%s [%s]: ", label, current); err != nil {
				return "", err
			}
		} else {
			if _, err := fmt.Fprintf(stdout, "%s: ", label); err != nil {
				return "", err
			}
		}

		value, err := readPromptInput(reader)
		if err != nil {
			return "", err
		}
		if value == "" {
			value = current
		}
		if value == "" {
			if _, err := fmt.Fprintf(stdout, "%s is required.\n", label); err != nil {
				return "", err
			}
			continue
		}
		if validate != nil {
			if err := validate(value); err != nil {
				if _, writeErr := fmt.Fprintf(stdout, "%v\n", err); writeErr != nil {
					return "", writeErr
				}
				continue
			}
		}
		return value, nil
	}
}

func promptSecretValue(reader *bufio.Reader, stdout io.Writer, label, current string) (string, error) {
	for {
		if current != "" {
			if _, err := fmt.Fprintf(stdout, "%s [press Enter to keep existing]: ", label); err != nil {
				return "", err
			}
		} else {
			if _, err := fmt.Fprintf(stdout, "%s: ", label); err != nil {
				return "", err
			}
		}

		value, err := readPromptInput(reader)
		if err != nil {
			return "", err
		}
		if value == "" {
			value = current
		}
		if value == "" {
			if _, err := fmt.Fprintf(stdout, "%s is required.\n", label); err != nil {
				return "", err
			}
			continue
		}
		return value, nil
	}
}

func readPromptInput(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if errors.Is(err, io.EOF) && line == "" {
		return "", fmt.Errorf("interactive input aborted")
	}
	return strings.TrimSpace(line), nil
}

func printDomainConfigSummary(stdout io.Writer, cfg config.File) error {
	if _, err := fmt.Fprintln(stdout, "Saved custom domain configuration:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Enabled: %t\n", cfg.Cloudflare.EnableDomain); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Domain: %s\n", cfg.Cloudflare.Domain); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "API Token: %s\n", maskSecret(cfg.Cloudflare.APIToken)); err != nil {
		return err
	}
	return nil
}

func maskSecret(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "********"
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func shouldSyncGPT(customDomain, reset bool, workspaceCfg config.GPTConfig) bool {
	if !customDomain || reset {
		return true
	}
	return strings.TrimSpace(workspaceCfg.GPTID) == ""
}

func acquireWorkspaceLock(workspace string) (func() error, error) {
	return acquireInstanceLock(
		workspaceLockFileName(workspace),
		fmt.Sprintf("workspace %s is already served by pid %%d; stop the existing process before starting another instance", workspace),
	)
}

func acquireNamedTunnelLock(identity string) (func() error, error) {
	sum := sha256.Sum256([]byte(strings.TrimSpace(identity)))
	return acquireInstanceLock(
		fmt.Sprintf("named-tunnel-%x.lock", sum),
		"cloudflare named tunnel is already used by pid %d; stop the existing custom-domain instance before starting another one",
	)
}

func acquireInstanceLock(fileName, conflictMessage string) (func() error, error) {
	baseDir, err := defaultStateDir()
	if err != nil {
		return nil, err
	}
	lockPath := filepath.Join(baseDir, "locks", strings.TrimSpace(fileName))
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}

	pid := os.Getpid()
	payload := []byte(strconv.Itoa(pid) + "\n")
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			if _, writeErr := file.Write(payload); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(lockPath)
				return nil, writeErr
			}
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, closeErr
			}
			return func() error {
				if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
					return err
				}
				return nil
			}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}

		ownerPID, readErr := readWorkspaceLockPID(lockPath)
		if readErr != nil {
			return nil, readErr
		}
		if processExists(ownerPID) {
			return nil, fmt.Errorf(conflictMessage, ownerPID)
		}
		if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
}

func workspaceLockFileName(workspace string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(workspace)))
	return fmt.Sprintf("%x.lock", sum)
}

func readWorkspaceLockPID(lockPath string) (int, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	pid, err := strconv.Atoi(line)
	if err != nil {
		return 0, fmt.Errorf("invalid workspace lock file %s", lockPath)
	}
	return pid, nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func runServe(ctx context.Context, workspace, model string, reset bool, stdout io.Writer) error {
	resolvedWorkspace, err := resolveServeWorkspace(workspace)
	if err != nil {
		return err
	}
	releaseLock, err := acquireWorkspaceLock(resolvedWorkspace)
	if err != nil {
		return err
	}
	defer func() {
		_ = releaseLock()
	}()

	store, err := newConfigStore()
	if err != nil {
		return err
	}
	cfg, err := store.Load()
	if err != nil {
		return err
	}
	workspaceCfg := cfg.WorkspaceConfig(resolvedWorkspace)
	if cfg.Cloudflare.EnableDomain {
		if err := cfg.Cloudflare.ValidateForCustomDomain(); err != nil {
			return err
		}
	}
	customDomain := cfg.Cloudflare.Enabled()
	var releaseNamedTunnelLock func() error
	if customDomain {
		releaseNamedTunnelLock, err = acquireNamedTunnelLock(cfg.Cloudflare.Domain)
		if err != nil {
			return err
		}
		defer func() {
			_ = releaseNamedTunnelLock()
		}()
	}
	if customDomain && strings.TrimSpace(workspaceCfg.Host) == "" {
		workspaceCfg.Host, err = config.DefaultHostname(resolvedWorkspace, cfg.Cloudflare.Domain)
		if err != nil {
			return err
		}
	}

	logger, err := audit.NewLogger("")
	if err != nil {
		return err
	}
	apiKey := strings.TrimSpace(workspaceCfg.APIKey)
	if apiKey == "" || !customDomain {
		apiKey, err = generateAPIKey()
		if err != nil {
			return err
		}
	}
	if customDomain {
		workspaceCfg.APIKey = apiKey
		cfg.SetWorkspace(resolvedWorkspace, workspaceCfg)
		if err := store.Save(cfg); err != nil {
			return err
		}
	}
	var publicBaseURL atomic.Value
	publicBaseURL.Store("")

	handler := server.NewHandler(server.Config{
		DefaultWorkspace: resolvedWorkspace,
		AuditLogger:      logger,
		APIKey:           apiKey,
	})

	listener, err := server.ListenFirstAvailable(tool.DefaultStartPort)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: handler}

	serveErr := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	localURL := "http://" + listener.Addr().String()
	if _, err := fmt.Fprintf(stdout, "Listening on %s\n", localURL); err != nil {
		return err
	}

	runContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	proxySession, err := startProxy(runContext, proxy.StartRequest{
		Kind:     "cloudflare",
		LocalURL: localURL,
		Hostname: strings.TrimSpace(workspaceCfg.Host),
		Domain:   cfg.Cloudflare.Domain,
		APIToken: cfg.Cloudflare.APIToken,
	})
	if err != nil {
		_ = httpServer.Shutdown(context.Background())
		return err
	}
	publicBaseURL.Store(proxySession.PublicURL)
	if _, err := fmt.Fprintf(stdout, "Public URL: %s\nAPI Key: %s\n", proxySession.PublicURL, apiKey); err != nil {
		return err
	}
	defer func() {
		if proxySession != nil {
			_ = proxySession.Close()
		}
	}()

	if shouldSyncGPT(customDomain, reset, workspaceCfg) {
		ensurer, err := newGPTEnsurer(store)
		if err != nil {
			_ = httpServer.Shutdown(context.Background())
			return err
		}

		publicURL, _ := publicBaseURL.Load().(string)
		ensureResult, err := ensurer.Ensure(runContext, gpt.CreateRequest{
			Workspace:        resolvedWorkspace,
			GPTName:          gpt.GPTNameForWorkspace(resolvedWorkspace),
			Instructions:     prompt.Build(resolvedWorkspace),
			OpenAPISchema:    server.BuildAPISpec(publicURL),
			RecommendedModel: firstNonEmpty(strings.TrimSpace(model), gpt.DefaultRecommendedModel),
			ActionAPIKey:     apiKey,
			ProgressWriter:   stdout,
		})
		if err != nil {
			_ = httpServer.Shutdown(context.Background())
			return err
		}

		switch {
		case ensureResult.Created:
			if _, err := fmt.Fprintf(stdout, "GPT created: %s\n", ensureResult.GPTID); err != nil {
				return err
			}
		case ensureResult.Updated:
			if _, err := fmt.Fprintf(stdout, "GPT updated: %s\n", ensureResult.GPTID); err != nil {
				return err
			}
		case ensureResult.UpdateSkipped:
			if _, err := fmt.Fprintf(stdout, "GPT update skipped (not implemented): %s\n", ensureResult.GPTID); err != nil {
				return err
			}
		}
	} else {
		if _, err := fmt.Fprintln(stdout, "GPT sync skipped; use --reset to refresh GPT metadata."); err != nil {
			return err
		}
	}

	select {
	case err := <-serveErr:
		return err
	case <-runContext.Done():
		return httpServer.Shutdown(context.Background())
	}
}

func resolveServeWorkspace(workspace string) (string, error) {
	resolvedWorkspace, err := tool.ResolveWorkspace(workspace, "")
	if err != nil {
		return "", err
	}

	info, err := os.Stat(resolvedWorkspace)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("workspace not found: %s", resolvedWorkspace)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace is not a directory: %s", resolvedWorkspace)
	}
	return resolvedWorkspace, nil
}
