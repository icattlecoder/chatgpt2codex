package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStartRejectsUnsupportedProxy(t *testing.T) {
	t.Parallel()

	_, err := Start(context.Background(), "ngrok", "http://127.0.0.1:8080")
	if err == nil {
		t.Fatal("expected Start to reject ngrok")
	}
	if !strings.Contains(err.Error(), `unsupported proxy "ngrok"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStartUsesEmbeddedCloudflareTunnel(t *testing.T) {
	original := startEmbeddedCloudflareTunnel
	t.Cleanup(func() {
		startEmbeddedCloudflareTunnel = original
	})

	called := false
	startEmbeddedCloudflareTunnel = func(_ context.Context, localURL string) (*Session, error) {
		called = true
		if localURL != "http://127.0.0.1:8080" {
			t.Fatalf("unexpected local url %q", localURL)
		}
		return &Session{PublicURL: "https://demo.trycloudflare.com"}, nil
	}

	session, err := Start(context.Background(), "cloudflare", "http://127.0.0.1:8080")
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

	cliCtx, err := newCloudflareCLIContext(context.Background(), "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("newCloudflareCLIContext returned error: %v", err)
	}

	if got := cliCtx.String("url"); got != "http://127.0.0.1:8080" {
		t.Fatalf("unexpected url %q", got)
	}
	if got := cliCtx.String("protocol"); got != "http2" {
		t.Fatalf("unexpected protocol %q", got)
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
