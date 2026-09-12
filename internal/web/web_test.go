package web

import (
	"testing"
)

func TestIsUnsafeURLBlocksLoopback(t *testing.T) {
	if !isUnsafeURL("http://127.0.0.1/secret") {
		t.Error("127.0.0.1 should be blocked")
	}
	if !isUnsafeURL("http://localhost/") {
		t.Error("localhost should be blocked")
	}
	if !isUnsafeURL("http://[::1]/") {
		t.Error("::1 should be blocked")
	}
}

func TestIsUnsafeURLBlocksPrivate(t *testing.T) {
	if !isUnsafeURL("http://192.168.1.1/admin") {
		t.Error("192.168.1.1 should be blocked")
	}
	if !isUnsafeURL("http://10.0.0.1/") {
		t.Error("10.0.0.1 should be blocked")
	}
	if !isUnsafeURL("http://172.16.0.1/") {
		t.Error("172.16.0.1 should be blocked")
	}
}

func TestIsUnsafeURLBlocksNonHTTP(t *testing.T) {
	if !isUnsafeURL("file:///etc/passwd") {
		t.Error("file:// should be blocked")
	}
	if !isUnsafeURL("gopher://internal/") {
		t.Error("gopher:// should be blocked")
	}
}

func TestIsUnsafeURLBlocksEmpty(t *testing.T) {
	if !isUnsafeURL("") {
		t.Error("empty URL should be blocked")
	}
	if !isUnsafeURL("not-a-url") {
		t.Error("invalid URL should be blocked")
	}
}

func TestIsUnsafeURLAllowsPublic(t *testing.T) {
	// Public IPs should not be blocked
	if isUnsafeURL("http://93.184.216.34/") {
		t.Error("public IP 93.184.216.34 should not be blocked")
	}
	if isUnsafeURL("https://example.com/") {
		t.Error("example.com should not be blocked")
	}
}

func TestParseDDG(t *testing.T) {
	html := `<a rel="nofollow" class="result__a" href="https://example.com/page">Title</a>`
	hits := parseDDG(html, 5)
	if len(hits) == 0 {
		t.Error("parseDDG found no results")
	}
	if hits[0].Title != "Title" {
		t.Errorf("Title = %q, want %q", hits[0].Title, "Title")
	}
}

func TestStripTags(t *testing.T) {
	got := stripTags("<p>hello <b>world</b></p>")
	if got != "hello world" {
		t.Errorf("stripTags = %q, want %q", got, "hello world")
	}
}
