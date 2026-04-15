package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	cloudflarecli "github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	cloudflaretunnel "github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

const (
	cloudflareSourceVersion = "2025.4.0"
	startupTimeout          = 20 * time.Second
	quickTunnelTimeout      = 15 * time.Second
	startupLogLimit         = 10
)

var (
	embeddedBuildInfo = cloudflarecli.GetBuildInfo("embedded-source", cloudflareSourceVersion)

	quickTunnelServiceURL         = "https://api.trycloudflare.com"
	startEmbeddedCloudflareTunnel = runEmbeddedCloudflareTunnel
)

type Session struct {
	PublicURL string

	closeOnce sync.Once
	closeFn   func() error
	closeErr  error
}

type quickTunnelSession struct {
	Hostname    string
	PublicURL   string
	Credentials connection.Credentials
}

type quickTunnelResponse struct {
	Success bool               `json:"success"`
	Result  quickTunnelResult  `json:"result"`
	Errors  []quickTunnelError `json:"errors"`
}

type quickTunnelResult struct {
	ID         string `json:"id"`
	Hostname   string `json:"hostname"`
	AccountTag string `json:"account_tag"`
	Secret     []byte `json:"secret"`
}

type quickTunnelError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type startupLogBuffer struct {
	mu        sync.Mutex
	lines     []string
	partial   string
	limit     int
	readyText string
	readyCh   chan struct{}
	readyOnce sync.Once
}

func Start(ctx context.Context, kind, localURL string) (*Session, error) {
	switch strings.TrimSpace(kind) {
	case "cloudflare":
		return startEmbeddedCloudflareTunnel(ctx, localURL)
	default:
		return nil, fmt.Errorf("unsupported proxy %q", kind)
	}
}

func runEmbeddedCloudflareTunnel(ctx context.Context, localURL string) (*Session, error) {
	quickTunnel, err := requestQuickTunnel(ctx)
	if err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	logBuffer := newStartupLogBuffer(startupLogLimit, "Registered tunnel connection")
	logger := zerolog.New(logBuffer).With().Timestamp().Logger()

	cliCtx, err := newCloudflareCLIContext(runCtx, localURL)
	if err != nil {
		cancel()
		return nil, err
	}

	gracefulShutdown := make(chan struct{})
	cloudflaretunnel.Init(embeddedBuildInfo, gracefulShutdown)

	done := make(chan error, 1)
	go func() {
		done <- cloudflaretunnel.StartServer(
			cliCtx,
			embeddedBuildInfo,
			&connection.TunnelProperties{
				Credentials:    quickTunnel.Credentials,
				QuickTunnelUrl: quickTunnel.Hostname,
			},
			&logger,
		)
	}()

	timeout := time.NewTimer(startupTimeout)
	defer timeout.Stop()

	select {
	case <-logBuffer.Ready():
		return &Session{
			PublicURL: quickTunnel.PublicURL,
			closeFn: func() error {
				cancel()
				return normalizeRuntimeError(<-done)
			},
		}, nil
	case err := <-done:
		cancel()
		if err == nil {
			err = errors.New("embedded cloudflare tunnel exited before registering a connection")
		}
		return nil, fmt.Errorf("proxy startup failed: %w. output: %s", err, logBuffer.String())
	case <-timeout.C:
		cancel()
		go drainDone(done)
		return nil, fmt.Errorf("proxy startup timed out waiting for a public URL. output: %s", logBuffer.String())
	case <-ctx.Done():
		cancel()
		go drainDone(done)
		return nil, ctx.Err()
	}
}

