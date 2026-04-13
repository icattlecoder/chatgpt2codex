package proxy

import (
	"context"
	"io"
	"strings"
	"testing"
)

type fakeStarter struct {
	process Process
	err     error
}

func (f fakeStarter) Start(_ context.Context, _ string, _ ...string) (Process, error) {
	return f.process, f.err
}

type fakeProcess struct {
	stdout io.ReadCloser
	stderr io.ReadCloser
	wait   error
}

func (f fakeProcess) Stdout() io.ReadCloser { return f.stdout }
func (f fakeProcess) Stderr() io.ReadCloser { return f.stderr }
func (f fakeProcess) Wait() error           { return f.wait }
func (f fakeProcess) Kill() error           { return nil }

func TestStartParsesCloudflareURL(t *testing.T) {
	process := fakeProcess{
		stdout: io.NopCloser(strings.NewReader("Starting tunnel\nhttps://demo.trycloudflare.com\n")),
		stderr: io.NopCloser(strings.NewReader("")),
	}
	session, err := Start(context.Background(), fakeStarter{process: process}, "cloudflare", "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if session.PublicURL != "https://demo.trycloudflare.com" {
		t.Fatalf("unexpected public url %q", session.PublicURL)
	}
}

func TestStartParsesNgrokURL(t *testing.T) {
	process := fakeProcess{
		stdout: io.NopCloser(strings.NewReader("{\"msg\":\"started tunnel\",\"url\":\"https://demo.ngrok-free.app\"}\n")),
		stderr: io.NopCloser(strings.NewReader("")),
	}
	session, err := Start(context.Background(), fakeStarter{process: process}, "ngrok", "http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if session.PublicURL != "https://demo.ngrok-free.app" {
		t.Fatalf("unexpected public url %q", session.PublicURL)
	}
}
