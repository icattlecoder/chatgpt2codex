package gpt

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	chromeDebugEnv           = "CHROME_DEBUG"
	chromeUILanguage         = "en-US"
	chromeLocaleOverride     = "en_US"
	chromeAcceptLanguage     = "en-US,en;q=0.9"
	homeURL                  = "https://chatgpt.com/"
	editorURL                = "https://chatgpt.com/gpts/editor"
	gptURLPrefix             = "https://chatgpt.com/g/"
	overallCreateTimeout     = 10 * time.Minute
	editorReadyTimeout       = 10 * time.Minute
	actionReadyTimeout       = 45 * time.Second
	saveReadyTimeout         = 2 * time.Minute
	postSaveSettleDelay      = 1500 * time.Millisecond
	postUpdateSettleDelay    = 5 * time.Second
	loginDetectTimeout       = 8 * time.Second
	stepPollInterval         = 1500 * time.Millisecond
	browserShutdownTimeout   = 10 * time.Second
	loginButtonSelector      = `[data-testid="login-button"]`
	configureButtonSelector  = `[data-testid="gizmo-editor-configure-button"]`
	nameInputSelector        = `[data-testid="gizmo-name-input"]`
	descriptionInputSelector = `[data-testid="gizmo-description-input"]`
	instructionsSelector     = `[data-testid="gizmo-instructions-input"]`
	saveButtonSelector       = `[data-testid="save-gizmo-button"]`
	savedGPTURLButtonSel     = `[data-testid="copy-saved-gpt-url-button"]`
)

var gptIDPattern = regexp.MustCompile(`(?i)/(?:gpts/editor|g)/(g-[0-9a-z]{32})(?:-[^/?#\s"'<>]+)?`)
var debugInputReader = bufio.NewReader(os.Stdin)
var actionCreateLabels = []string{"Create new action"}
var createButtonLabels = []string{"Create"}
var privateVisibilityLabels = []string{"Only me"}
var updateButtonLabels = []string{"Update"}
var openBrowserURL = func(rawURL string) error {
	cmd := exec.Command("open", rawURL)
	return cmd.Run()
}
var cancelBrowserContext = chromedp.Cancel

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
	if strings.TrimSpace(request.ActionAPIKey) == "" {
		return CreateResult{}, errors.New("action api key is required")
	}

	if err := os.MkdirAll(c.profileDir, 0o755); err != nil {
		return CreateResult{}, err
	}

	browserCtx, closeBrowser := c.newBrowserContext(ctx)
	defer func() {
		if err := closeBrowser(); err != nil {
			log.Printf("WARN: failed to close Chrome cleanly: %v", err)
		}
	}()

	runCtx, cancelRun := context.WithTimeout(browserCtx, overallCreateTimeout)
	defer cancelRun()

	if err := initializeEnglishBrowserSession(runCtx); err != nil {
		return CreateResult{}, fmt.Errorf("failed to initialize english browser session: %w", err)
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, "Open ChatGPT and enter the GPT editor", func() error {
		writeProgress(request.ProgressWriter, "Opening Chrome for GPT creation.\n")
		return openEditorPageWithLogin(runCtx, request.ProgressWriter, editorURL)
	}); err != nil {
		return CreateResult{}, err
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, "Wait for the GPT editor to become ready", func() error {
		return waitForEditorReady(runCtx, request.ProgressWriter)
	}); err != nil {
		return CreateResult{}, err
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, "Open the Configure tab", func() error {
		return waitForAndClickSelector(runCtx, actionReadyTimeout, configureButtonSelector)
	}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to open configure tab: %w", err)
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, "Fill GPT name", func() error {
		return waitForAndSetValue(runCtx, nameInputSelector, request.GPTName)
	}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to set gpt name: %w", err)
	}
	if err := runDebugStep(runCtx, request.ProgressWriter, "Fill GPT description", func() error {
		return waitForAndSetValue(runCtx, descriptionInputSelector, GPTDescriptionForWorkspace(request.Workspace))
	}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to set gpt description: %w", err)
	}
	if err := runDebugStep(runCtx, request.ProgressWriter, "Fill GPT instructions", func() error {
		return waitForAndSetValue(runCtx, instructionsSelector, request.Instructions)
	}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to set gpt instructions: %w", err)
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, fmt.Sprintf("Select recommended model: %s", request.RecommendedModel), func() error {
		return waitForAndSelectModel(runCtx, request.RecommendedModel)
	}); err != nil {
		return CreateResult{}, err
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, "Open the GPT action editor", func() error {
		return waitForAndOpenOrCreateActionEditor(runCtx, actionReadyTimeout)
	}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to open action editor: %w", err)
	}
	if err := runDebugStep(runCtx, request.ProgressWriter, "Fill the action OpenAPI schema", func() error {
		return waitForAndSetActionSchema(runCtx, request.OpenAPISchema)
	}); err != nil {
		return CreateResult{}, err
	}
	if err := waitForAndConfigureActionAuth(runCtx, request.ProgressWriter, request.ActionAPIKey); err != nil {
		return CreateResult{}, fmt.Errorf("failed to configure action authentication: %w", err)
	}
	if err := runDebugStep(runCtx, request.ProgressWriter, "Open the GPT visibility dialog", func() error {
		return waitForAndOpenVisibilityDialog(runCtx, saveReadyTimeout)
	}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to open gpt visibility dialog: %w", err)
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, "Set GPT visibility to private", func() error {
		return waitForAndClickByText(runCtx, saveReadyTimeout, privateVisibilityLabels)
	}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to choose private visibility: %w", err)
	}
	if err := runDebugStep(runCtx, request.ProgressWriter, "Save the GPT", func() error {
		return waitForAndClickSelector(runCtx, saveReadyTimeout, saveButtonSelector)
	}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to save gpt: %w", err)
	}

	var finalURL string
	if err := runDebugStep(runCtx, request.ProgressWriter, "Wait for the saved GPT URL", func() error {
		var err error
		finalURL, err = waitForGPTURL(runCtx)
		return err
	}); err != nil {
		return CreateResult{}, err
	}
	gptID, err := extractGPTID(finalURL)
	if err != nil {
		return CreateResult{}, err
	}
	viewURL := gptViewURL(gptID)

	if err := runDebugStep(runCtx, request.ProgressWriter, "Open the created GPT in the default browser", func() error {
		writeProgress(request.ProgressWriter, "Opening created GPT in default browser: %s\n", viewURL)
		return openBrowserURL(viewURL)
	}); err != nil {
		return CreateResult{}, fmt.Errorf("failed to open created gpt: %w", err)
	}

	writeProgress(request.ProgressWriter, "GPT created successfully: %s\n", gptID)
	return CreateResult{
		GPTID:     gptID,
		EditorURL: viewURL,
	}, nil
}

