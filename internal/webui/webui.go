// Package webui embeds the gateway's single-page live report. Plain HTML,
// CSS, and vanilla JS with no build step: the page fetches /api/report and
// renders it, so everything a user needs ships inside the binary.
package webui

import (
	"embed"
	"net/http"
)

//go:embed index.html
var content embed.FS

// Handler serves the report page at "/" only; unknown paths 404 so typos
// don't silently render the dashboard.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		raw, err := content.ReadFile("index.html")
		if err != nil {
			http.Error(w, "report page unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(raw)
	})
}
