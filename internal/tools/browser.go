package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func registerBrowserTools(r *Registry) {
	r.Register(&Tool{
		Name:        "browser_fetch",
		Description: "Fetch a web page using a real headless Chrome browser that renders JavaScript. Returns the page text content (DOM).",
		Parameters:  mustJSON(`{"type":"object","properties":{"url":{"type":"string","description":"URL to fetch"},"timeout":{"type":"integer","description":"Max seconds (default 30)"}},"required":["url"]}`),
		Exec:        browserFetch,
	})
	r.Register(&Tool{
		Name:        "browser_screenshot",
		Description: "Take a PNG screenshot of a web page using headless Chrome. Saves to the given path.",
		Parameters:  mustJSON(`{"type":"object","properties":{"url":{"type":"string"},"path":{"type":"string","description":"Output file path (.png)"},"timeout":{"type":"integer","description":"Max seconds (default 30)"}},"required":["url","path"]}`),
		Exec:        browserScreenshot,
	})
}

func browserFetch(ctx context.Context, args map[string]any) Result {
	url, _ := args["url"].(string)
	if url == "" {
		return Result{Output: "missing url", IsError: true}
	}
	timeout := 30
	if t, ok := toInt(args["timeout"]); ok && t > 0 {
		timeout = t
	}
	ctx2, cancel := context.WithTimeout(ctx, time.Duration(timeout+5)*time.Second)
	defer cancel()

	chrome := chromePath()
	cmd := exec.CommandContext(ctx2, chrome,
		"--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
		"--dump-dom", "--virtual-time-budget="+fmt.Sprintf("%d", timeout*1000), url,
	)
	cmd.Env = append(os.Environ(), "DISPLAY=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Output: fmt.Sprintf("browser_fetch error: %v\n%s", err, string(out)), IsError: true}
	}
	text := htmlToText(string(out))
	if len(text) > 15000 {
		text = text[:15000] + "\n... (truncated)"
	}
	return Result{Output: text}
}

func browserScreenshot(ctx context.Context, args map[string]any) Result {
	url, _ := args["url"].(string)
	path, _ := args["path"].(string)
	if url == "" || path == "" {
		return Result{Output: "missing url or path", IsError: true}
	}
	timeout := 30
	if t, ok := toInt(args["timeout"]); ok && t > 0 {
		timeout = t
	}
	ctx2, cancel := context.WithTimeout(ctx, time.Duration(timeout+5)*time.Second)
	defer cancel()

	os.MkdirAll(filepath.Dir(path), 0755)
	chrome := chromePath()
	cmd := exec.CommandContext(ctx2, chrome,
		"--headless=new", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage",
		"--screenshot="+path, "--window-size=1280,720",
		"--virtual-time-budget="+fmt.Sprintf("%d", timeout*1000), url,
	)
	cmd.Env = append(os.Environ(), "DISPLAY=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return Result{Output: fmt.Sprintf("browser_screenshot error: %v\n%s", err, string(out)), IsError: true}
	}
	return Result{Output: fmt.Sprintf("screenshot saved to %s (%d bytes)", path, fileSize(path))}
}

func chromePath() string {
	for _, p := range []string{"google-chrome", "chromium", "chromium-browser", "google-chrome-stable"} {
		if _, err := exec.LookPath(p); err == nil {
			return p
		}
	}
	return "google-chrome"
}

func fileSize(p string) int64 {
	fi, _ := os.Stat(p)
	if fi != nil {
		return fi.Size()
	}
	return 0
}

func htmlToText(s string) string {
	s = strings.NewReplacer("<br>", "\n", "<br/>", "\n", "<br />", "\n", "<p>", "\n", "</p>", "\n", "<li>", "\n- ").Replace(s)
	for {
		start := strings.Index(s, "<")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], ">")
		if end < 0 {
			break
		}
		s = s[:start] + s[start+end+1:]
	}
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}