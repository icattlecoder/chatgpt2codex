package gpt

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

func TestExtractGPTID(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  string
		isErr bool
	}{
		{
			name: "editor url",
			raw:  "https://chatgpt.com/gpts/editor/g-69de6a95a114819197df38ed79220511",
			want: "g-69de6a95a114819197df38ed79220511",
		},
		{
			name: "view url with slug",
			raw:  "https://chatgpt.com/g/g-69df0a752e248191a605e46abe4e10eb-codex-chatgpt2codex",
			want: "g-69df0a752e248191a605e46abe4e10eb",
		},
		{
			name: "text containing saved url",
			raw:  "查看 GPT https://chatgpt.com/g/g-69df0a752e248191a605e46abe4e10eb-codex-chatgpt2codex",
			want: "g-69df0a752e248191a605e46abe4e10eb",
		},
		{
			name:  "invalid url",
			raw:   "https://chatgpt.com/g/invalid",
			isErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gptID, err := extractGPTID(tt.raw)
			if tt.isErr {
				if err == nil {
					t.Fatalf("extractGPTID(%q) expected error", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractGPTID(%q) returned error: %v", tt.raw, err)
			}
			if gptID != tt.want {
				t.Fatalf("extractGPTID(%q) = %q, want %q", tt.raw, gptID, tt.want)
			}
		})
	}
}

func TestLooksLikeLoginRedirect(t *testing.T) {
	if !looksLikeLoginRedirect("https://chatgpt.com/auth/login") {
		t.Fatalf("expected auth login url to be detected")
	}
	if !looksLikeLoginRedirect("https://auth.openai.com/u/login/identifier") {
		t.Fatalf("expected auth.openai.com login url to be detected")
	}
	if looksLikeLoginRedirect("https://chatgpt.com/gpts/editor") {
		t.Fatalf("did not expect editor url to be treated as login redirect")
	}
	if looksLikeLoginRedirect("https://chatgpt.com/") {
		t.Fatalf("did not expect home url to be treated as login redirect")
	}
}

func TestDebugPauseEnabled(t *testing.T) {
	t.Setenv(chromeDebugEnv, "")
	if debugPauseEnabled() {
		t.Fatalf("expected debug pause to be disabled by default")
	}

	t.Setenv(chromeDebugEnv, "true")
	if !debugPauseEnabled() {
		t.Fatalf("expected debug pause to be enabled when env var is true")
	}

	t.Setenv(chromeDebugEnv, "1")
	if !debugPauseEnabled() {
		t.Fatalf("expected debug pause to be enabled when env var is 1")
	}
}

func TestWaitForDebugInput(t *testing.T) {
	t.Setenv(chromeDebugEnv, "true")

	originalReader := debugInputReader
	debugInputReader = bufio.NewReader(strings.NewReader("\n"))
	t.Cleanup(func() {
		debugInputReader = originalReader
	})

	if err := waitForDebugInput(context.Background(), nil, "test step"); err != nil {
		t.Fatalf("waitForDebugInput returned error: %v", err)
	}
}

func TestSetActionSchemaScriptTargetsSchemaTextarea(t *testing.T) {
	expectedSelectors := []string{
		`textarea[placeholder*="OpenAPI"]`,
		`textarea[placeholder*="schema"]`,
		`textarea[aria-label*="OpenAPI"]`,
		`textarea[name="prompt-textarea"]`,
		`textarea[aria-label*="ChatGPT"]`,
		`wcDTda_fallbackTextarea`,
	}

	for _, selector := range expectedSelectors {
		if !strings.Contains(setActionSchemaScript, selector) {
			t.Fatalf("expected setActionSchemaScript to contain %q", selector)
		}
	}

	if regexp.MustCompile(`querySelectorAll\('textarea,input,\[contenteditable="true"\],\[role="textbox"\]'\)`).MatchString(setActionSchemaScript) {
		t.Fatalf("did not expect setActionSchemaScript to search generic text inputs anymore")
	}
}

