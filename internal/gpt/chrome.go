package gpt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	editorURL            = "https://chatgpt.com/gpts/editor"
	overallCreateTimeout = 10 * time.Minute
	editorReadyTimeout   = 10 * time.Minute
	actionReadyTimeout   = 45 * time.Second
	saveReadyTimeout     = 2 * time.Minute
	stepPollInterval     = 1500 * time.Millisecond
	nameInputSelector    = `[data-testid="gizmo-name-input"]`
	instructionsSelector = `[data-testid="gizmo-instructions-input"]`
	saveButtonSelector   = `[data-testid="save-gizmo-button"]`
)

var gptIDPattern = regexp.MustCompile(`/gpts/editor/(g-[^/?#]+)`)

type ChromeCreator struct {
	profileDir string
}

type domActionResult struct {
	OK         bool     `json:"ok"`
	Message    string   `json:"message"`
	Candidates []string `json:"candidates"`
	Value      string   `json:"value"`
	Label      string   `json:"label"`
}

func NewChromeCreator(profileDir string) (*ChromeCreator, error) {
	if strings.TrimSpace(profileDir) == "" {
		return nil, errors.New("chrome profile dir is required")
	}
	return &ChromeCreator{profileDir: profileDir}, nil
}

func (c *ChromeCreator) Create(ctx context.Context, request CreateRequest) (CreateResult, error) {
	if strings.TrimSpace(request.GPTName) == "" {
		return CreateResult{}, errors.New("gpt name is required")
	}
	if strings.TrimSpace(request.Instructions) == "" {
		return CreateResult{}, errors.New("gpt instructions are required")
	}
	if strings.TrimSpace(request.OpenAPISchema) == "" {
		return CreateResult{}, errors.New("openapi schema is required")
	}
	if strings.TrimSpace(request.RecommendedModel) == "" {
		request.RecommendedModel = DefaultRecommendedModel
	}

	if err := os.MkdirAll(c.profileDir, 0o755); err != nil {
		return CreateResult{}, err
	}

	browserCtx := c.newBrowserContext(ctx)

	runCtx, cancelRun := context.WithTimeout(browserCtx, overallCreateTimeout)
	defer cancelRun()

	writeProgress(request.ProgressWriter, "Opening Chrome for GPT creation: %s\n", editorURL)
	if err := chromedp.Run(runCtx,
		chromedp.Navigate(editorURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	); err != nil {
		return CreateResult{}, fmt.Errorf("failed to open GPT editor: %w", err)
	}

	if err := waitForEditorReady(runCtx, request.ProgressWriter); err != nil {
		return CreateResult{}, err
	}

	writeProgress(request.ProgressWriter, "Filling GPT name and instructions.\n")
	if err := waitForAndSetValue(runCtx, nameInputSelector, request.GPTName); err != nil {
		return CreateResult{}, fmt.Errorf("failed to set gpt name: %w", err)
	}
	if err := waitForAndSetValue(runCtx, instructionsSelector, request.Instructions); err != nil {
		return CreateResult{}, fmt.Errorf("failed to set gpt instructions: %w", err)
	}

	writeProgress(request.ProgressWriter, "Selecting recommended model: %s\n", request.RecommendedModel)
	if err := waitForAndSelectModel(runCtx, request.RecommendedModel); err != nil {
		return CreateResult{}, err
	}

	writeProgress(request.ProgressWriter, "Creating GPT action.\n")
	if err := waitForAndClickByText(runCtx, actionReadyTimeout, []string{"创建新操作", "Create new action", "Action"}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to open action editor: %w", err)
	}
	if err := waitForAndSetActionSchema(runCtx, request.OpenAPISchema); err != nil {
		return CreateResult{}, err
	}
	if err := waitForAndClickByText(runCtx, actionReadyTimeout, []string{"创建", "Create"}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to create action: %w", err)
	}

	writeProgress(request.ProgressWriter, "Setting GPT visibility to private and saving.\n")
	if err := waitForAndClickByText(runCtx, saveReadyTimeout, []string{"只有我", "Only me"}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to choose private visibility: %w", err)
	}
	if err := waitForAndClickSelector(runCtx, saveReadyTimeout, saveButtonSelector); err != nil {
		return CreateResult{}, fmt.Errorf("failed to save gpt: %w", err)
	}

	finalURL, err := waitForGPTURL(runCtx)
	if err != nil {
		return CreateResult{}, err
	}
	gptID, err := extractGPTID(finalURL)
	if err != nil {
		return CreateResult{}, err
	}

	writeProgress(request.ProgressWriter, "GPT created successfully: %s\n", gptID)
	return CreateResult{
		GPTID:     gptID,
		EditorURL: finalURL,
	}, nil
}

func (c *ChromeCreator) newBrowserContext(ctx context.Context) context.Context {
	// chromedp's default allocator options are tuned for automated tests and include
	// mock credential storage flags. Those break persistent ChatGPT login state on
	// macOS, so keep the useful browser stability flags but let Chrome use its real
	// profile/keychain integration.
	opts := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("enable-features", "NetworkService,NetworkServiceInProcess"),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-breakpad", true),
		chromedp.Flag("disable-client-side-phishing-detection", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-features", "site-per-process,TranslateUI,BlinkGenPropertyTrees"),
		chromedp.Flag("disable-hang-monitor", true),
		chromedp.Flag("disable-ipc-flooding-protection", true),
		chromedp.Flag("disable-popup-blocking", false),
		chromedp.Flag("disable-prompt-on-repost", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("force-color-profile", "srgb"),
		chromedp.Flag("metrics-recording-only", true),
		chromedp.Flag("safebrowsing-disable-auto-update", true),
		chromedp.Flag("enable-automation", true),
		chromedp.UserDataDir(c.profileDir),
		chromedp.Flag("headless", false),
		chromedp.Flag("start-maximized", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	}

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, opts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	go func() {
		<-ctx.Done()
		cancelBrowser()
		cancelAllocator()
	}()
	return browserCtx
}

func waitForEditorReady(ctx context.Context, writer io.Writer) error {
	noticedLogin := false

	err := waitUntil(ctx, editorReadyTimeout, func() (bool, error) {
		currentURL, err := currentURL(ctx)
		if err != nil {
			return false, nil
		}

		visible, err := visibleSelector(ctx, nameInputSelector)
		if err == nil && strings.HasPrefix(currentURL, editorURL) && visible {
			return true, nil
		}

		if !noticedLogin && looksLikeLoginRedirect(currentURL) {
			writeProgress(writer, "ChatGPT login is required. Complete login in the opened Chrome window, then automation will continue.\n")
			noticedLogin = true
		}
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("timed out waiting for ChatGPT GPT editor to become ready")
		}
		return err
	}
	return nil
}

func waitForAndSetValue(ctx context.Context, selector, value string) error {
	return waitUntil(ctx, actionReadyTimeout, func() (bool, error) {
		ok, err := setElementValue(ctx, selector, value)
		if err != nil {
			return false, nil
		}
		return ok, nil
	})
}

func waitForAndSelectModel(ctx context.Context, model string) error {
	var result domActionResult
	err := waitUntil(ctx, actionReadyTimeout, func() (bool, error) {
		runErr := evaluateFunction(ctx, selectModelScript, &result, model)
		if runErr != nil {
			return false, nil
		}
		return result.OK, nil
	})
	if err != nil {
		if len(result.Candidates) > 0 {
			return fmt.Errorf("failed to select recommended model %q: available options: %s", model, strings.Join(result.Candidates, ", "))
		}
		return fmt.Errorf("failed to select recommended model %q", model)
	}
	return nil
}

func waitForAndSetActionSchema(ctx context.Context, schema string) error {
	var result domActionResult
	err := waitUntil(ctx, actionReadyTimeout, func() (bool, error) {
		runErr := evaluateFunction(ctx, setActionSchemaScript, &result, schema)
		if runErr != nil {
			return false, nil
		}
		return result.OK, nil
	})
	if err != nil {
		if len(result.Candidates) > 0 {
			return fmt.Errorf("failed to locate action schema input: candidates=%s", strings.Join(result.Candidates, ", "))
		}
		return errors.New("failed to locate action schema input")
	}
	return nil
}

func waitForAndClickByText(ctx context.Context, timeout time.Duration, labels []string) error {
	var result domActionResult
	err := waitUntil(ctx, timeout, func() (bool, error) {
		runErr := evaluateFunction(ctx, clickByTextScript, &result, labels)
		if runErr != nil {
			return false, nil
		}
		return result.OK, nil
	})
	if err != nil {
		if len(result.Candidates) > 0 {
			return fmt.Errorf("could not click any of %q; visible candidates: %s", labels, strings.Join(result.Candidates, ", "))
		}
		return err
	}
	return nil
}

func waitForAndClickSelector(ctx context.Context, timeout time.Duration, selector string) error {
	return waitUntil(ctx, timeout, func() (bool, error) {
		ok, err := clickSelector(ctx, selector)
		if err != nil {
			return false, nil
		}
		return ok, nil
	})
}

func waitForGPTURL(ctx context.Context) (string, error) {
	var finalURL string
	err := waitUntil(ctx, saveReadyTimeout, func() (bool, error) {
		current, err := currentURL(ctx)
		if err != nil {
			return false, nil
		}
		if gptIDPattern.MatchString(current) {
			finalURL = current
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return "", errors.New("failed to obtain GPT id from browser URL after save")
	}
	return finalURL, nil
}

func extractGPTID(rawURL string) (string, error) {
	matches := gptIDPattern.FindStringSubmatch(strings.TrimSpace(rawURL))
	if len(matches) != 2 {
		return "", fmt.Errorf("could not extract gpt id from url %q", rawURL)
	}
	return matches[1], nil
}

func currentURL(ctx context.Context) (string, error) {
	var location string
	if err := chromedp.Run(ctx, chromedp.Location(&location)); err != nil {
		return "", err
	}
	return location, nil
}

func visibleSelector(ctx context.Context, selector string) (bool, error) {
	var visible bool
	err := evaluateFunction(ctx, visibleSelectorScript, &visible, selector)
	return visible, err
}

func setElementValue(ctx context.Context, selector, value string) (bool, error) {
	var result domActionResult
	err := evaluateFunction(ctx, setValueScript, &result, selector, value)
	if err != nil {
		return false, err
	}
	return result.OK, nil
}

func clickSelector(ctx context.Context, selector string) (bool, error) {
	var result domActionResult
	err := evaluateFunction(ctx, clickSelectorScript, &result, selector)
	if err != nil {
		return false, err
	}
	return result.OK, nil
}

func evaluateFunction(ctx context.Context, fn string, out any, args ...any) error {
	marshaledArgs := make([]string, 0, len(args))
	for _, arg := range args {
		data, err := json.Marshal(arg)
		if err != nil {
			return err
		}
		marshaledArgs = append(marshaledArgs, string(data))
	}

	expression := fmt.Sprintf("(%s)(%s)", fn, strings.Join(marshaledArgs, ","))
	return chromedp.Run(ctx, chromedp.Evaluate(expression, out))
}

func waitUntil(parent context.Context, timeout time.Duration, condition func() (bool, error)) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	ticker := time.NewTicker(stepPollInterval)
	defer ticker.Stop()

	for {
		ok, err := condition()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func looksLikeLoginRedirect(rawURL string) bool {
	lowered := strings.ToLower(strings.TrimSpace(rawURL))
	if lowered == "" {
		return false
	}
	if strings.HasPrefix(lowered, editorURL) {
		return false
	}
	return strings.Contains(lowered, "/auth") || strings.Contains(lowered, "/login") || strings.Contains(lowered, "chatgpt.com")
}

func writeProgress(writer io.Writer, format string, args ...any) {
	if writer == nil {
		return
	}
	_, _ = fmt.Fprintf(writer, format, args...)
}

const visibleSelectorScript = `function(selector) {
	const isVisible = (element) => {
		if (!element) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.display === 'none' || style.visibility === 'hidden') return false;
		const rect = element.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	return isVisible(document.querySelector(selector));
}`

const setValueScript = `function(selector, value) {
	const element = document.querySelector(selector);
	if (!element) {
		return { ok: false };
	}
	element.focus();
	if (element instanceof HTMLInputElement) {
		const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value');
		if (descriptor && descriptor.set) {
			descriptor.set.call(element, value);
		} else {
			element.value = value;
		}
	} else if (element instanceof HTMLTextAreaElement) {
		const descriptor = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value');
		if (descriptor && descriptor.set) {
			descriptor.set.call(element, value);
		} else {
			element.value = value;
		}
	} else {
		return { ok: false };
	}
	element.dispatchEvent(new Event('input', { bubbles: true }));
	element.dispatchEvent(new Event('change', { bubbles: true }));
	return { ok: true };
}`

const clickSelectorScript = `function(selector) {
	const element = document.querySelector(selector);
	if (!element) {
		return { ok: false };
	}
	const style = window.getComputedStyle(element);
	const rect = element.getBoundingClientRect();
	if (!style || style.display === 'none' || style.visibility === 'hidden' || rect.width === 0 || rect.height === 0) {
		return { ok: false };
	}
	if ('disabled' in element && element.disabled) {
		return { ok: false };
	}
	element.click();
	return { ok: true };
}`

const clickByTextScript = `function(labels) {
	const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim().toLowerCase();
	const isVisible = (element) => {
		if (!element) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.display === 'none' || style.visibility === 'hidden') return false;
		const rect = element.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	const elements = Array.from(document.querySelectorAll('button,[role="button"],[role="radio"]')).filter(isVisible);
	const candidates = elements.slice(0, 25).map((element) => (element.innerText || element.textContent || '').replace(/\s+/g, ' ').trim()).filter(Boolean);
	const wanted = (labels || []).map(normalize);
	for (const label of wanted) {
		const exact = elements.find((element) => normalize(element.innerText || element.textContent) === label);
		if (exact) {
			exact.click();
			return { ok: true };
		}
	}
	return { ok: false, candidates };
}`

const selectModelScript = `function(requestedModel) {
	const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim().toLowerCase();
	const isVisible = (element) => {
		if (!element) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.display === 'none' || style.visibility === 'hidden') return false;
		const rect = element.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	const requested = normalize(requestedModel);
	const selects = Array.from(document.querySelectorAll('select')).filter(isVisible);
	const available = [];
	for (const select of selects) {
		for (const option of Array.from(select.options || [])) {
			available.push((option.textContent || '').replace(/\s+/g, ' ').trim());
		}
		const option = Array.from(select.options || []).find((entry) => {
			return normalize(entry.value) === requested || normalize(entry.textContent) === requested;
		});
		if (!option) continue;
		select.value = option.value;
		select.dispatchEvent(new Event('input', { bubbles: true }));
		select.dispatchEvent(new Event('change', { bubbles: true }));
		return { ok: true, value: option.value, label: (option.textContent || '').replace(/\s+/g, ' ').trim() };
	}
	return { ok: false, candidates: available };
}`

const setActionSchemaScript = `function(schema) {
	const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim().toLowerCase();
	const isVisible = (element) => {
		if (!element) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.display === 'none' || style.visibility === 'hidden') return false;
		const rect = element.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	const setValue = (element, value) => {
		element.focus();
		if (element instanceof HTMLInputElement) {
			const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value');
			if (descriptor && descriptor.set) {
				descriptor.set.call(element, value);
			} else {
				element.value = value;
			}
		} else if (element instanceof HTMLTextAreaElement) {
			const descriptor = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value');
			if (descriptor && descriptor.set) {
				descriptor.set.call(element, value);
			} else {
				element.value = value;
			}
		} else if (element.isContentEditable) {
			element.textContent = value;
		} else {
			return false;
		}
		element.dispatchEvent(new Event('input', { bubbles: true }));
		element.dispatchEvent(new Event('change', { bubbles: true }));
		return true;
	};
	const elements = Array.from(document.querySelectorAll('textarea,input,[contenteditable="true"],[role="textbox"]')).filter((element) => {
		if (!isVisible(element)) return false;
		if (element.matches('[data-testid="gizmo-name-input"],[data-testid="gizmo-instructions-input"]')) return false;
		return true;
	});
	const candidates = elements.map((element) => {
		const rect = element.getBoundingClientRect();
		const context = normalize([
			element.getAttribute('aria-label'),
			element.getAttribute('placeholder'),
			element.closest('section,div,form,dialog') ? element.closest('section,div,form,dialog').innerText : '',
		].join(' ').slice(0, 500));
		let score = 0;
		if (context.includes('openapi')) score += 100;
		if (context.includes('schema')) score += 80;
		if (context.includes('架构')) score += 80;
		if (context.includes('yaml')) score += 40;
		if (element.tagName === 'TEXTAREA') score += 20;
		score += Math.min((rect.width * rect.height) / 1000, 50);
		return {
			element,
			description: (element.getAttribute('aria-label') || element.getAttribute('placeholder') || element.tagName).trim(),
			score,
		};
	}).sort((left, right) => right.score - left.score);
	if (candidates.length === 0) {
		return { ok: false };
	}
	const descriptions = candidates.slice(0, 5).map((candidate) => candidate.description || candidate.element.tagName);
	const target = candidates[0].element;
	if (!setValue(target, schema)) {
		return { ok: false, candidates: descriptions };
	}
	return { ok: true, candidates: descriptions };
}`
