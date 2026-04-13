package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	docsasset "github.com/icattlecoder/chatgpt2codex/docs"
	"github.com/icattlecoder/chatgpt2codex/internal/audit"
	"github.com/icattlecoder/chatgpt2codex/internal/prompt"
	"github.com/icattlecoder/chatgpt2codex/internal/proxy"
	"github.com/icattlecoder/chatgpt2codex/internal/server"
	"github.com/icattlecoder/chatgpt2codex/internal/tool"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}

	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:], stdout, stderr)
	case "tools":
		_, err := io.WriteString(stdout, docsasset.ToolsAPISpec)
		return err
	case "promt", "prompt":
		return runPrompt(args[1:], stdout)
	case "-h", "--help", "help":
		printUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	defaultWorkspace, err := os.Getwd()
	if err != nil {
		return err
	}
	workspaceFlag := fs.String("worksapce", defaultWorkspace, "default workspace")
	workspaceAlias := fs.String("workspace", "", "default workspace")
	proxyFlag := fs.String("proxy", "", "proxy provider: ngrok or cloudflare")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if extra := fs.Args(); len(extra) > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(extra, " "))
	}

	workspace := *workspaceFlag
	if strings.TrimSpace(*workspaceAlias) != "" {
		workspace = *workspaceAlias
	}

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
	if strings.TrimSpace(*proxyFlag) != "" {
		proxySession, err = proxy.Start(runContext, proxy.DefaultStarter(), strings.TrimSpace(*proxyFlag), localURL)
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

func runPrompt(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("promt", flag.ContinueOnError)
	defaultWorkspace, err := os.Getwd()
	if err != nil {
		return err
	}
	workspaceFlag := fs.String("worksapce", defaultWorkspace, "working directory")
	workspaceAlias := fs.String("workspace", "", "working directory")
	if err := fs.Parse(args); err != nil {
		return err
	}

	workspace := *workspaceFlag
	if strings.TrimSpace(*workspaceAlias) != "" {
		workspace = *workspaceAlias
	}
	_, err = io.WriteString(stdout, prompt.Build(workspace))
	return err
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: chatgpt2codex <command> [options]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  serve   Start the tool API server")
	fmt.Fprintln(w, "  tools   Print docs/tools.api.yaml")
	fmt.Fprintln(w, "  promt   Print the system prompt")
}
