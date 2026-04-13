package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Process interface {
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Wait() error
	Kill() error
}

type Starter interface {
	Start(ctx context.Context, name string, args ...string) (Process, error)
}

type osStarter struct{}

type osProcess struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr io.ReadCloser
}

type Session struct {
	PublicURL string
	process   Process
}

var (
	cloudflareURL = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.trycloudflare\.com`)
	ngrokURL      = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.(?:ngrok-free\.app|ngrok\.io|ngrok\.app)`)
)

func DefaultStarter() Starter {
	return osStarter{}
}

func (osStarter) Start(ctx context.Context, name string, args ...string) (Process, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &osProcess{cmd: cmd, stdout: stdout, stderr: stderr}, nil
}

func (p *osProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *osProcess) Stderr() io.ReadCloser { return p.stderr }
func (p *osProcess) Wait() error           { return p.cmd.Wait() }
func (p *osProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func Start(ctx context.Context, starter Starter, kind, localURL string) (*Session, error) {
	if starter == nil {
		starter = DefaultStarter()
	}

	name, args, matcher, err := commandFor(kind, localURL)
	if err != nil {
		return nil, err
	}

	process, err := starter.Start(ctx, name, args...)
	if err != nil {
		return nil, err
	}

	lines := make(chan string, 32)
	waitErr := make(chan error, 1)
	var readWait sync.WaitGroup
	readOutput := func(reader io.Reader) {
		defer readWait.Done()
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}
	readWait.Add(2)
	go readOutput(process.Stdout())
	go readOutput(process.Stderr())
	go func() {
		readWait.Wait()
		close(lines)
	}()
	go func() {
		waitErr <- process.Wait()
	}()

	timeout := time.NewTimer(20 * time.Second)
	defer timeout.Stop()

	lastLines := make([]string, 0, 10)
	processExited := false
	processErr := error(nil)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				if processExited {
					if processErr == nil {
						processErr = errors.New("proxy process exited before publishing a URL")
					}
					_ = process.Kill()
					return nil, fmt.Errorf("proxy startup failed: %w. output: %s", processErr, strings.Join(lastLines, " | "))
				}
				lines = nil
				continue
			}
			if line == "" {
				continue
			}
			lastLines = append(lastLines, line)
			if len(lastLines) > 10 {
				lastLines = lastLines[len(lastLines)-10:]
			}
			if match := matcher.FindString(line); match != "" {
				return &Session{PublicURL: match, process: process}, nil
			}
		case err := <-waitErr:
			processExited = true
			processErr = err
			if lines == nil {
				if processErr == nil {
					processErr = errors.New("proxy process exited before publishing a URL")
				}
				_ = process.Kill()
				return nil, fmt.Errorf("proxy startup failed: %w. output: %s", processErr, strings.Join(lastLines, " | "))
			}
		case <-timeout.C:
			_ = process.Kill()
			return nil, fmt.Errorf("proxy startup timed out waiting for a public URL. output: %s", strings.Join(lastLines, " | "))
		case <-ctx.Done():
			_ = process.Kill()
			return nil, ctx.Err()
		}
	}
}

func commandFor(kind, localURL string) (string, []string, *regexp.Regexp, error) {
	switch kind {
	case "cloudflare":
		return "cloudflared", []string{"tunnel", "--url", localURL}, cloudflareURL, nil
	case "ngrok":
		return "ngrok", []string{"http", "--log=stdout", "--log-format=json", localURL}, ngrokURL, nil
	default:
		return "", nil, nil, fmt.Errorf("unsupported proxy %q", kind)
	}
}

func (s *Session) Close() error {
	if s == nil || s.process == nil {
		return nil
	}
	return s.process.Kill()
}
