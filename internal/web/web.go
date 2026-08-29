// Package web provides the web_search / web_extract backends.
// web_search uses DuckDuckGo's HTML endpoint (no API key needed);
// web_extract fetches a URL and strips HTML to readable text.
package web

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var uaClient = &http.Client{Timeout: 20 * time.Second}

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36"

// SearchHit is one organic result.
type SearchHit struct {
	Title, URL, Description string
}

// Search queries Bing's RSS endpoint (works from this machine; personal
// low-volume use per Bing RSS terms), falling back to DuckDuckGo HTML.
func Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 5
	}
	hits, err := searchBing(ctx, query, limit)
	if err == nil && len(hits) > 0 {
		return hits, nil
	}
	hits2, err2 := searchDDG(ctx, query, limit)
	if err2 == nil && len(hits2) > 0 {
		return hits2, nil
	}
	if err != nil && err2 != nil {
		return nil, fmt.Errorf("bing: %v; ddg: %v", err, err2)
	}
	return hits, nil
}

// searchBing fetches Bing search results as RSS and parses the items.
func searchBing(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	u := "https://www.bing.com/search?q=" + url.QueryEscape(query) + "&format=rss"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := uaClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bing request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bing HTTP %d", resp.StatusCode)
	}
	var feed struct {
		Channel struct {
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Description string `xml:"description"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&feed); err != nil {
		return nil, fmt.Errorf("bing rss parse: %w", err)
	}
	var out []SearchHit
	for _, it := range feed.Channel.Items {
		if len(out) >= limit {
			break
		}
		if strings.TrimSpace(it.Title) == "" || strings.TrimSpace(it.Link) == "" {
			continue
		}
		out = append(out, SearchHit{
			Title:       stripTags(it.Title),
			URL:         strings.TrimSpace(it.Link),
			Description: stripTags(it.Description),
		})
	}
	return out, nil
}

// searchDDG queries DuckDuckGo's HTML endpoint (no key required).
func searchDDG(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	form := url.Values{"q": {query}, "kl": {"wt-wt"}}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://html.duckduckgo.com/html/", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)
	resp, err := uaClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseDDG(string(body), limit), nil
}

var (
	resultRe  = regexp.MustCompile(`(?s)<a rel="nofollow" class="result__a" href="([^"]+)"[^>]*>(.*?)</a>`)
	snippetRe = regexp.MustCompile(`(?s)<a class="result__snippet"[^>]*>(.*?)</a>`)
	tagRe     = regexp.MustCompile(`<[^>]+>`)
)

func parseDDG(page string, limit int) []SearchHit {
	hrefs := resultRe.FindAllStringSubmatch(page, -1)
	snips := snippetRe.FindAllStringSubmatch(page, -1)
	var out []SearchHit
	for i, m := range hrefs {
		if len(out) >= limit {
			break
		}
		u := unescapeDDGURL(m[1])
		if u == "" {
			continue
		}
		title := stripTags(m[2])
		desc := ""
		if i < len(snips) {
			desc = stripTags(snips[i][1])
		}
		out = append(out, SearchHit{Title: title, URL: u, Description: desc})
	}
	return out
}

// unescapeDDGURL resolves DDG's //duckduckgo.com/l/?uddg=... redirect links.
func unescapeDDGURL(raw string) string {
	raw = html.UnescapeString(raw)
	if strings.HasPrefix(raw, "//duckduckgo.com/l/") || strings.HasPrefix(raw, "https://duckduckgo.com/l/") {
		if u, err := url.Parse(raw); err == nil {
			if got := u.Query().Get("uddg"); got != "" {
				if dec, err := url.QueryUnescape(got); err == nil {
					return dec
				}
			}
		}
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return ""
}

func stripTags(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// Extract fetches one or more URLs and returns readable text.
func Extract(ctx context.Context, urls []string) ([]PageResult, error) {
	var out []PageResult
	for _, u := range urls {
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			u = "https://" + u
		}
		r, err := extractOne(ctx, u)
		if err != nil {
			r = PageResult{URL: u, Title: "ERROR", Content: err.Error()}
		}
		out = append(out, r)
	}
	return out, nil
}

type PageResult struct {
	URL, Title, Content string
}

// RE2 has no backreferences, so one regex per tag name.
var scriptStyleRes = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`),
	regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`),
	regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`),
	regexp.MustCompile(`(?is)<svg[^>]*>.*?</svg>`),
}

func stripBlocks(doc string) string {
	for _, re := range scriptStyleRes {
		doc = re.ReplaceAllString(doc, " ")
	}
	return doc
}

func extractOne(ctx context.Context, u string) (PageResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return PageResult{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := uaClient.Do(req)
	if err != nil {
		return PageResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return PageResult{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 3<<20))
	if err != nil {
		return PageResult{}, err
	}
	text, title := htmlToText(string(body))
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "")
	}
	if len(text) > 15000 {
		text = text[:15000] + "\n[content truncated]"
	}
	return PageResult{URL: u, Title: title, Content: text}, nil
}

// htmlToText converts HTML to readable text: drops script/style, blocks to
// newlines, collapses whitespace. Good enough for research extraction.
func htmlToText(doc string) (text, title string) {
	if m := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindStringSubmatch(doc); m != nil {
		title = stripTags(m[1])
	}
	doc = stripBlocks(doc)
	// Block-level boundaries become newlines.
	doc = regexp.MustCompile(`(?i)</(p|div|section|article|h[1-6]|li|tr|br|pre|blockquote|table|ul|ol)>`).ReplaceAllString(doc, "\n")
	doc = regexp.MustCompile(`(?i)<(br|hr)[^>]*>`).ReplaceAllString(doc, "\n")
	doc = tagRe.ReplaceAllString(doc, " ")
	doc = html.UnescapeString(doc)
	lines := strings.Split(doc, "\n")
	var kept []string
	for _, ln := range lines {
		ln = strings.Join(strings.Fields(ln), " ")
		if strings.TrimSpace(ln) != "" {
			kept = append(kept, ln)
		}
	}
	return strings.Join(kept, "\n"), title
}