func TestExtractSavedGPTURLScriptRequiresSavedUI(t *testing.T) {
	if !strings.Contains(extractSavedGPTURLScript, `if (!isVisible(copyButton))`) {
		t.Fatalf("expected extractSavedGPTURLScript to require the saved GPT copy button")
	}
}

func TestWaitForGPTURLDoesNotUseCurrentEditorURLFallback(t *testing.T) {
	if strings.Contains(extractSavedGPTURLScript, "currentURL(") {
		t.Fatalf("did not expect extractSavedGPTURLScript to inspect current page url")
	}

	source, err := os.ReadFile("chrome.go")
	if err != nil {
		t.Fatalf("failed to read chrome.go: %v", err)
	}
	waitForGPTURLSource := extractFunctionSource(string(source), "func waitForGPTURL(")
	if strings.Contains(waitForGPTURLSource, "currentURL(") {
		t.Fatalf("did not expect waitForGPTURL to use current page url fallback")
	}
}

func TestWaitForAndSubmitUpdateWaitsForCompletionSignals(t *testing.T) {
	source, err := os.ReadFile("chrome.go")
	if err != nil {
		t.Fatalf("failed to read chrome.go: %v", err)
	}
	waitForAndSubmitUpdateSource := extractFunctionSource(string(source), "func waitForAndSubmitUpdate(")
	if strings.Contains(waitForAndSubmitUpdateSource, "chromedp.Sleep(1500*time.Millisecond)") {
		t.Fatalf("did not expect waitForAndSubmitUpdate to use only a fixed 1.5s sleep anymore")
	}
	if !strings.Contains(waitForAndSubmitUpdateSource, "updateSubmissionStatusScript") {
		t.Fatalf("expected waitForAndSubmitUpdate to wait for update completion signals")
	}
	if !strings.Contains(waitForAndSubmitUpdateSource, "postUpdateSettleDelay") {
		t.Fatalf("expected waitForAndSubmitUpdate to include a post-update settle delay")
	}
}

func TestUpdateSubmissionStatusScriptChecksPendingAndSuccessLabels(t *testing.T) {
	expectedLabels := []string{
		"updated",
		"updating",
		"saving",
	}

	for _, label := range expectedLabels {
		if !strings.Contains(updateSubmissionStatusScript, label) {
			t.Fatalf("expected updateSubmissionStatusScript to contain %q", label)
		}
	}
}

func TestChromeAutomationDefaultsToEnglishUI(t *testing.T) {
	source, err := os.ReadFile("chrome.go")
	if err != nil {
		t.Fatalf("failed to read chrome.go: %v", err)
	}
	if !strings.Contains(string(source), `chromedp.Flag("lang", chromeUILanguage)`) {
		t.Fatalf("expected Chrome allocator to pin the browser ui language to english")
	}
	if !strings.Contains(string(source), `"Accept-Language": chromeAcceptLanguage`) {
		t.Fatalf("expected browser session to send english accept-language headers")
	}
}

func extractFunctionSource(source, signature string) string {
	start := strings.Index(source, signature)
	if start < 0 {
		return ""
	}
	remaining := source[start:]
	end := strings.Index(remaining, "\nfunc ")
	if end < 0 {
		return remaining
	}
	return remaining[:end]
}

func TestGPTViewURL(t *testing.T) {
	if got := gptViewURL("g-69deff410dac8191823ad16e4ac5be64"); got != "https://chatgpt.com/g/g-69deff410dac8191823ad16e4ac5be64" {
		t.Fatalf("unexpected gpt view url %q", got)
	}
}

func TestChromeCreatorRequiresActionAPIKey(t *testing.T) {
	creator := &ChromeCreator{profileDir: t.TempDir()}

	if _, err := creator.Create(context.Background(), CreateRequest{
		GPTName:       "Codex/test",
		Instructions:  "hello",
		OpenAPISchema: "openapi: 3.1.0",
	}); err == nil || !strings.Contains(err.Error(), "action api key is required") {
		t.Fatalf("expected create to require action api key, got %v", err)
	}

	if err := creator.Update(context.Background(), UpdateRequest{
		GPTID:         "g-1234567890abcdef1234567890abcdef",
		GPTName:       "Codex/test",
		Instructions:  "hello",
		OpenAPISchema: "openapi: 3.1.0",
	}); err == nil || !strings.Contains(err.Error(), "action api key is required") {
		t.Fatalf("expected update to require action api key, got %v", err)
	}
}

