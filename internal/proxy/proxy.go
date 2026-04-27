package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cloudflare/cloudflared/cfapi"
	cloudflarecli "github.com/cloudflare/cloudflared/cmd/cloudflared/cliutil"
	cloudflaretunnel "github.com/cloudflare/cloudflared/cmd/cloudflared/tunnel"
	"github.com/cloudflare/cloudflared/connection"
	"github.com/cloudflare/cloudflared/validation"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

const (
	cloudflareSourceVersion = "2025.4.0"
	startupTimeout          = 20 * time.Second
	quickTunnelTimeout      = 15 * time.Second
	publicURLProbeTimeout   = 15 * time.Second
	publicURLProbeInterval  = 500 * time.Millisecond
	publicURLRequestTimeout = 2 * time.Second
	startupLogLimit         = 10
)

var (
	embeddedBuildInfo = cloudflarecli.GetBuildInfo("embedded-source", cloudflareSourceVersion)

	quickTunnelServiceURL         = "https://api.trycloudflare.com"
	cloudflareAPIBaseURL          = "https://api.cloudflare.com/client/v4"
	startEmbeddedCloudflareTunnel = runEmbeddedCloudflareTunnel
	startNamedCloudflareTunnel    = runNamedCloudflareTunnel
	routeNamedTunnelHostname      = ensureNamedTunnelRoute
	configureNamedTunnelIngress   = upsertNamedTunnelConfiguration
)

const (
	defaultNamedTunnelName = "chatgpt2codex"
)

type StartRequest struct {
	Kind     string
	LocalURL string
	Hostname string
	Domain   string
	APIToken string
}

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

type namedTunnelConfigRequest struct {
	Config namedTunnelRemoteConfig `json:"config"`
}

type namedTunnelRemoteConfig struct {
	Ingress []namedTunnelIngressRule `json:"ingress"`
}

type namedTunnelIngressRule struct {
	Hostname      string         `json:"hostname,omitempty"`
	Service       string         `json:"service"`
	OriginRequest map[string]any `json:"originRequest,omitempty"`
}

type cloudflareAPIResponse struct {
	Success bool                 `json:"success"`
	Errors  []cloudflareAPIError `json:"errors"`
}

type cloudflareAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cloudflareZoneListResponse struct {
	Success bool                 `json:"success"`
	Result  []cloudflareZone     `json:"result"`
	Errors  []cloudflareAPIError `json:"errors"`
}

type cloudflareZone struct {
	ID      string                `json:"id"`
	Name    string                `json:"name"`
	Account cloudflareZoneAccount `json:"account"`
}

type cloudflareZoneAccount struct {
	ID string `json:"id"`
}

type cloudflareTunnelListResponse struct {
	Success bool                 `json:"success"`
	Result  []cloudflareTunnel   `json:"result"`
	Errors  []cloudflareAPIError `json:"errors"`
}

type cloudflareTunnel struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type cloudflareTunnelTokenResponse struct {
	Success bool                 `json:"success"`
	Result  string               `json:"result"`
	Errors  []cloudflareAPIError `json:"errors"`
}

