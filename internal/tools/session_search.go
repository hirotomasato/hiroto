package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/hirotomasato/hiroto/internal/session"
)

func registerSessionSearch(r *Registry, opts Options) {
	r.Register(&Tool{
		Name:        "session_search",
		Description: "Search past conversation sessions by keyword. Returns matching sessions with ID, title, date, and matching message snippets.",
		Parameters: mustJSON(`{"type":"object","properties":{"query":{"type":"string","description":"Search query (keyword or phrase)"}},"required":["query"]}`),
		Exec: func(ctx context.Context, args map[string]any) Result {
			query, _ := args["query"].(string)
			if query == "" {
				return Result{Output: "missing query", IsError: true}
			}
			if opts.SessionSearch == nil {
				return Result{Output: "session_search not available (no session store)", IsError: true}
			}
			results := opts.SessionSearch(query)
			if len(results) == 0 {
				return Result{Output: fmt.Sprintf("no sessions found for: %s", query)}
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d sesi ditemukan:\n\n", len(results))
			for i, s := range results {
				if i >= 10 {
					fmt.Fprintf(&b, "... dan %d lainnya\n", len(results)-10)
					break
				}
				fmt.Fprintf(&b, "## %s\n", s.Title)
				fmt.Fprintf(&b, "  id: %s\n", s.ID)
				fmt.Fprintf(&b, "  model: %s\n", s.Model)
				fmt.Fprintf(&b, "  updated: %s\n", s.Updated.Format("02 Jan 15:04"))
				fmt.Fprintf(&b, "  messages: %d\n\n", len(s.Messages))
			}
			return Result{Output: strings.TrimRight(b.String(), "\n")}
		},
	})
}

// SessionSearchFunc is the callback for searching sessions.
type SessionSearchFunc func(query string) []session.Session