func TestFilteredChromedpErrorfSuppressesUnmarshalNoise(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(originalOutput)
	})

	filteredChromedpErrorf("could not unmarshal event: %v", "parse error: expected string near offset 1108")
	if buf.Len() != 0 {
		t.Fatalf("expected chromedp unmarshal noise to be suppressed, got %q", buf.String())
	}

	filteredChromedpErrorf("browser crashed: %s", "boom")
	if !strings.Contains(buf.String(), "ERROR: browser crashed: boom") {
		t.Fatalf("expected non-noise error to be logged, got %q", buf.String())
	}
}

func TestNewBrowserContextCleanupUsesGracefulCancel(t *testing.T) {
	creator := &ChromeCreator{profileDir: t.TempDir()}
	_, cleanup := creator.newBrowserContext(context.Background())

	originalCancel := cancelBrowserContext
	var called bool
	cancelBrowserContext = func(ctx context.Context) error {
		called = true
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatalf("expected graceful shutdown context to have a deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > browserShutdownTimeout {
			t.Fatalf("unexpected shutdown timeout window: %v", remaining)
		}
		return nil
	}
	t.Cleanup(func() {
		cancelBrowserContext = originalCancel
	})

	if err := cleanup(); err != nil {
		t.Fatalf("cleanup returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected graceful chromedp cancel to be used")
	}
}

func TestNewBrowserContextCleanupDetachesFromParentCancellation(t *testing.T) {
	parentCtx, cancelParent := context.WithCancel(context.Background())
	creator := &ChromeCreator{profileDir: t.TempDir()}
	_, cleanup := creator.newBrowserContext(parentCtx)

	cancelParent()

	originalCancel := cancelBrowserContext
	var called bool
	cancelBrowserContext = func(ctx context.Context) error {
		called = true
		if err := ctx.Err(); err != nil {
			t.Fatalf("expected shutdown context to ignore parent cancellation, got %v", err)
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatalf("expected graceful shutdown context to have a deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > browserShutdownTimeout {
			t.Fatalf("unexpected shutdown timeout window: %v", remaining)
		}
		return nil
	}
	t.Cleanup(func() {
		cancelBrowserContext = originalCancel
	})

	if err := cleanup(); err != nil {
		t.Fatalf("cleanup returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected graceful chromedp cancel to be used")
	}
}

func TestNewBrowserContextCleanupSuppressesExpectedCancelErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "context canceled", err: context.Canceled},
		{name: "invalid context", err: chromedp.ErrInvalidContext},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creator := &ChromeCreator{profileDir: t.TempDir()}
			_, cleanup := creator.newBrowserContext(context.Background())

			originalCancel := cancelBrowserContext
			cancelBrowserContext = func(context.Context) error {
				return tt.err
			}
			t.Cleanup(func() {
				cancelBrowserContext = originalCancel
			})

			if err := cleanup(); err != nil {
				t.Fatalf("cleanup returned error: %v", err)
			}
		})
	}
}

func TestNewBrowserContextCleanupReturnsUnexpectedCancelErrors(t *testing.T) {
	creator := &ChromeCreator{profileDir: t.TempDir()}
	_, cleanup := creator.newBrowserContext(context.Background())

	originalCancel := cancelBrowserContext
	boom := errors.New("boom")
	cancelBrowserContext = func(context.Context) error {
		return boom
	}
	t.Cleanup(func() {
		cancelBrowserContext = originalCancel
	})

	if err := cleanup(); !errors.Is(err, boom) {
		t.Fatalf("cleanup error = %v, want %v", err, boom)
	}
}
