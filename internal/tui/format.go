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

// osc8 wraps label in an OSC 8 terminal hyperlink pointing at url, so a click in
// a supporting terminal (iTerm2, kitty, WezTerm, Warp, …) opens the *full* URL
// rather than guessing from the visible text. Terminals without OSC 8 ignore the
// escapes and just show label. BEL terminates the sequence (widest support).
func osc8(url, label string) string {
	return "\x1b]8;;" + url + "\x07" + label + "\x1b]8;;\x07"
}

// shortLink formats a URL for display: scheme dropped and truncated to max with
// an ellipsis. The real (full) URL is preserved as the hyperlink target.
func shortLink(raw string, max int) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s = strings.TrimSuffix(s, "/")
	if max > 1 {
		if r := []rune(s); len(r) > max {
			s = string(r[:max-1]) + "…"
		}
	}
	return s
}

// htmlToText renders an HTML body as readable text while keeping links live:
// each <a href> becomes a clickable OSC 8 hyperlink (so a "Sign in" button is
// still clickable, not flattened to dead text), and the URL's &amp; separators
// are decoded so provider links resolve correctly.
var (
	tagRe        = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>`)
	anchorRe     = regexp.MustCompile(`(?is)<a\b[^>]*?href\s*=\s*["']?(https?://[^"'>\s]+)[^>]*>(.*?)</a>`)
	blockCloseRe = regexp.MustCompile(`(?i)</(p|div|tr|h[1-6]|li)>`)
	anyTag       = regexp.MustCompile(`(?s)<[^>]+>`)
	wsLines      = regexp.MustCompile(`\n[ \t]*\n[ \t]*\n+`)
	bareURLRe    = regexp.MustCompile(`https?://[^\s<>"')\]}]+`)
	htmlEntities = strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", "\"", "&#39;", "'", "&mdash;", "—", "&rsquo;", "'",
	)
	// urlAmp restores the ampersand a URL is HTML-encoded with (& → &amp;). It
	// also repairs links cached before the server-side extractor was fixed.
	urlAmp = strings.NewReplacer("&amp;", "&", "&#38;", "&", "&#x26;", "&", "&#X26;", "&")
)

func htmlToText(h string) string {
	h = tagRe.ReplaceAllString(h, "")
	h = anchorRe.ReplaceAllStringFunc(h, func(a string) string {
		m := anchorRe.FindStringSubmatch(a)
		if m == nil {
			return a
		}
		url := urlAmp.Replace(m[1])
		label := htmlEntities.Replace(strings.TrimSpace(anyTag.ReplaceAllString(m[2], "")))
		if label == "" {
			label = shortLink(url, 60)
		}
		return " " + osc8(url, label) + " "
	})
	h = strings.ReplaceAll(h, "<br>", "\n")
	h = strings.ReplaceAll(h, "<br/>", "\n")
	h = strings.ReplaceAll(h, "<br />", "\n")
	h = blockCloseRe.ReplaceAllString(h, "\n")
	h = anyTag.ReplaceAllString(h, "")
	h = htmlEntities.Replace(h)
	h = wsLines.ReplaceAllString(h, "\n\n")
	return strings.TrimSpace(h)
}

// linkifyText makes bare URLs in a plain-text body clickable (OSC 8), leaving
// the visible text unchanged and trailing punctuation outside the link.
func linkifyText(s string) string {
	return bareURLRe.ReplaceAllStringFunc(s, func(u string) string {
		clean := strings.TrimRight(u, ".,;:!?)]}\"'")
		trail := u[len(clean):]
		return osc8(urlAmp.Replace(clean), clean) + trail
	})
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