type namedTunnelResolution struct {
	ZoneID    string
	AccountID string
	TunnelID  uuid.UUID
	Token     connection.TunnelToken
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

func Start(ctx context.Context, request StartRequest) (*Session, error) {
	switch strings.TrimSpace(request.Kind) {
	case "cloudflare":
		if strings.TrimSpace(request.Hostname) != "" {
			return startNamedCloudflareTunnel(ctx, request)
		}
		return startEmbeddedCloudflareTunnel(ctx, request.LocalURL)
	default:
		return nil, fmt.Errorf("unsupported proxy %q", request.Kind)
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

	cliCtx, err := newCloudflareCLIContext(runCtx, localURL, "")
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
		if err := waitForPublicURLReady(runCtx, quickTunnel.PublicURL, publicURLProbeTimeout, publicURLProbeInterval); err != nil {
			cancel()
			go drainDone(done)
			return nil, fmt.Errorf("proxy public url %s not ready: %w", quickTunnel.PublicURL, err)
		}
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

func runNamedCloudflareTunnel(ctx context.Context, request StartRequest) (*Session, error) {
	hostname, err := validation.ValidateHostname(strings.TrimSpace(request.Hostname))
	if err != nil {
		return nil, fmt.Errorf("invalid custom hostname: %w", err)
	}

	resolution, err := resolveNamedTunnel(ctx, request.APIToken, request.Domain)
	if err != nil {
		return nil, err
	}
	credentials := resolution.Token.Credentials()
	if credentials.TunnelID != resolution.TunnelID {
		return nil, fmt.Errorf("cloudflare tunnel token does not match tunnel %s", resolution.TunnelID.String())
	}

	if err := routeNamedTunnelHostname(
		ctx,
		request.APIToken,
		resolution.ZoneID,
		credentials.TunnelID,
		credentials.AccountTag,
		hostname,
	); err != nil {
		return nil, err
	}
	if err := configureNamedTunnelIngress(
		ctx,
		request.APIToken,
		credentials.TunnelID,
		credentials.AccountTag,
		hostname,
		request.LocalURL,
	); err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	logBuffer := newStartupLogBuffer(startupLogLimit, "Registered tunnel connection")
	logger := zerolog.New(logBuffer).With().Timestamp().Logger()

	cliCtx, err := newCloudflareCLIContext(runCtx, request.LocalURL, hostname)
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
			&connection.TunnelProperties{Credentials: credentials},
			&logger,
		)
	}()

	timeout := time.NewTimer(startupTimeout)
	defer timeout.Stop()

	publicURL := "https://" + hostname
	select {
	case <-logBuffer.Ready():
		if err := waitForPublicURLReady(runCtx, publicURL, publicURLProbeTimeout, publicURLProbeInterval); err != nil {
			cancel()
			go drainDone(done)
			return nil, fmt.Errorf("proxy public url %s not ready: %w", publicURL, err)
		}
		return &Session{
			PublicURL: publicURL,
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
		return nil, fmt.Errorf("proxy startup timed out waiting for public hostname %s. output: %s", hostname, logBuffer.String())
	case <-ctx.Done():
		cancel()
		go drainDone(done)
		return nil, ctx.Err()
	}
}

func ensureNamedTunnelRoute(ctx context.Context, apiToken, zoneID string, tunnelID uuid.UUID, accountTag, hostname string) error {
	apiToken = strings.TrimSpace(apiToken)
	zoneID = strings.TrimSpace(zoneID)
	if apiToken == "" || zoneID == "" {
		return fmt.Errorf("custom domain requires cloudflare domain and api token; rerun `chatgpt2codex domain`")
	}

	logger := zerolog.Nop()
	client, err := cfapi.NewRESTClient(cloudflareAPIBaseURL, accountTag, zoneID, apiToken, embeddedBuildInfo.UserAgent(), &logger)
	if err != nil {
		return fmt.Errorf("failed to initialize cloudflare api client: %w", err)
	}
	if _, err := client.RouteTunnel(tunnelID, cfapi.NewDNSRoute(hostname, false)); err != nil {
		return fmt.Errorf("failed to route hostname %s to tunnel %s: %w", hostname, tunnelID.String(), err)
	}
	return nil
}

func upsertNamedTunnelConfiguration(ctx context.Context, apiToken string, tunnelID uuid.UUID, accountTag, hostname, localURL string) error {
	apiToken = strings.TrimSpace(apiToken)
	accountTag = strings.TrimSpace(accountTag)
	hostname = strings.TrimSpace(hostname)
	localURL = strings.TrimSpace(localURL)
	if apiToken == "" {
		return fmt.Errorf("custom domain requires cloudflare api token; rerun `chatgpt2codex domain`")
	}
	if accountTag == "" {
		return errors.New("cloudflare account tag is required")
	}
	if hostname == "" {
		return errors.New("hostname is required")
	}
	if localURL == "" {
		return errors.New("local url is required")
	}

	payload, err := json.Marshal(namedTunnelConfigRequest{
		Config: namedTunnelRemoteConfig{
			Ingress: []namedTunnelIngressRule{
				{
					Hostname:      hostname,
					Service:       localURL,
					OriginRequest: map[string]any{},
				},
				{Service: "http_status:404"},
			},
		},
	})
	if err != nil {
		return err
	}

	endpoint := fmt.Sprintf("%s/accounts/%s/cfd_tunnel/%s/configurations", cloudflareAPIBaseURL, accountTag, tunnelID.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", embeddedBuildInfo.UserAgent())

	client := &http.Client{Timeout: quickTunnelTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to configure tunnel ingress for %s: %w", hostname, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("failed to configure tunnel ingress for %s: status %d: %s", hostname, resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var result cloudflareAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode tunnel ingress response: %w", err)
	}
	if !result.Success {
		messages := make([]string, 0, len(result.Errors))
		for _, entry := range result.Errors {
			if strings.TrimSpace(entry.Message) != "" {
				messages = append(messages, strings.TrimSpace(entry.Message))
			}
		}
		if len(messages) == 0 {
			return fmt.Errorf("failed to configure tunnel ingress for %s", hostname)
		}
		return fmt.Errorf("failed to configure tunnel ingress for %s: %s", hostname, strings.Join(messages, "; "))
	}
	return nil
}

func resolveNamedTunnel(ctx context.Context, apiToken, domain string) (*namedTunnelResolution, error) {
	apiToken = strings.TrimSpace(apiToken)
	domain = strings.ToLower(strings.Trim(strings.TrimSpace(domain), "."))
	if domain == "" {
		return nil, errors.New("custom domain requires cloudflare domain; rerun `chatgpt2codex domain`")
	}
	if apiToken == "" {
		return nil, errors.New("custom domain requires cloudflare api token; rerun `chatgpt2codex domain`")
	}

	zone, err := lookupCloudflareZone(ctx, apiToken, domain)
	if err != nil {
		return nil, err
	}
	tunnel, err := lookupNamedTunnel(ctx, apiToken, zone.Account.ID, defaultNamedTunnelName)
	if err != nil {
		return nil, err
	}
	tokenValue, err := fetchNamedTunnelToken(ctx, apiToken, zone.Account.ID, tunnel.ID)
	if err != nil {
		return nil, err
	}
	token, err := cloudflaretunnel.ParseToken(tokenValue)
	if err != nil {
		return nil, fmt.Errorf("invalid tunnel token returned by cloudflare: %w", err)
	}
	return &namedTunnelResolution{
		ZoneID:    zone.ID,
		AccountID: zone.Account.ID,
		TunnelID:  tunnel.ID,
		Token:     *token,
	}, nil
}

func lookupCloudflareZone(ctx context.Context, apiToken, domain string) (cloudflareZone, error) {
	endpoint, err := cloudflareEndpoint("/zones", url.Values{"name": []string{domain}})
	if err != nil {
		return cloudflareZone{}, err
	}
	body, status, err := requestCloudflareAPI(ctx, apiToken, endpoint)
	if err != nil {
		return cloudflareZone{}, fmt.Errorf("failed to lookup cloudflare zone %s: %w", domain, err)
	}
	var payload cloudflareZoneListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return cloudflareZone{}, fmt.Errorf("failed to decode cloudflare zone response: %w", err)
	}
	if status != http.StatusOK || !payload.Success {
		return cloudflareZone{}, fmt.Errorf("failed to lookup cloudflare zone %s: %s", domain, cloudflareErrorMessage(status, payload.Errors, body))
	}
	if len(payload.Result) == 0 {
		return cloudflareZone{}, fmt.Errorf("cloudflare zone %s not found", domain)
	}
	if len(payload.Result) > 1 {
		return cloudflareZone{}, fmt.Errorf("cloudflare zone %s matched multiple zones", domain)
	}
	zone := payload.Result[0]
	if strings.TrimSpace(zone.ID) == "" || strings.TrimSpace(zone.Account.ID) == "" {
		return cloudflareZone{}, fmt.Errorf("cloudflare zone %s response missing zone or account id", domain)
	}
	return zone, nil
}

func lookupNamedTunnel(ctx context.Context, apiToken, accountID, tunnelName string) (cloudflareTunnel, error) {
	accountID = strings.TrimSpace(accountID)
	tunnelName = strings.TrimSpace(tunnelName)
	if accountID == "" {
		return cloudflareTunnel{}, errors.New("cloudflare account id is required")
	}
	if tunnelName == "" {
		return cloudflareTunnel{}, errors.New("cloudflare tunnel name is required")
	}
	endpoint, err := cloudflareEndpoint(
		fmt.Sprintf("/accounts/%s/cfd_tunnel", accountID),
		url.Values{"is_deleted": []string{"false"}, "name": []string{tunnelName}},
	)
	if err != nil {
		return cloudflareTunnel{}, err
	}
	body, status, err := requestCloudflareAPI(ctx, apiToken, endpoint)
	if err != nil {
		return cloudflareTunnel{}, fmt.Errorf("failed to lookup cloudflare tunnel %s: %w", tunnelName, err)
	}
	var payload cloudflareTunnelListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return cloudflareTunnel{}, fmt.Errorf("failed to decode cloudflare tunnel response: %w", err)
	}
	if status != http.StatusOK || !payload.Success {
		return cloudflareTunnel{}, fmt.Errorf("failed to lookup cloudflare tunnel %s: %s", tunnelName, cloudflareErrorMessage(status, payload.Errors, body))
	}
	if len(payload.Result) == 0 {
		return cloudflareTunnel{}, fmt.Errorf("cloudflare named tunnel %q not found", tunnelName)
	}
	if len(payload.Result) > 1 {
		return cloudflareTunnel{}, fmt.Errorf("cloudflare named tunnel %q matched multiple tunnels", tunnelName)
	}
	if payload.Result[0].ID == uuid.Nil {
		return cloudflareTunnel{}, fmt.Errorf("cloudflare named tunnel %q response missing tunnel id", tunnelName)
	}
	return payload.Result[0], nil
}

