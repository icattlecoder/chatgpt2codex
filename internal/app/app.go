package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/icattlecoder/chatgpt2codex/internal/audit"
	"github.com/icattlecoder/chatgpt2codex/internal/config"
	docsasset "github.com/icattlecoder/chatgpt2codex/internal/docsasset"
	"github.com/icattlecoder/chatgpt2codex/internal/gpt"
	"github.com/icattlecoder/chatgpt2codex/internal/prompt"
	"github.com/icattlecoder/chatgpt2codex/internal/proxy"
	"github.com/icattlecoder/chatgpt2codex/internal/server"
	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

var startProxy = func(ctx context.Context, kind, localURL string) (*proxy.Session, error) {
	return proxy.Start(ctx, kind, localURL)
}

type gptEnsurer interface {
	Ensure(context.Context, gpt.CreateRequest) (gpt.EnsureResult, error)
}

var newConfigStore = func() (gpt.Store, error) {
	return config.NewStore("")
}

var newGPTEnsurer = func(store gpt.Store) (gptEnsurer, error) {
	profileDir, err := config.DefaultChromeProfileDir()
	if err != nil {
		return nil, err
	}
	creator, err := gpt.NewChromeCreator(profileDir)
	if err != nil {
		return nil, err
	}
	return gpt.NewManager(store, creator, nil), nil
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

	rootCmd := &cobra.Command{
		Use:           "chatgpt2codex",
		Short:         "Expose local tool APIs for code assistants",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)

	rootCmd.AddCommand(
		newServeCommand(defaultWorkspace, stdout),
		newToolsCommand(stdout),
		newPromptCommand(defaultWorkspace, stdout),
	)

	return rootCmd, nil
}

func newServeCommand(defaultWorkspace string, stdout io.Writer) *cobra.Command {
	var workspace string
	var legacyWorkspace string
	var model string
	var legacyProxy string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the tool API server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedWorkspace := workspace
			if !cmd.Flags().Changed("workspace") && cmd.Flags().Changed("worksapce") {
				resolvedWorkspace = legacyWorkspace
			}
			return runServe(cmd.Context(), resolvedWorkspace, model, stdout)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", defaultWorkspace, "default workspace")
	cmd.Flags().StringVar(&legacyWorkspace, "worksapce", "", "deprecated alias for --workspace")
	cmd.Flags().StringVar(&model, "model", gpt.DefaultRecommendedModel, "recommended GPT model")
	cmd.Flags().StringVar(&legacyProxy, "proxy", "", "deprecated proxy provider flag; cloudflare is always enabled")
	_ = cmd.Flags().MarkDeprecated("worksapce", "use --workspace instead")
	_ = cmd.Flags().MarkHidden("worksapce")
	_ = cmd.Flags().MarkDeprecated("proxy", "cloudflare is always enabled")
	_ = cmd.Flags().MarkHidden("proxy")

	return cmd
}

func newToolsCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "Print embedded tools API spec",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := io.WriteString(stdout, docsasset.ToolsAPISpec)
			return err
		},
	}
}

func newPromptCommand(defaultWorkspace string, stdout io.Writer) *cobra.Command {
	var workspace string
	var legacyWorkspace string

	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Print the system prompt",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedWorkspace := workspace
			if !cmd.Flags().Changed("workspace") && cmd.Flags().Changed("worksapce") {
				resolvedWorkspace = legacyWorkspace
			}
			return runPrompt(resolvedWorkspace, stdout)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", defaultWorkspace, "working directory")
	cmd.Flags().StringVar(&legacyWorkspace, "worksapce", "", "deprecated alias for --workspace")
	_ = cmd.Flags().MarkDeprecated("worksapce", "use --workspace instead")
	_ = cmd.Flags().MarkHidden("worksapce")

	return cmd
}

func runServe(ctx context.Context, workspace, model string, stdout io.Writer) error {
	resolvedWorkspace, err := resolveServeWorkspace(workspace)
	if err != nil {
		return err
	}

	logger, err := audit.NewLogger("")
	if err != nil {
		return err
	}
	var publicBaseURL atomic.Value
	publicBaseURL.Store("")

	handler := server.NewHandler(server.Config{
		DefaultWorkspace: resolvedWorkspace,
		AuditLogger:      logger,
		PublicBaseURL: func() string {
			value, _ := publicBaseURL.Load().(string)
			return value
		},
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

	proxySession, err := startProxy(runContext, "cloudflare", localURL)
	if err != nil {
		_ = httpServer.Shutdown(context.Background())
		return err
	}
	publicBaseURL.Store(proxySession.PublicURL)
	if _, err := fmt.Fprintf(stdout, "Public URL: %s\n%s\n", proxySession.PublicURL, strings.TrimRight(proxySession.PublicURL, "/")+"/api.yaml"); err != nil {
		return err
	}
	defer func() {
		if proxySession != nil {
			_ = proxySession.Close()
		}
	}()

	store, err := newConfigStore()
	if err != nil {
		_ = httpServer.Shutdown(context.Background())
		return err
	}
	ensurer, err := newGPTEnsurer(store)
	if err != nil {
		_ = httpServer.Shutdown(context.Background())
		return err
	}

	ensureResult, err := ensurer.Ensure(runContext, gpt.CreateRequest{
		Workspace:        resolvedWorkspace,
		GPTName:          gpt.GPTNameForWorkspace(resolvedWorkspace),
		Instructions:     prompt.Build(resolvedWorkspace),
		OpenAPISchema:    server.BuildAPISpec(proxySession.PublicURL),
		RecommendedModel: firstNonEmpty(strings.TrimSpace(model), gpt.DefaultRecommendedModel),
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

	select {
	case err := <-serveErr:
		return err
	case <-runContext.Done():
		return httpServer.Shutdown(context.Background())
	}
}

func runPrompt(workspace string, stdout io.Writer) error {
	_, err := io.WriteString(stdout, prompt.Build(workspace))
	return err
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
