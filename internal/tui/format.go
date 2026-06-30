package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// relTime renders a compact "time ago" like the web UI (now, 5s, 3m, 2h, 4d).
func relTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	s := int(time.Since(t).Seconds())
	switch {
	case s < 5:
		return "now"
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh", s/3600)
	case s < 604800:
		return fmt.Sprintf("%dd", s/86400)
	default:
		return t.Format("Jan 2")
	}
}

func fmtSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

var fromRe = regexp.MustCompile(`^\s*"?([^"<]*?)"?\s*<([^>]+)>\s*$`)

// parseFrom splits a From header into display name + address.
func parseFrom(from string) (name, email string) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", ""
	}
	if m := fromRe.FindStringSubmatch(from); m != nil {
		return strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
	}
	return "", from
}

// hostOf returns the host portion of a URL for compact link display.
func hostOf(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return raw
	}
	return s
}

// htmlToText strips tags so an HTML-only mail still shows readable body text.
var (
	tagRe   = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>`)
	anyTag  = regexp.MustCompile(`(?s)<[^>]+>`)
	wsLines = regexp.MustCompile(`\n[ \t]*\n[ \t]*\n+`)
)

func htmlToText(h string) string {
	h = tagRe.ReplaceAllString(h, "")
	h = strings.ReplaceAll(h, "<br>", "\n")
	h = strings.ReplaceAll(h, "<br/>", "\n")
	h = strings.ReplaceAll(h, "<br />", "\n")
	h = regexp.MustCompile(`(?i)</(p|div|tr|h[1-6]|li)>`).ReplaceAllString(h, "\n")
	h = anyTag.ReplaceAllString(h, "")
	h = strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", "\"", "&#39;", "'", "&mdash;", "—", "&rsquo;", "'",
	).Replace(h)
	h = wsLines.ReplaceAllString(h, "\n\n")
	return strings.TrimSpace(h)
}

// truncate shortens s to max display columns, adding an ellipsis.
func truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
