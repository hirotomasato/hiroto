package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

// Browser session (one per agent process).
var (
	browserCtx    context.Context
	browserCancel context.CancelFunc
	browserMu     sync.Mutex
)

func registerBrowserCDP(r *Registry) {
	r.Register(&Tool{
		Name:        "browser_navigate",
		Description: "Navigate to a URL in the persistent browser session and return the page text content. Start the browser first with browser_start if not already running.",
		Parameters:  mustJSON(`{"type":"object","properties":{"url":{"type":"string","description":"URL to navigate to"},"timeout":{"type":"integer","description":"Max seconds to wait (default 30)"}},"required":["url"]}`),
		Exec:        browserNavigateCDP,
	})
	r.Register(&Tool{
		Name:        "browser_click",
		Description: "Click an element in the current page by CSS selector. Returns the updated page text.",
		Parameters:  mustJSON(`{"type":"object","properties":{"selector":{"type":"string","description":"CSS selector of the element to click"}},"required":["selector"]}`),
		Exec:        browserClickCDP,
	})
	r.Register(&Tool{
		Name:        "browser_type",
		Description: "Type text into an input element identified by CSS selector.",
		Parameters:  mustJSON(`{"type":"object","properties":{"selector":{"type":"string","description":"CSS selector of the input element"},"text":{"type":"string","description":"Text to type"}},"required":["selector","text"]}`),
		Exec:        browserTypeCDP,
	})
	r.Register(&Tool{
		Name:        "browser_exec",
		Description: "Execute a JavaScript expression in the current page and return the result. Use expressions like document.title or document.querySelector('h1').innerText (no return keyword needed).",
		Parameters:  mustJSON(`{"type":"object","properties":{"code":{"type":"string","description":"JavaScript code to execute. Use return to get a value back."}},"required":["code"]}`),
		Exec:        browserExecCDP,
	})
	r.Register(&Tool{
		Name:        "browser_start",
		Description: "Start a persistent headless Chrome browser session (required before other browser_* tools).",
		Parameters:  mustJSON(`{"type":"object","properties":{},"required":[]}`),
		Exec:        browserStartCDP,
	})
	r.Register(&Tool{
		Name:        "browser_stop",
		Description: "Stop the persistent browser session.",
		Parameters:  mustJSON(`{"type":"object","properties":{},"required":[]}`),
		Exec:        browserStopCDP,
	})
	r.Register(&Tool{
		Name:        "browser_screenshot_cdp",
		Description: "Take a screenshot of the current page in the persistent browser session.",
		Parameters:  mustJSON(`{"type":"object","properties":{"path":{"type":"string","description":"Output file path (.png)"}},"required":["path"]}`),
		Exec:        browserScreenshotCDP,
	})
}

func ensureBrowser() error {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserCtx != nil {
		return nil
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Headless,
	)
	allocCtx, _ := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx)
	if err := chromedp.Run(ctx); err != nil {
		cancel()
		return fmt.Errorf("browser start failed: %w", err)
	}
	browserCtx, browserCancel = ctx, cancel
	return nil
}

func browserStartCDP(ctx context.Context, args map[string]any) Result {
	if err := ensureBrowser(); err != nil {
		return Result{Output: err.Error(), IsError: true}
	}
	return Result{Output: "browser started (Chrome headless, CDP session)"}
}

func browserStopCDP(ctx context.Context, args map[string]any) Result {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserCancel != nil {
		browserCancel()
		browserCtx, browserCancel = nil, nil
	}
	return Result{Output: "browser stopped"}
}