func (c *ChromeCreator) Update(ctx context.Context, request UpdateRequest) error {
	gptID := strings.TrimSpace(request.GPTID)
	if gptID == "" {
		return errors.New("gpt id is required")
	}
	if strings.TrimSpace(request.GPTName) == "" {
		return errors.New("gpt name is required")
	}
	if strings.TrimSpace(request.Instructions) == "" {
		return errors.New("gpt instructions are required")
	}
	if strings.TrimSpace(request.OpenAPISchema) == "" {
		return errors.New("openapi schema is required")
	}
	if strings.TrimSpace(request.RecommendedModel) == "" {
		request.RecommendedModel = DefaultRecommendedModel
	}
	if strings.TrimSpace(request.ActionAPIKey) == "" {
		return errors.New("action api key is required")
	}

	if err := os.MkdirAll(c.profileDir, 0o755); err != nil {
		return err
	}

	browserCtx, closeBrowser := c.newBrowserContext(ctx)
	defer func() {
		if err := closeBrowser(); err != nil {
			log.Printf("WARN: failed to close Chrome cleanly: %v", err)
		}
	}()

	runCtx, cancelRun := context.WithTimeout(browserCtx, overallCreateTimeout)
	defer cancelRun()

	if err := initializeEnglishBrowserSession(runCtx); err != nil {
		return fmt.Errorf("failed to initialize english browser session: %w", err)
	}

	targetEditorURL := gptEditorURL(gptID)
	if err := runDebugStep(runCtx, request.ProgressWriter, "Open ChatGPT and enter the GPT editor", func() error {
		writeProgress(request.ProgressWriter, "Opening Chrome for GPT update.\n")
		return openEditorPageWithLogin(runCtx, request.ProgressWriter, targetEditorURL)
	}); err != nil {
		return err
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, "Wait for the GPT editor to become ready", func() error {
		return waitForEditorReady(runCtx, request.ProgressWriter)
	}); err != nil {
		return err
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, "Open the Configure tab", func() error {
		return waitForAndClickSelector(runCtx, actionReadyTimeout, configureButtonSelector)
	}); err != nil {
		return fmt.Errorf("failed to open configure tab: %w", err)
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, "Fill GPT name", func() error {
		return waitForAndSetValue(runCtx, nameInputSelector, request.GPTName)
	}); err != nil {
		return fmt.Errorf("failed to set gpt name: %w", err)
	}
	if err := runDebugStep(runCtx, request.ProgressWriter, "Fill GPT description", func() error {
		return waitForAndSetValue(runCtx, descriptionInputSelector, GPTDescriptionForWorkspace(request.Workspace))
	}); err != nil {
		return fmt.Errorf("failed to set gpt description: %w", err)
	}
	if err := runDebugStep(runCtx, request.ProgressWriter, "Fill GPT instructions", func() error {
		return waitForAndSetValue(runCtx, instructionsSelector, request.Instructions)
	}); err != nil {
		return fmt.Errorf("failed to set gpt instructions: %w", err)
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, fmt.Sprintf("Select recommended model: %s", request.RecommendedModel), func() error {
		return waitForAndSelectModel(runCtx, request.RecommendedModel)
	}); err != nil {
		return err
	}

	if err := runDebugStep(runCtx, request.ProgressWriter, "Open the GPT action editor", func() error {
		return waitForAndOpenOrCreateActionEditor(runCtx, actionReadyTimeout)
	}); err != nil {
		return fmt.Errorf("failed to open action editor: %w", err)
	}
	if err := runDebugStep(runCtx, request.ProgressWriter, "Fill the action OpenAPI schema", func() error {
		return waitForAndSetActionSchema(runCtx, request.OpenAPISchema)
	}); err != nil {
		return err
	}
	if err := waitForAndConfigureActionAuth(runCtx, request.ProgressWriter, request.ActionAPIKey); err != nil {
		return fmt.Errorf("failed to configure action authentication: %w", err)
	}
	if err := runDebugStep(runCtx, request.ProgressWriter, "Update the GPT", func() error {
		return waitForAndSubmitUpdate(runCtx)
	}); err != nil {
		return fmt.Errorf("failed to update gpt: %w", err)
	}

	viewURL := gptViewURL(gptID)
	if err := runDebugStep(runCtx, request.ProgressWriter, "Open the updated GPT in the default browser", func() error {
		writeProgress(request.ProgressWriter, "Opening updated GPT in default browser: %s\n", viewURL)
		return openBrowserURL(viewURL)
	}); err != nil {
		return fmt.Errorf("failed to open updated gpt: %w", err)
	}

	writeProgress(request.ProgressWriter, "GPT updated successfully: %s\n", gptID)
	return nil
}

