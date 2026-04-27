package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudflare/cloudflared/connection"
	"github.com/google/uuid"
)

func TestStartRejectsUnsupportedProxy(t *testing.T) {
	t.Parallel()

	_, err := Start(context.Background(), StartRequest{Kind: "ngrok", LocalURL: "http://127.0.0.1:8080"})
	if err == nil {
		t.Fatal("expected Start to reject ngrok")
	}
	if !strings.Contains(err.Error(), `unsupported proxy "ngrok"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartUsesEmbeddedCloudflareTunnel(t *testing.T) {
	original := startEmbeddedCloudflareTunnel
	originalNamed := startNamedCloudflareTunnel
	t.Cleanup(func() {
		startEmbeddedCloudflareTunnel = original
		startNamedCloudflareTunnel = originalNamed
	})

	called := false
	startEmbeddedCloudflareTunnel = func(_ context.Context, localURL string) (*Session, error) {
		called = true
		if localURL != "http://127.0.0.1:8080" {
			t.Fatalf("unexpected local url %q", localURL)
		}
		return &Session{PublicURL: "https://demo.trycloudflare.com"}, nil
	}
	startNamedCloudflareTunnel = func(_ context.Context, _ StartRequest) (*Session, error) {
		t.Fatal("did not expect named tunnel starter")
		return nil, nil
	}

	session, err := Start(context.Background(), StartRequest{
		Kind:     "cloudflare",
		LocalURL: "http://127.0.0.1:8080",
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if !called {
		t.Fatal("expected embedded cloudflare tunnel starter to be called")
	}
	if session.PublicURL != "https://demo.trycloudflare.com" {
		t.Fatalf("unexpected public url %q", session.PublicURL)
	}
}

func TestStartUsesNamedCloudflareTunnelWhenHostnameProvided(t *testing.T) {
	originalQuick := startEmbeddedCloudflareTunnel
	originalNamed := startNamedCloudflareTunnel
	t.Cleanup(func() {
		startEmbeddedCloudflareTunnel = originalQuick
		startNamedCloudflareTunnel = originalNamed
	})

	startEmbeddedCloudflareTunnel = func(_ context.Context, _ string) (*Session, error) {
		t.Fatal("did not expect quick tunnel starter")
		return nil, nil
	}
	startNamedCloudflareTunnel = func(_ context.Context, request StartRequest) (*Session, error) {
		if request.LocalURL != "http://127.0.0.1:8080" {
			t.Fatalf("unexpected local url %q", request.LocalURL)
		}
		if request.Hostname != "demo.example.com" {
			t.Fatalf("unexpected hostname %q", request.Hostname)
		}
		if request.Domain != "example.com" || request.APIToken != "api-token" {
			t.Fatalf("expected cloudflare config to be forwarded, got %#v", request)
		}
		return &Session{PublicURL: "https://" + request.Hostname}, nil
	}

	session, err := Start(context.Background(), StartRequest{
		Kind:     "cloudflare",
		LocalURL: "http://127.0.0.1:8080",
		Hostname: "demo.example.com",
		Domain:   "example.com",
		APIToken: "api-token",
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if session.PublicURL != "https://demo.example.com" {
		t.Fatalf("unexpected public url %q", session.PublicURL)
	}
}

func TestRequestQuickTunnelParsesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if r.URL.Path != "/tunnel" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Fatal("expected user-agent header")
		}

		payload := quickTunnelResponse{
			Success: true,
			Result: quickTunnelResult{
				ID:         "11111111-1111-1111-1111-111111111111",
				Hostname:   "demo.trycloudflare.com",
				AccountTag: "acct-123",
				Secret:     []byte("secret-bytes"),
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	original := quickTunnelServiceURL
	quickTunnelServiceURL = server.URL
	defer func() {
		quickTunnelServiceURL = original
	}()

	session, err := requestQuickTunnel(context.Background())
	if err != nil {
		t.Fatalf("requestQuickTunnel returned error: %v", err)
	}
	if session.Hostname != "demo.trycloudflare.com" {
		t.Fatalf("unexpected hostname %q", session.Hostname)
	}
	if session.PublicURL != "https://demo.trycloudflare.com" {
		t.Fatalf("unexpected public url %q", session.PublicURL)
	}
	if session.Credentials.AccountTag != "acct-123" {
		t.Fatalf("unexpected account tag %q", session.Credentials.AccountTag)
	}
	if got := string(session.Credentials.TunnelSecret); got != "secret-bytes" {
		t.Fatalf("unexpected tunnel secret %q", got)
	}
}

func TestNewCloudflareCLIContextPrefersHTTP2(t *testing.T) {
	t.Parallel()

	cliCtx, err := newCloudflareCLIContext(context.Background(), "http://127.0.0.1:8080", "")
	if err != nil {
		t.Fatalf("newCloudflareCLIContext returned error: %v", err)
	}

	if got := cliCtx.String("url"); got != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected url %q", got)
	}
	if got := cliCtx.String("protocol"); got != "http2" {
		t.Fatalf("unexpected protocol %q", got)
	}
	if got := cliCtx.String("hostname"); got != "" {
		t.Fatalf("expected empty hostname, got %q", got)
	}
}

func TestNewCloudflareCLIContextIncludesHostname(t *testing.T) {
	t.Parallel()

	cliCtx, err := newCloudflareCLIContext(context.Background(), "http://127.0.0.1:8080", "demo.example.com")
	if err != nil {
		t.Fatalf("newCloudflareCLIContext returned error: %v", err)
	}
	if got := cliCtx.String("hostname"); got != "demo.example.com" {
		t.Fatalf("unexpected hostname %q", got)
	}
}

func TestStartupLogBufferDetectsReadyLine(t *testing.T) {
	buffer := newStartupLogBuffer(3, "Registered tunnel connection")
	if _, err := buffer.Write([]byte("first\nRegistered tunnel connection\n")); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	select {
	case <-buffer.Ready():
	default:
		t.Fatal("expected ready signal")
	}

	if got := buffer.String(); !strings.Contains(got, "Registered tunnel connection") {
		t.Fatalf("unexpected buffer contents %q", got)
	}
}

func TestEnsureNamedTunnelRouteRequiresAPIConfig(t *testing.T) {
	err := ensureNamedTunnelRoute(context.Background(), "", "", uuid.MustParse("11111111-1111-1111-1111-111111111111"), "acct", "demo.example.com")
	if err == nil {
		t.Fatalf("expected missing env vars to fail")
	}
}

func TestRunNamedCloudflareTunnelRequiresDomainAndAPIToken(t *testing.T) {
	_, err := runNamedCloudflareTunnel(context.Background(), StartRequest{
		LocalURL: "http://127.0.0.1:8080",
		Hostname: "demo.example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "cloudflare domain") {
		t.Fatalf("expected missing domain error, got %v", err)
	}

	_, err = runNamedCloudflareTunnel(context.Background(), StartRequest{
		LocalURL: "http://127.0.0.1:8080",
		Hostname: "demo.example.com",
		Domain:   "example.com",
	})
	if err == nil || !strings.Contains(err.Error(), "cloudflare api token") {
		t.Fatalf("expected missing api token error, got %v", err)
	}
}

func TestRunNamedCloudflareTunnelConfiguresRemoteIngress(t *testing.T) {
	originalRoute := routeNamedTunnelHostname
	originalConfigure := configureNamedTunnelIngress
	t.Cleanup(func() {
		routeNamedTunnelHostname = originalRoute
		configureNamedTunnelIngress = originalConfigure
	})

	tunnelID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tokenValue, err := connection.TunnelToken{
		AccountTag:   "acct-123",
		TunnelSecret: []byte("secret"),
		TunnelID:     tunnelID,
	}.Encode()
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	originalAPIBaseURL := cloudflareAPIBaseURL
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zones":
			if got := r.URL.Query().Get("name"); got != "example.com" {
				t.Fatalf("unexpected zone query %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"result": []map[string]any{{
					"id":   "zone-123",
					"name": "example.com",
					"account": map[string]any{
						"id": "acct-123",
					},
				}},
			})
		case "/accounts/acct-123/cfd_tunnel":
			if got := r.URL.Query().Get("name"); got != defaultNamedTunnelName {
				t.Fatalf("unexpected tunnel name query %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"result": []map[string]any{{
					"id":   tunnelID.String(),
					"name": defaultNamedTunnelName,
				}},
			})
		case "/accounts/acct-123/cfd_tunnel/" + tunnelID.String() + "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"result":  tokenValue,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		cloudflareAPIBaseURL = originalAPIBaseURL
		apiServer.Close()
	})
	cloudflareAPIBaseURL = apiServer.URL

	var configured bool
	routeNamedTunnelHostname = func(_ context.Context, apiToken, zoneID string, gotTunnelID uuid.UUID, accountTag, hostname string) error {
		if apiToken != "api-token" {
			t.Fatalf("unexpected route api token %q", apiToken)
		}
		if zoneID != "zone-123" {
			t.Fatalf("unexpected route zone id %q", zoneID)
		}
		if gotTunnelID != tunnelID {
			t.Fatalf("unexpected route tunnel id %q", gotTunnelID)
		}
		if accountTag != "acct-123" {
			t.Fatalf("unexpected route account tag %q", accountTag)
		}
		if hostname != "demo.example.com" {
			t.Fatalf("unexpected route hostname %q", hostname)
		}
		return nil
	}
	configureNamedTunnelIngress = func(_ context.Context, apiToken string, gotTunnelID uuid.UUID, accountTag, hostname, localURL string) error {
		configured = true
		if apiToken != "api-token" {
			t.Fatalf("unexpected api token %q", apiToken)
		}
		if gotTunnelID != tunnelID {
			t.Fatalf("unexpected tunnel id %q", gotTunnelID)
		}
		if accountTag != "acct-123" {
			t.Fatalf("unexpected account tag %q", accountTag)
		}
		if hostname != "demo.example.com" {
			t.Fatalf("unexpected hostname %q", hostname)
		}
		if localURL != "http://127.0.0.1:8080" {
			t.Fatalf("unexpected local url %q", localURL)
		}
		return errors.New("boom")
	}

	_, err = runNamedCloudflareTunnel(context.Background(), StartRequest{
		LocalURL: "http://127.0.0.1:8080",
		Hostname: "demo.example.com",
		Domain:   "example.com",
		APIToken: "api-token",
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected configure error, got %v", err)
	}
	if !configured {
		t.Fatal("expected named tunnel ingress to be configured")
	}
}

func TestWaitForPublicURLReadyWaitsUntilNon5xx(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.URL.Path != "/__chatgpt2codex_probe__" {
			t.Fatalf("unexpected probe path %q", r.URL.Path)
		}
		if got := r.Header.Get("Openai-Conversation-Id"); got != "startup-probe" {
			t.Fatalf("unexpected conversation header %q", got)
		}
		if attempts < 3 {
			http.Error(w, "warming up", http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	err := waitForPublicURLReady(context.Background(), server.URL, time.Second, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("waitForPublicURLReady returned error: %v", err)
	}
	if attempts < 3 {
		t.Fatalf("expected multiple probe attempts, got %d", attempts)
	}
}

func TestWaitForPublicURLReadyReturnsLastProbeErrorOnTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "warming up", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := waitForPublicURLReady(context.Background(), server.URL, 80*time.Millisecond, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected waitForPublicURLReady to fail")
	}
	if !strings.Contains(err.Error(), "received status 503") {
		t.Fatalf("expected last probe status in error, got %v", err)
	}
}