func browserNavigateCDP(ctx context.Context, args map[string]any) Result {
	url, _ := args["url"].(string)
	if url == "" {
		return Result{Output: "missing url", IsError: true}
	}
	if err := ensureBrowser(); err != nil {
		return Result{Output: err.Error(), IsError: true}
	}
	timeout := 30
	if t, ok := toInt(args["timeout"]); ok && t > 0 {
		timeout = t
	}
	tctx, cancel := context.WithTimeout(browserCtx, time.Duration(timeout)*time.Second)
	defer cancel()

	var title, body string
	err := chromedp.Run(tctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Title(&title),
		chromedp.Evaluate("document.body.innerText", &body),
	)
	if err != nil {
		return Result{Output: fmt.Sprintf("browser_navigate error: %v", err), IsError: true}
	}
	if len(body) > 15000 {
		body = body[:15000] + "\n... (truncated)"
	}
	return Result{Output: fmt.Sprintf("# %s\n%s", title, body)}
}

func browserClickCDP(ctx context.Context, args map[string]any) Result {
	selector, _ := args["selector"].(string)
	if selector == "" {
		return Result{Output: "missing selector", IsError: true}
	}
	if err := ensureBrowser(); err != nil {
		return Result{Output: err.Error(), IsError: true}
	}
	tctx, cancel := context.WithTimeout(browserCtx, 15*time.Second)
	defer cancel()

	var body string
	err := chromedp.Run(tctx,
		chromedp.WaitVisible(selector),
		chromedp.Click(selector),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate("document.body.innerText", &body),
	)
	if err != nil {
		return Result{Output: fmt.Sprintf("browser_click error: %v", err), IsError: true}
	}
	if len(body) > 15000 {
		body = body[:15000] + "\n... (truncated)"
	}
	return Result{Output: body}
}

func browserTypeCDP(ctx context.Context, args map[string]any) Result {
	selector, _ := args["selector"].(string)
	text, _ := args["text"].(string)
	if selector == "" || text == "" {
		return Result{Output: "missing selector or text", IsError: true}
	}
	if err := ensureBrowser(); err != nil {
		return Result{Output: err.Error(), IsError: true}
	}
	tctx, cancel := context.WithTimeout(browserCtx, 15*time.Second)
	defer cancel()

	err := chromedp.Run(tctx,
		chromedp.WaitVisible(selector),
		chromedp.SendKeys(selector, text),
	)
	if err != nil {
		return Result{Output: fmt.Sprintf("browser_type error: %v", err), IsError: true}
	}
	return Result{Output: fmt.Sprintf("typed into %s", selector)}
}

func browserExecCDP(ctx context.Context, args map[string]any) Result {
	code, _ := args["code"].(string)
	if code == "" {
		return Result{Output: "missing code", IsError: true}
	}
	if err := ensureBrowser(); err != nil {
		return Result{Output: err.Error(), IsError: true}
	}
	tctx, cancel := context.WithTimeout(browserCtx, 15*time.Second)
	defer cancel()

	var result string
	err := chromedp.Run(tctx, chromedp.Evaluate(code, &result))
	if err != nil {
		return Result{Output: fmt.Sprintf("browser_exec error: %v", err), IsError: true}
	}
	return Result{Output: result}
}

func browserScreenshotCDP(ctx context.Context, args map[string]any) Result {
	path, _ := args["path"].(string)
	if path == "" {
		return Result{Output: "missing path", IsError: true}
	}
	if err := ensureBrowser(); err != nil {
		return Result{Output: err.Error(), IsError: true}
	}
	tctx, cancel := context.WithTimeout(browserCtx, 15*time.Second)
	defer cancel()

	var buf []byte
	err := chromedp.Run(tctx, chromedp.FullScreenshot(&buf, 90))
	if err != nil {
		return Result{Output: fmt.Sprintf("screenshot error: %v", err), IsError: true}
	}
	os.MkdirAll(strings.TrimSuffix(path, "/"+string(filepath.Base(path))), 0755)
	if err := os.WriteFile(path, buf, 0644); err != nil {
		return Result{Output: fmt.Sprintf("write error: %v", err), IsError: true}
	}
	return Result{Output: fmt.Sprintf("screenshot saved to %s (%d bytes)", path, len(buf))}
}