func requestQuickTunnel(ctx context.Context) (*quickTunnelSession, error) {
	requestCtx, cancel := context.WithTimeout(ctx, quickTunnelTimeout)
	defer cancel()

	client := http.Client{
		Transport: &http.Transport{
			TLSHandshakeTimeout:   quickTunnelTimeout,
			ResponseHeaderTimeout: quickTunnelTimeout,
		},
		Timeout: quickTunnelTimeout,
	}

	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, quickTunnelServiceURL+"/tunnel", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build quick tunnel request: %w", err)
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("User-Agent", embeddedBuildInfo.UserAgent())

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request quick tunnel: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read quick tunnel response: %w", err)
	}

	var payload quickTunnelResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("failed to unmarshal quick tunnel response: %w", err)
	}
	if !payload.Success {
		return nil, fmt.Errorf("failed to request quick tunnel: %s", payload.errorMessage(resp.Status))
	}

	hostname := strings.TrimSpace(payload.Result.Hostname)
	if hostname == "" {
		return nil, errors.New("quick tunnel response did not include a hostname")
	}

	tunnelID, err := uuid.Parse(payload.Result.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse quick tunnel ID: %w", err)
	}

	publicURL := hostname
	if !strings.HasPrefix(publicURL, "https://") {
		publicURL = "https://" + publicURL
	}

	return &quickTunnelSession{
		Hostname:  hostname,
		PublicURL: publicURL,
		Credentials: connection.Credentials{
			AccountTag:   payload.Result.AccountTag,
			TunnelSecret: payload.Result.Secret,
			TunnelID:     tunnelID,
		},
	}, nil
}

func newCloudflareCLIContext(ctx context.Context, localURL string) (*cli.Context, error) {
	app := &cli.App{
		Name:    "chatgpt2codex-cloudflare",
		Version: cloudflareSourceVersion,
		Flags:   cloudflaretunnel.Flags(),
	}

	set := flag.NewFlagSet(app.Name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	for _, flagDef := range app.Flags {
		if err := flagDef.Apply(set); err != nil {
			return nil, err
		}
	}

	args := []string{
		"--url", localURL,
		// Prefer HTTP/2 here. QUIC has hit TLS curve negotiation failures on
		// some environments, which prevents quick tunnels from coming up.
		"--protocol", "http2",
		"--ha-connections", "1",
		"--no-autoupdate",
		"--metrics", "127.0.0.1:0",
		"--loglevel", "info",
		"--transport-loglevel", "error",
	}
	if err := set.Parse(args); err != nil {
		return nil, err
	}

	cliCtx := cli.NewContext(app, set, nil)
	cliCtx.Context = ctx
	return cliCtx, nil
}

func (r quickTunnelResponse) errorMessage(status string) string {
	messages := make([]string, 0, len(r.Errors))
	for _, entry := range r.Errors {
		if strings.TrimSpace(entry.Message) != "" {
			messages = append(messages, entry.Message)
		}
	}
	if len(messages) == 0 {
		return status
	}
	return strings.Join(messages, "; ")
}

func newStartupLogBuffer(limit int, readyText string) *startupLogBuffer {
	return &startupLogBuffer{
		lines:     make([]string, 0, limit),
		limit:     limit,
		readyText: readyText,
		readyCh:   make(chan struct{}),
	}
}

func (b *startupLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.partial += string(p)
	for {
		idx := strings.IndexByte(b.partial, '\n')
		if idx < 0 {
			break
		}
		b.addLineLocked(strings.TrimSpace(b.partial[:idx]))
		b.partial = b.partial[idx+1:]
	}
	return len(p), nil
}

func (b *startupLogBuffer) Ready() <-chan struct{} {
	return b.readyCh
}

func (b *startupLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	lines := append([]string(nil), b.lines...)
	if pending := strings.TrimSpace(b.partial); pending != "" {
		lines = append(lines, pending)
	}
	return strings.Join(lines, " | ")
}

func (b *startupLogBuffer) addLineLocked(line string) {
	if line == "" {
		return
	}
	b.lines = append(b.lines, line)
	if len(b.lines) > b.limit {
		b.lines = b.lines[len(b.lines)-b.limit:]
	}
	if strings.Contains(line, b.readyText) {
		b.readyOnce.Do(func() {
			close(b.readyCh)
		})
	}
}

func (s *Session) Close() error {
	if s == nil || s.closeFn == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = s.closeFn()
	})
	return s.closeErr
}

func normalizeRuntimeError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func drainDone(done <-chan error) {
	<-done
}