func fetchNamedTunnelToken(ctx context.Context, apiToken, accountID string, tunnelID uuid.UUID) (string, error) {
	if tunnelID == uuid.Nil {
		return "", errors.New("cloudflare tunnel id is required")
	}
	endpoint, err := cloudflareEndpoint(fmt.Sprintf("/accounts/%s/cfd_tunnel/%s/token", strings.TrimSpace(accountID), tunnelID.String()), nil)
	if err != nil {
		return "", err
	}
	body, status, err := requestCloudflareAPI(ctx, apiToken, endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to fetch cloudflare tunnel token for %s: %w", tunnelID.String(), err)
	}
	var payload cloudflareTunnelTokenResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("failed to decode cloudflare tunnel token response: %w", err)
	}
	if status != http.StatusOK || !payload.Success {
		return "", fmt.Errorf("failed to fetch cloudflare tunnel token for %s: %s", tunnelID.String(), cloudflareErrorMessage(status, payload.Errors, body))
	}
	if strings.TrimSpace(payload.Result) == "" {
		return "", fmt.Errorf("cloudflare tunnel %s token response was empty", tunnelID.String())
	}
	return strings.TrimSpace(payload.Result), nil
}

func cloudflareEndpoint(path string, query url.Values) (string, error) {
	base := strings.TrimRight(cloudflareAPIBaseURL, "/")
	endpoint, err := url.Parse(base + path)
	if err != nil {
		return "", err
	}
	if len(query) > 0 {
		endpoint.RawQuery = query.Encode()
	}
	return endpoint.String(), nil
}

