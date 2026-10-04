package report

import (
	"fmt"
	"html"
	"io"
)

// WriteBadgeSVG renders a shields-style score badge, served at
// /api/badge.svg. Green means the telemetry can be trusted; red means
// dashboards built on it are lying to you.
//
// The label reaches this function from telemetry (service.name via the
// ?service= query param) — anyone who can send OTLP can choose it, and SVG
// served as image/svg+xml executes scripts. Escape or be owned.
func WriteBadgeSVG(w io.Writer, label string, score float64) error {
	color := "#e05d44" // red
	switch {
	case score >= 0.95:
		color = "#4c1" // green
	case score >= 0.80:
		color = "#dfb317" // yellow
	}
	value := fmt.Sprintf("%.1f%%", score*100)

	// Widths approximated at 6.5px per character plus padding — fine for a
	// generated badge without font metrics. Measured on the raw string,
	// rendered with the escaped one.
	labelW := 6*len(label) + 14
	valueW := 6*len(value) + 14
	total := labelW + valueW
	label = html.EscapeString(label)
	value = html.EscapeString(value)

	_, err := fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">
  <linearGradient id="s" x2="0" y2="100%%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>
  <clipPath id="r"><rect width="%d" height="20" rx="3" fill="#fff"/></clipPath>
  <g clip-path="url(#r)">
    <rect width="%d" height="20" fill="#555"/>
    <rect x="%d" width="%d" height="20" fill="%s"/>
    <rect width="%d" height="20" fill="url(#s)"/>
  </g>
  <g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">
    <text x="%d" y="14">%s</text>
    <text x="%d" y="14">%s</text>
  </g>
</svg>
`, total, label, value, total, labelW, labelW, valueW, color, total, labelW/2, label, labelW+valueW/2, value)
	return err
}