func (c *ChromeCreator) newBrowserContext(ctx context.Context) (context.Context, func() error) {
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
		chromedp.Flag("lang", chromeUILanguage),
		chromedp.UserDataDir(c.profileDir),
		chromedp.Flag("headless", false),
		chromedp.Flag("start-maximized", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
	}

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, opts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx, chromedp.WithErrorf(filteredChromedpErrorf))
	cleanup := func() error {
		defer cancelBrowser()
		defer cancelAllocator()

		// Detach shutdown from the caller's cancellation so Chrome still gets a
		// dedicated grace period to exit cleanly when the outer workflow times out.
		shutdownCtx, cancelShutdown := context.WithTimeout(context.WithoutCancel(browserCtx), browserShutdownTimeout)
		defer cancelShutdown()

		err := cancelBrowserContext(shutdownCtx)
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, chromedp.ErrInvalidContext) {
			return err
		}
		return nil
	}
	return browserCtx, cleanup
}

func waitForEditorReady(ctx context.Context, writer io.Writer) error {
	noticedLogin := false

	err := waitUntil(ctx, editorReadyTimeout, func() (bool, error) {
		currentURL, err := currentURL(ctx)
		if err != nil {
			return false, nil
		}

		configureVisible, configureErr := visibleSelector(ctx, configureButtonSelector)
		nameVisible, nameErr := visibleSelector(ctx, nameInputSelector)
		if strings.HasPrefix(currentURL, editorURL) &&
			((configureErr == nil && configureVisible) || (nameErr == nil && nameVisible)) {
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

func openEditorPageWithLogin(ctx context.Context, writer io.Writer, targetURL string) error {
	if err := runDebugStep(ctx, writer, "Open the ChatGPT home page", func() error {
		writeProgress(writer, "Opening ChatGPT home page: %s\n", homeURL)
		return chromedp.Run(ctx,
			chromedp.Navigate(homeURL),
			chromedp.WaitReady("body", chromedp.ByQuery),
		)
	}); err != nil {
		return fmt.Errorf("failed to open ChatGPT home page: %w", err)
	}

	loginStarted, err := maybeStartLogin(ctx, writer)
	if err != nil {
		return err
	}
	if loginStarted {
		if err := runDebugStep(ctx, writer, "Wait for ChatGPT login to complete", func() error {
			return waitForLoginCompletion(ctx)
		}); err != nil {
			return err
		}
	}

	if err := runDebugStep(ctx, writer, "Open the GPT editor page", func() error {
		writeProgress(writer, "Opening GPT editor: %s\n", targetURL)
		return chromedp.Run(ctx,
			chromedp.Navigate(targetURL),
			chromedp.WaitReady("body", chromedp.ByQuery),
		)
	}); err != nil {
		return fmt.Errorf("failed to open GPT editor: %w", err)
	}
	return nil
}

func maybeStartLogin(ctx context.Context, writer io.Writer) (bool, error) {
	clicked := false
	err := runDebugStep(ctx, writer, "Check whether login is required and click the login button if needed", func() error {
		return waitUntil(ctx, loginDetectTimeout, func() (bool, error) {
			visible, err := visibleSelector(ctx, loginButtonSelector)
			if err != nil || !visible {
				return false, nil
			}

			ok, err := clickSelector(ctx, loginButtonSelector)
			if err != nil || !ok {
				return false, nil
			}

			writeProgress(writer, "ChatGPT login is required. Complete login in the opened Chrome window, then automation will continue.\n")
			clicked = true
			return true, nil
		})
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return false, nil
		}
		return false, err
	}
	return clicked, nil
}

func waitForLoginCompletion(ctx context.Context) error {
	err := waitUntil(ctx, editorReadyTimeout, func() (bool, error) {
		current, err := currentURL(ctx)
		if err != nil {
			return false, nil
		}
		if looksLikeLoginRedirect(current) {
			return false, nil
		}

		visible, err := visibleSelector(ctx, loginButtonSelector)
		if err == nil && visible {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("timed out waiting for ChatGPT login to complete")
		}
		return err
	}
	return nil
}

func initializeEnglishBrowserSession(ctx context.Context) error {
	headers := network.Headers{
		"Accept-Language": chromeAcceptLanguage,
	}
	return chromedp.Run(ctx,
		network.Enable(),
		network.SetExtraHTTPHeaders(headers),
		emulation.SetLocaleOverride().WithLocale(chromeLocaleOverride),
	)
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

func waitForAndConfigureActionAuth(ctx context.Context, writer io.Writer, apiKey string) error {
	if err := runDebugStep(ctx, writer, "Open the action authentication dialog", func() error {
		return waitForAndOpenActionAuthDialog(ctx)
	}); err != nil {
		return err
	}
	if err := runDebugStep(ctx, writer, "Select API key authentication type", func() error {
		return waitForActionAuthStep(ctx, "selectServiceHTTP", nil, "failed to select API key authentication type")
	}); err != nil {
		return err
	}
	if err := runDebugStep(ctx, writer, "Fill the action API key", func() error {
		return waitForActionAuthStep(ctx, "fillAPIKey", apiKey, "failed to fill action api key")
	}); err != nil {
		return err
	}
	if err := runDebugStep(ctx, writer, "Select Bearer authentication scheme", func() error {
		return waitForActionAuthStep(ctx, "selectBearer", nil, "failed to select bearer authentication scheme")
	}); err != nil {
		return err
	}
	if err := runDebugStep(ctx, writer, "Save action authentication", func() error {
		return waitForActionAuthStep(ctx, "save", nil, "failed to save action authentication")
	}); err != nil {
		return err
	}
	return nil
}

func waitForAndOpenActionAuthDialog(ctx context.Context) error {
	return waitForActionAuthStep(ctx, "openDialog", nil, "failed to open action authentication dialog")
}

func waitForActionAuthStep(ctx context.Context, step string, value any, errMessage string) error {
	var result domActionResult
	err := waitUntil(ctx, saveReadyTimeout, func() (bool, error) {
		runErr := evaluateFunction(ctx, configureActionAuthStepScript, &result, step, value)
		if runErr != nil {
			return false, nil
		}
		return result.OK, nil
	})
	if err != nil {
		details := make([]string, 0, 2)
		if strings.TrimSpace(result.Message) != "" {
			details = append(details, strings.TrimSpace(result.Message))
		}
		if len(result.Candidates) > 0 {
			details = append(details, "visible candidates: "+strings.Join(result.Candidates, ", "))
		}
		if len(details) > 0 {
			return fmt.Errorf("%s; %s", errMessage, strings.Join(details, "; "))
		}
		return errors.New(errMessage)
	}
	return nil
}

func waitForAndOpenOrCreateActionEditor(ctx context.Context, timeout time.Duration) error {
	var result domActionResult
	err := waitUntil(ctx, timeout, func() (bool, error) {
		runErr := evaluateFunction(ctx, openOrCreateActionEditorScript, &result, actionCreateLabels)
		if runErr != nil {
			return false, nil
		}
		return result.OK, nil
	})
	if err != nil {
		if len(result.Candidates) > 0 {
			return fmt.Errorf("failed to open action editor; visible candidates: %s", strings.Join(result.Candidates, ", "))
		}
		return errors.New("failed to open action editor")
	}
	return nil
}

func waitForAndSubmitUpdate(ctx context.Context) error {
	if err := waitForAndClickByText(ctx, saveReadyTimeout, updateButtonLabels); err != nil {
		return err
	}

	var result domActionResult
	err := waitUntil(ctx, saveReadyTimeout, func() (bool, error) {
		runErr := evaluateFunction(ctx, updateSubmissionStatusScript, &result)
		if runErr != nil {
			return false, nil
		}
		return result.OK, nil
	})
	if err != nil {
		if strings.TrimSpace(result.Message) != "" {
			return fmt.Errorf("timed out waiting for GPT update to finish; %s", strings.TrimSpace(result.Message))
		}
		return errors.New("timed out waiting for GPT update to finish")
	}
	return chromedp.Run(ctx, chromedp.Sleep(postUpdateSettleDelay))
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

func waitForAndOpenVisibilityDialog(ctx context.Context, timeout time.Duration) error {
	var result domActionResult
	err := waitUntil(ctx, timeout, func() (bool, error) {
		visible, visibleErr := visibleText(ctx, privateVisibilityLabels)
		if visibleErr == nil && visible {
			return true, nil
		}

		runErr := evaluateFunction(ctx, clickByTextScript, &result, createButtonLabels)
		if runErr != nil {
			return false, nil
		}
		return false, nil
	})
	if err != nil {
		if len(result.Candidates) > 0 {
			return fmt.Errorf("could not open visibility dialog via %q; visible candidates: %s", createButtonLabels, strings.Join(result.Candidates, ", "))
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
	var result domActionResult
	err := waitUntil(ctx, saveReadyTimeout, func() (bool, error) {
		if runErr := evaluateFunction(ctx, extractSavedGPTURLScript, &result, savedGPTURLButtonSel, gptURLPrefix); runErr == nil {
			if gptID, extractErr := extractGPTID(result.Value); extractErr == nil && gptID != "" {
				finalURL = strings.TrimSpace(result.Value)
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		if len(result.Candidates) > 0 {
			return "", fmt.Errorf("failed to obtain GPT url after save; visible candidates: %s", strings.Join(result.Candidates, ", "))
		}
		return "", errors.New("failed to obtain GPT url after save")
	}
	if err := chromedp.Run(ctx, chromedp.Sleep(postSaveSettleDelay)); err != nil {
		return "", err
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

func gptViewURL(gptID string) string {
	return gptURLPrefix + strings.TrimSpace(gptID)
}

func gptEditorURL(gptID string) string {
	return editorURL + "/" + strings.TrimSpace(gptID)
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

func visibleText(ctx context.Context, labels []string) (bool, error) {
	var visible bool
	err := evaluateFunction(ctx, visibleTextScript, &visible, labels)
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

func runDebugStep(ctx context.Context, writer io.Writer, label string, action func() error) error {
	if err := waitForDebugInput(ctx, writer, label); err != nil {
		return err
	}
	return action()
}

func waitForDebugInput(ctx context.Context, writer io.Writer, label string) error {
	if !debugPauseEnabled() {
		return nil
	}

	writeProgress(writer, "Debug pause: %s. Press Enter to continue.\n", label)

	resultCh := make(chan error, 1)
	go func() {
		_, err := debugInputReader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			resultCh <- fmt.Errorf("failed to read debug input: %w", err)
			return
		}
		resultCh <- nil
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-resultCh:
		return err
	}
}

func debugPauseEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(chromeDebugEnv)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func filteredChromedpErrorf(format string, args ...interface{}) {
	if strings.Contains(format, "could not unmarshal event:") {
		return
	}
	log.Printf("ERROR: "+format, args...)
}

func looksLikeLoginRedirect(rawURL string) bool {
	lowered := strings.ToLower(strings.TrimSpace(rawURL))
	if lowered == "" {
		return false
	}
	normalizedHomeURL := strings.TrimSuffix(strings.ToLower(homeURL), "/")
	if strings.HasPrefix(lowered, editorURL) {
		return false
	}
	if lowered == normalizedHomeURL || lowered == normalizedHomeURL+"/" ||
		strings.HasPrefix(lowered, normalizedHomeURL+"/?") ||
		strings.HasPrefix(lowered, normalizedHomeURL+"/#") {
		return false
	}
	return strings.Contains(lowered, "/auth") ||
		strings.Contains(lowered, "/login") ||
		strings.Contains(lowered, "auth.openai.com") ||
		strings.Contains(lowered, "auth0.openai.com")
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

const visibleTextScript = `function(labels) {
	const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim().toLowerCase();
	const isVisible = (element) => {
		if (!element) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.display === 'none' || style.visibility === 'hidden') return false;
		const rect = element.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	const wanted = new Set((labels || []).map(normalize));
	const elements = Array.from(document.querySelectorAll('button,[role="button"],[role="radio"]')).filter(isVisible);
	return elements.some((element) => wanted.has(normalize(element.innerText || element.textContent)));
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
	const isInteractable = (element) => {
		if (!isVisible(element)) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.pointerEvents === 'none') return false;
		if (element.matches(':disabled')) return false;
		if (element.getAttribute('aria-disabled') === 'true') return false;
		if ('disabled' in element && element.disabled) return false;
		return true;
	};
	const elements = Array.from(document.querySelectorAll('button,[role="button"],[role="radio"]')).filter(isVisible);
	const candidates = elements.slice(0, 25).map((element) => (element.innerText || element.textContent || '').replace(/\s+/g, ' ').trim()).filter(Boolean);
	const wanted = (labels || []).map(normalize);
	for (const label of wanted) {
		const exact = elements.find((element) => normalize(element.innerText || element.textContent) === label && isInteractable(element));
		if (exact) {
			exact.click();
			return { ok: true };
		}
	}
	return { ok: false, candidates };
}`

const updateSubmissionStatusScript = `function() {
	const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim().toLowerCase();
	const isVisible = (element) => {
		if (!element) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.display === 'none' || style.visibility === 'hidden') return false;
		const rect = element.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	const isInteractable = (element) => {
		if (!isVisible(element)) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.pointerEvents === 'none') return false;
		if (element.matches(':disabled')) return false;
		if (element.getAttribute('aria-disabled') === 'true') return false;
		if ('disabled' in element && element.disabled) return false;
		return true;
	};
	const textIncludes = (labels) => {
		const wanted = (labels || []).map(normalize);
		if (wanted.length === 0) return false;
		return Array.from(document.querySelectorAll('body *')).some((element) => {
			if (!isVisible(element)) return false;
			const text = normalize(element.innerText || element.textContent);
			if (!text) return false;
			return wanted.some((label) => text.includes(label));
		});
	};
	const buttons = Array.from(document.querySelectorAll('button,[role="button"]')).filter(isVisible);
	const updateButton = buttons.find((element) => {
		const text = normalize(element.innerText || element.textContent);
		return text === 'update';
	});
	if (textIncludes(['updated', 'successfully updated'])) {
		return { ok: true, value: 'completed', message: 'update success message is visible' };
	}
	if (!updateButton) {
		return { ok: true, value: 'completed', message: 'update button is no longer visible' };
	}
	if (isInteractable(updateButton)) {
		return { ok: false, value: 'ready', message: 'update button is still interactable' };
	}
	if (textIncludes(['updating', 'saving'])) {
		return { ok: false, value: 'pending', message: 'update is still being processed' };
	}
	return { ok: true, value: 'completed', message: 'update button is no longer interactable' };
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
	const preferredSelectors = [
		'textarea[placeholder*="OpenAPI"]',
		'textarea[placeholder*="schema"]',
		'textarea[aria-label*="OpenAPI"]',
	];
	const isChatTextarea = (element) => {
		if (!element) return false;
		if (element.matches('textarea[name="prompt-textarea"],textarea[aria-label*="ChatGPT"],textarea[data-virtualkeyboard="true"]')) {
			return true;
		}
		return element.classList.contains('wcDTda_fallbackTextarea');
	};
	for (const selector of preferredSelectors) {
		const preferred = document.querySelector(selector);
		if (!preferred || !isVisible(preferred) || isChatTextarea(preferred)) {
			continue;
		}
		if (!setValue(preferred, schema)) {
			return { ok: false, candidates: [selector] };
		}
		return { ok: true, candidates: [selector] };
	}
	const elements = Array.from(document.querySelectorAll('textarea')).filter((element) => {
		if (!isVisible(element)) return false;
		if (isChatTextarea(element)) return false;
		if (element.matches('[data-testid="gizmo-name-input"],[data-testid="gizmo-instructions-input"]')) return false;
		const text = normalize([
			element.getAttribute('aria-label'),
			element.getAttribute('placeholder'),
			element.closest('section,div,form,dialog') ? element.closest('section,div,form,dialog').innerText : '',
		].join(' ').slice(0, 500));
		return text.includes('openapi') || text.includes('schema');
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
		if (context.includes('yaml')) score += 40;
		if (context.includes('json')) score += 20;
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

const configureActionAuthStepScript = `function(step, value) {
	const normalize = (input) => (input || '').replace(/\s+/g, ' ').trim().toLowerCase();
	const describe = (element) => (element?.innerText || element?.textContent || '').replace(/\s+/g, ' ').trim();
	const isVisible = (element) => {
		if (!element) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.display === 'none' || style.visibility === 'hidden') return false;
		const rect = element.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	const isInteractable = (element) => {
		if (!isVisible(element)) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.pointerEvents === 'none') return false;
		if (element.matches(':disabled')) return false;
		if (element.getAttribute('aria-disabled') === 'true') return false;
		if ('disabled' in element && element.disabled) return false;
		return true;
	};
	const textEquals = (text, labels) => {
		const normalized = normalize(text);
		return (labels || []).some((label) => normalized === normalize(label));
	};
	const uniqueStrings = (values) => Array.from(new Set((values || []).filter(Boolean)));
	const uniqueElements = (values) => Array.from(new Set((values || []).filter(Boolean)));
	const candidates = uniqueStrings(
		Array.from(document.querySelectorAll('label,button,[role="button"],[role="radio"],input,div'))
			.filter(isVisible)
			.map(describe),
	).slice(0, 60);
	const click = (element) => {
		if (!element) return false;
		element.scrollIntoView?.({ block: 'center', inline: 'center' });
		if (isInteractable(element)) {
			element.click();
			return true;
		}
		const nestedTarget = element.querySelector?.('button,[role="button"],[role="radio"]');
		if (nestedTarget && isInteractable(nestedTarget)) {
			nestedTarget.click();
			return true;
		}
		if (isVisible(element)) {
			element.click();
			return true;
		}
		return false;
	};
	const setValue = (element, nextValue) => {
		if (!element) return false;
		element.focus();
		if (element instanceof HTMLInputElement) {
			const descriptor = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value');
			if (descriptor && descriptor.set) {
				descriptor.set.call(element, nextValue);
			} else {
				element.value = nextValue;
			}
		} else if (element instanceof HTMLTextAreaElement) {
			const descriptor = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value');
			if (descriptor && descriptor.set) {
				descriptor.set.call(element, nextValue);
			} else {
				element.value = nextValue;
			}
		} else {
			return false;
		}
		element.dispatchEvent(new Event('input', { bubbles: true }));
		element.dispatchEvent(new Event('change', { bubbles: true }));
		return element.value === nextValue;
	};
	const findExactTextNodes = (labels, selectors = 'label,button,[role="button"],[role="radio"],div,span') => {
		return Array.from(document.querySelectorAll(selectors)).filter((element) => isVisible(element) && textEquals(describe(element), labels));
	};
	const findButtonByLabels = (root, labels) => {
		const searchRoot = root || document;
		return Array.from(searchRoot.querySelectorAll('button,[role="button"]')).find((element) => isInteractable(element) && textEquals(describe(element), labels)) || null;
	};
	const hasAuthMarkers = (root) => {
		if (!root) return false;
		if (root.querySelector('input[type="password"],input[placeholder="[HIDDEN]"],[role="radio"][value="service_http"],[role="radio"][value="bearer"]')) {
			return true;
		}
		return findExactTextNodes(['Authentication Type'], 'label,div,span').some((node) => root.contains(node));
	};
	const isAuthDialogRoot = (root) => {
		if (!root || !hasAuthMarkers(root)) return false;
		return !!findButtonByLabels(root, ['Save']) && !!findButtonByLabels(root, ['Cancel']);
	};
	const findAuthDialog = () => {
		const saveButtons = Array.from(document.querySelectorAll('button,[role="button"]')).filter((element) => isInteractable(element) && textEquals(describe(element), ['Save']));
		for (const saveButton of saveButtons) {
			let current = saveButton.parentElement;
			while (current && current !== document.body) {
				if (isVisible(current) && isAuthDialogRoot(current)) {
					return current;
				}
				current = current.parentElement;
			}
		}
		const inputs = Array.from(document.querySelectorAll('input[type="password"],input[placeholder="[HIDDEN]"]')).filter(isVisible);
		for (const input of inputs) {
			let current = input.parentElement;
			while (current && current !== document.body) {
				if (isVisible(current) && isAuthDialogRoot(current)) {
					return current;
				}
				current = current.parentElement;
			}
		}
		return null;
	};
	const findAuthTrigger = () => {
		const labelNodes = findExactTextNodes(['Authentication'], 'label,div,span');
		for (const labelNode of labelNodes) {
			const roots = uniqueElements([
				labelNode.parentElement,
				labelNode.parentElement?.parentElement,
				labelNode.closest('div'),
				labelNode.closest('section'),
			]);
			for (const root of roots) {
				const children = Array.from(root.children || []).filter((child) => isVisible(child) && !child.contains(labelNode));
				for (const child of children) {
					const className = typeof child.className === 'string' ? child.className : '';
					if (child.querySelector('button,[role="button"]') || child.getAttribute('role') === 'button' || className.includes('cursor-pointer') || (className.includes('border') && className.includes('rounded'))) {
						return child;
					}
				}
			}
		}
		return null;
	};
	const findRadio = (root, radioValue, labels) => {
		const searchRoot = root || document;
		const byValue = Array.from(searchRoot.querySelectorAll('[role="radio"]')).find((element) => isVisible(element) && normalize(element.getAttribute('value')) === normalize(radioValue));
		if (byValue) {
			return byValue;
		}
		const labelNodes = findExactTextNodes(labels, 'label,button,[role="radio"],div,span').filter((node) => searchRoot === document || searchRoot.contains(node));
		for (const labelNode of labelNodes) {
			const controlId = labelNode.getAttribute?.('for');
			const control = controlId ? document.getElementById(controlId) : null;
			const target = labelNode.querySelector?.('button,[role="radio"]') || control || labelNode;
			if (target && isVisible(target)) {
				return target;
			}
		}
		return null;
	};
	const ensureRadioSelected = (root, radioValue, labels) => {
		const radio = findRadio(root, radioValue, labels);
		if (!radio) return false;
		if (radio.getAttribute('aria-checked') === 'true') return true;
		click(radio);
		return false;
	};
	const findAPIKeyInput = (root) => {
		const searchRoot = root || document;
		const directMatch = Array.from(searchRoot.querySelectorAll('input[type="password"],input[placeholder="[HIDDEN]"]')).find(isVisible);
		if (directMatch) {
			return directMatch;
		}
		const byHint = Array.from(searchRoot.querySelectorAll('input:not([type="hidden"])')).find((element) => {
			if (!isVisible(element)) return false;
			const text = normalize([
				element.getAttribute('aria-label'),
				element.getAttribute('placeholder'),
				element.closest('section,div,form,dialog') ? element.closest('section,div,form,dialog').innerText : '',
			].join(' '));
			return text.includes('api key');
		});
		if (byHint) {
			return byHint;
		}
		const labelNodes = findExactTextNodes(['API Key'], 'label').filter((node) => searchRoot === document || searchRoot.contains(node));
		for (const labelNode of labelNodes) {
			let current = labelNode.parentElement;
			while (current) {
				const input = Array.from(current.querySelectorAll('input:not([type="hidden"])')).find(isVisible);
				if (input) {
					return input;
				}
				if (current === searchRoot) break;
				current = current.parentElement;
			}
		}
		return Array.from(searchRoot.querySelectorAll('input:not([type="hidden"])')).find(isVisible) || null;
	};

	if (step === 'openDialog') {
		if (findAuthDialog()) {
			return { ok: true, message: 'dialog open', candidates };
		}
		const trigger = findAuthTrigger();
		if (!trigger) {
			return { ok: false, message: 'authentication trigger not found', candidates };
		}
		click(trigger);
		return { ok: false, message: 'authentication trigger clicked', candidates };
	}

	const dialog = findAuthDialog();
	if (!dialog) {
		if (step === 'save') {
			return { ok: true, message: 'dialog closed', candidates };
		}
		return { ok: false, message: 'authentication dialog not open', candidates };
	}

	if (step === 'selectServiceHTTP') {
		return { ok: ensureRadioSelected(dialog, 'service_http', ['API Key']), candidates };
	}
	if (step === 'fillAPIKey') {
		const input = findAPIKeyInput(dialog);
		if (!input) {
			return { ok: false, message: 'api key input not found', candidates };
		}
		if (input.value === value) {
			return { ok: true, candidates };
		}
		return { ok: setValue(input, value), candidates };
	}
	if (step === 'selectBearer') {
		return { ok: ensureRadioSelected(dialog, 'bearer', ['Bearer']), candidates };
	}
	if (step === 'save') {
		const saveButton = findButtonByLabels(dialog, ['Save']);
		if (!saveButton) {
			return { ok: false, message: 'save button not found', candidates };
		}
		click(saveButton);
		return { ok: false, message: 'save clicked', candidates };
	}

	return { ok: false, message: 'unknown action auth step', candidates };
}`

const openOrCreateActionEditorScript = `function(createLabels) {
	const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim().toLowerCase();
	const isVisible = (element) => {
		if (!element) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.display === 'none' || style.visibility === 'hidden') return false;
		const rect = element.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	const isInteractable = (element) => {
		if (!isVisible(element)) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.pointerEvents === 'none') return false;
		if (element.matches(':disabled')) return false;
		if (element.getAttribute('aria-disabled') === 'true') return false;
		if ('disabled' in element && element.disabled) return false;
		return true;
	};
	const describe = (element) => (element.innerText || element.textContent || '').replace(/\s+/g, ' ').trim();
	const wanted = new Set((createLabels || []).map(normalize));
	const buttons = Array.from(document.querySelectorAll('button,[role="button"]')).filter(isVisible);
	const candidates = buttons.slice(0, 25).map(describe).filter(Boolean);
	const createButton = buttons.find((element) => wanted.has(normalize(describe(element))) && isInteractable(element));
	if (!createButton) {
		return { ok: false, candidates };
	}
	const findExistingRows = (root) => {
		if (!root) return [];
		return Array.from(root.querySelectorAll('div')).filter((element) => {
			if (!isInteractable(element)) return false;
			if (element.contains(createButton)) return false;
			const text = describe(element);
			if (!text) return false;
			if (wanted.has(normalize(text))) return false;
			const className = typeof element.className === 'string' ? element.className : '';
			if (!className.includes('border') || !className.includes('rounded')) return false;
			return !!element.querySelector('button');
		}).sort((left, right) => left.getBoundingClientRect().top - right.getBoundingClientRect().top);
	};
	const searchRoots = [
		createButton.previousElementSibling,
		createButton.parentElement,
		createButton.closest('.space-y-1'),
	].filter(Boolean);
	for (const root of searchRoots) {
		const rows = findExistingRows(root);
		if (rows.length === 0) continue;
		rows[0].click();
		return { ok: true, message: 'existing', candidates: rows.slice(0, 5).map(describe).filter(Boolean) };
	}
	createButton.click();
	return { ok: true, message: 'create', candidates };
}`

const extractSavedGPTURLScript = `function(copyButtonSelector, expectedPrefix) {
	const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim();
	const isVisible = (element) => {
		if (!element) return false;
		const style = window.getComputedStyle(element);
		if (!style || style.display === 'none' || style.visibility === 'hidden') return false;
		const rect = element.getBoundingClientRect();
		return rect.width > 0 && rect.height > 0;
	};
	const urls = [];
	const pushURL = (value) => {
		const normalized = normalize(value);
		if (!normalized) return;
		const match = normalized.match(/https:\/\/chatgpt\.com\/g\/g-[^\/\s"'?#<]+/i);
		if (!match) return;
		const url = match[0];
		if (!urls.includes(url)) {
			urls.push(url);
		}
	};

	const copyButton = document.querySelector(copyButtonSelector);
	if (!isVisible(copyButton)) {
		return { ok: false, candidates: [] };
	}
	const row = copyButton.closest('div');
	if (row) {
		pushURL(row.innerText || row.textContent || '');
		for (const child of Array.from(row.children || [])) {
			if (child === copyButton) continue;
			pushURL(child.innerText || child.textContent || '');
		}
	}

	const root = copyButton.closest('[role="dialog"]') || copyButton.closest('main') || document.body;
	for (const link of Array.from(root.querySelectorAll('a[href]'))) {
		if (!isVisible(link)) continue;
		pushURL(link.href || '');
	}

	for (const link of Array.from(document.querySelectorAll('a[href]'))) {
		if (!isVisible(link)) continue;
		const href = normalize(link.href || '');
		if (expectedPrefix && !href.startsWith(expectedPrefix)) continue;
		pushURL(href);
	}

	if (urls.length > 0) {
		return { ok: true, value: urls[0], candidates: urls };
	}
	return { ok: false, candidates: [] };
}`