func requestCloudflareAPI(ctx context.Context, apiToken, endpoint string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiToken))
	req.Header.Set("Accept", "application/json;version=1")
	req.Header.Set("User-Agent", embeddedBuildInfo.UserAgent())

	client := &http.Client{Timeout: quickTunnelTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}
	return body, resp.StatusCode, nil
}

func cloudflareErrorMessage(status int, errorsList []cloudflareAPIError, body []byte) string {
	messages := make([]string, 0, len(errorsList))
	for _, entry := range errorsList {
		if strings.TrimSpace(entry.Message) != "" {
			messages = append(messages, strings.TrimSpace(entry.Message))
		}
	}
	if len(messages) > 0 {
		return strings.Join(messages, "; ")
	}
	if len(body) > 0 {
		return fmt.Sprintf("status %d: %s", status, strings.TrimSpace(string(body)))
	}
	return fmt.Sprintf("status %d", status)
}

func newCloudflareCLIContext(ctx context.Context, localURL, hostname string) (*cli.Context, error) {
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
		"--hostname", strings.TrimSpace(hostname),
		// Prefer HTTP/2 here. QUIC has hit TLS curve negotiation failures on
		// some environments, which prevents quick tunnels from coming up.
		"--protocol", "http2",
		"--ha-connections", "1",
		"--no-autoupdate",
		"--metrics", "127.0.0.1:0",
		"--loglevel", "info",
		"--transport-loglevel", "error",
	}
	if strings.TrimSpace(hostname) == "" {
		args = []string{
			"--url", localURL,
			"--protocol", "http2",
			"--ha-connections", "1",
			"--no-autoupdate",
			"--metrics", "127.0.0.1:0",
			"--loglevel", "info",
			"--transport-loglevel", "error",
		}
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

func waitForPublicURLReady(ctx context.Context, publicURL string, timeout, interval time.Duration) error {
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if publicURL == "" {
		return errors.New("public url is required")
	}
	if timeout <= 0 {
		timeout = publicURLProbeTimeout
	}
	if interval <= 0 {
		interval = publicURLProbeInterval
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	probeURL := publicURL + "/__chatgpt2codex_probe__"
	client := &http.Client{Timeout: publicURLRequestTimeout}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error
	for {
		ready, err := probePublicURL(probeCtx, client, probeURL)
		if ready {
			return nil
		}
		if err != nil {
			if probeCtx.Err() != nil && lastErr != nil {
				return fmt.Errorf("%w; last probe error: %v", probeCtx.Err(), lastErr)
			}
			lastErr = err
		}

		select {
		case <-probeCtx.Done():
			if lastErr != nil {
				return fmt.Errorf("%w; last probe error: %v", probeCtx.Err(), lastErr)
			}
			return probeCtx.Err()
		case <-ticker.C:
		}
	}
}

func probePublicURL(ctx context.Context, client *http.Client, probeURL string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "chatgpt2codex-startup-probe")
	req.Header.Set("Openai-Conversation-Id", "startup-probe")

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 500 {
		return false, fmt.Errorf("received status %d from %s", resp.StatusCode, probeURL)
	}
	return true, nil
}
