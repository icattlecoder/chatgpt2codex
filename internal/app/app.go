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
	"syscall"

	"github.com/spf13/cobra"

	docsasset "github.com/icattlecoder/chatgpt2codex/docs"
	"github.com/icattlecoder/chatgpt2codex/internal/audit"
	"github.com/icattlecoder/chatgpt2codex/internal/prompt"
	"github.com/icattlecoder/chatgpt2codex/internal/proxy"
	"github.com/icattlecoder/chatgpt2codex/internal/server"
	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

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
	var proxyProvider string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the tool API server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedWorkspace := workspace
			if !cmd.Flags().Changed("workspace") && cmd.Flags().Changed("worksapce") {
				resolvedWorkspace = legacyWorkspace
			}
			return runServe(cmd.Context(), resolvedWorkspace, proxyProvider, stdout)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", defaultWorkspace, "default workspace")
	cmd.Flags().StringVar(&legacyWorkspace, "worksapce", "", "deprecated alias for --workspace")
	cmd.Flags().StringVar(&proxyProvider, "proxy", "", "proxy provider: ngrok or cloudflare")
	_ = cmd.Flags().MarkDeprecated("worksapce", "use --workspace instead")
	_ = cmd.Flags().MarkHidden("worksapce")

	return cmd
}

func newToolsCommand(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "tools",
		Short: "Print docs/tools.api.yaml",
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

func runServe(ctx context.Context, workspace, proxyProvider string, stdout io.Writer) error {
	logger, err := audit.NewLogger("")
	if err != nil {
		return err
	}
	handler := server.NewHandler(server.Config{
		DefaultWorkspace: workspace,
		AuditLogger:      logger,
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

	var proxySession *proxy.Session
	if strings.TrimSpace(proxyProvider) != "" {
		proxySession, err = proxy.Start(runContext, proxy.DefaultStarter(), strings.TrimSpace(proxyProvider), localURL)
		if err != nil {
			_ = httpServer.Shutdown(context.Background())
			return err
		}
		if _, err := fmt.Fprintf(stdout, "Public URL: %s\n", proxySession.PublicURL); err != nil {
			return err
		}
	}
	defer func() {
		if proxySession != nil {
			_ = proxySession.Close()
		}
	}()

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
