// Package extract derives useful, first-class signals from a received message:
// one-time codes, links, unsubscribe targets, and a lightweight spam score.
// This is the server-side version of what the dashboard used to guess at
// client-side — computed once, in Go, and exposed through the API so any
// consumer (test suites, the UI, future automations) gets the same answer.
package extract

import (
	"regexp"
	"sort"
	"strings"
)

// Result is the bundle of everything we extract from one message.
type Result struct {
	Codes       []string `json:"codes"`
	Links       []string `json:"links"`
	Unsubscribe []string `json:"unsubscribe"`
}

var (
	tagRe       = regexp.MustCompile(`(?s)<[^>]+>`)
	wsRe        = regexp.MustCompile(`\s+`)
	linkRe      = regexp.MustCompile(`https?://[^\s"'<>)\]}]+`)
	hrefRe      = regexp.MustCompile(`(?i)href\s*=\s*["']?(https?://[^\s"'<>]+)`)
	unsubBodyRe = regexp.MustCompile(`(?i)https?://[^\s"'<>)\]}]*unsub[^\s"'<>)\]}]*`)
	angleRe     = regexp.MustCompile(`<([^>]+)>`)

	// A code candidate: a 4–8 char run of digits or UPPERCASE alphanumerics.
	// Case-sensitive on purpose — ordinary lowercase words must not match.
	codeTokenRe = regexp.MustCompile(`\b[0-9A-Z]{4,8}\b`)
	// Words that, when they appear just before a candidate, mark it as a real
	// one-time code. Matched against the preceding context window.
	codeWordRe = regexp.MustCompile(`(?i)(code|otp|one[\s-]?time|verification|verify|passcode|pin\b|token|2fa|auth|security|confirm)`)
)

// StripHTML reduces an HTML body to rough plain text for keyword scanning.
func StripHTML(html string) string {
	if html == "" {
		return ""
	}
	s := tagRe.ReplaceAllString(html, " ")
	s = strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&nbsp;", " ", "&#39;", "'", "&quot;", `"`).Replace(s)
	return wsRe.ReplaceAllString(s, " ")
}

// From computes all extraction signals from a message's parts.
func From(headers map[string]string, text, html string) Result {
	plain := text
	if plain == "" {
		plain = StripHTML(html)
	}
	return Result{
		Codes:       codes(plain),
		Links:       links(text, html),
		Unsubscribe: unsubscribe(headers, text, html),
	}
}

// codes finds one-time codes by scoring every candidate token: a candidate with
// a code keyword right before it wins decisively, years and digits inside URLs
// are excluded, and a bare 6-digit number is only used as a last resort. This
// fixes the old behavior of grabbing the first stray 6-digit number it saw.
func codes(plain string) []string {
	// Blank out URLs so digits inside links (?token=123456, /v/998877) never
	// register as codes.
	scan := linkRe.ReplaceAllStringFunc(plain, func(u string) string {
		return strings.Repeat(" ", len(u))
	})
	lower := strings.ToLower(scan)

	type cand struct {
		val   string
		score int
		pos   int
	}
	var cands []cand
	for _, loc := range codeTokenRe.FindAllStringIndex(scan, -1) {
		tok := scan[loc[0]:loc[1]]
		if !hasDigit(tok) {
			continue // all-letter uppercase runs (e.g. "HELLO") aren't codes
		}
		score := lengthScore(len(tok))
		// keyword in the ~40 chars before the token → strong signal.
		start := loc[0] - 40
		if start < 0 {
			start = 0
		}
		if codeWordRe.MatchString(lower[start:loc[0]]) {
			score += 100
		}
		if isYearLike(tok) {
			score -= 80 // 1998 / 2026 etc. are almost never codes
		}
		cands = append(cands, cand{tok, score, loc[0]})
	}

	// Highest score first; ties broken by earliest position (reading order).
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].pos < cands[j].pos
	})

	set := newOrderedSet()
	for _, c := range cands {
		if c.score > 0 && c.score >= 100 { // confident: keyword-associated
			set.add(c.val)
		}
	}
	// Fallback: no keyword context anywhere → the classic standalone 6-digit OTP.
	if len(set.items) == 0 {
		for _, c := range cands {
			if len(c.val) == 6 && allDigits(c.val) && !isYearLike(c.val) {
				set.add(c.val)
				break
			}
		}
	}
	return set.slice(6)
}

// lengthScore nudges toward the most common OTP lengths (6, then 4/5/7/8).
func lengthScore(n int) int {
	switch n {
	case 6:
		return 12
	case 4, 5:
		return 6
	default:
		return 5
	}
}

func isYearLike(s string) bool {
	if len(s) != 4 || !allDigits(s) {
		return false
	}
	return s >= "1900" && s <= "2099"
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func links(text, html string) []string {
	set := newOrderedSet()
	// Prefer href targets (they're the real destinations in HTML mail).
	for _, m := range hrefRe.FindAllStringSubmatch(html, -1) {
		set.add(trimURL(m[1]))
	}
	for _, m := range linkRe.FindAllString(text, -1) {
		set.add(trimURL(m))
	}
	for _, m := range linkRe.FindAllString(html, -1) {
		set.add(trimURL(m))
	}
	return set.slice(40)
}

func unsubscribe(headers map[string]string, text, html string) []string {
	set := newOrderedSet()
	// RFC 2369 List-Unsubscribe header: one or more <…> values.
	for _, key := range []string{"List-Unsubscribe", "List-unsubscribe", "list-unsubscribe"} {
		if v, ok := headers[key]; ok {
			for _, m := range angleRe.FindAllStringSubmatch(v, -1) {
				u := strings.TrimSpace(m[1])
				if strings.HasPrefix(u, "http") {
					set.add(u)
				}
			}
		}
	}
	// Body links that look like unsubscribe endpoints.
	for _, m := range unsubBodyRe.FindAllString(text+" "+html, -1) {
		set.add(trimURL(m))
	}
	return set.slice(10)
}

// ampEntities un-encodes the ampersand a URL is HTML-attribute-encoded with
// inside an href (`&` → `&amp;`). Without this, query separators stay encoded
// and a link like `?mode=signIn&amp;oobCode=…` opens with a param named
// `amp;oobCode` and no real `oobCode`/`mode` — breaking provider sign-in links.
// Only the ampersand forms are decoded, never a blanket HTML unescape, so a
// legitimate param such as `&copy=1` is left intact.
var ampEntities = strings.NewReplacer("&amp;", "&", "&#38;", "&", "&#x26;", "&", "&#X26;", "&")

func trimURL(u string) string {
	u = ampEntities.Replace(strings.TrimSpace(u))
	return strings.TrimRight(u, ".,;)]\"'")
}

// orderedSet preserves first-seen order while de-duplicating.
type orderedSet struct {
	seen  map[string]struct{}
	items []string
}

func newOrderedSet() *orderedSet { return &orderedSet{seen: map[string]struct{}{}} }

func (s *orderedSet) add(v string) {
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	if _, ok := s.seen[v]; ok {
		return
	}
	s.seen[v] = struct{}{}
	s.items = append(s.items, v)
}

func (s *orderedSet) slice(max int) []string {
	out := s.items
	if len(out) > max {
		out = out[:max]
	}
	if out == nil {
		return []string{}
	}
	return out
}

// --- spam scoring ---

// Spam is a lightweight, explainable spam assessment. It is intentionally
// heuristic (no external services) so it works fully offline; the score is a
// 0–100 hint, not a verdict.
type Spam struct {
	Score   int      `json:"score"`   // 0 (clean) – 100 (very spammy)
	Label   string   `json:"label"`   // "clean" | "low" | "medium" | "high"
	Reasons []string `json:"reasons"` // human-readable contributing factors
}

var spammyPhrases = []string{
	"viagra", "lottery", "winner", "click here now", "act now", "limited time",
	"risk-free", "100% free", "make money", "work from home", "weight loss",
	"crypto", "investment opportunity", "wire transfer", "nigerian prince",
	"you have won", "claim your prize", "congratulations you",
}

// Score evaluates a message. authResults is the Authentication-Results header
// (may be empty); linkCount is len(extracted links).
func Score(headers map[string]string, subject, text, html string, linkCount int) Spam {
	score := 0
	var reasons []string

	body := strings.ToLower(subject + " " + text + " " + StripHTML(html))

	hits := 0
	for _, p := range spammyPhrases {
		if strings.Contains(body, p) {
			hits++
		}
	}
	if hits > 0 {
		add := hits * 15
		if add > 45 {
			add = 45
		}
		score += add
		reasons = append(reasons, plural(hits, "spam-trigger phrase"))
	}

	if strings.Count(subject, "!") >= 2 || strings.Contains(subject, "!!!") {
		score += 10
		reasons = append(reasons, "excessive exclamation in subject")
	}
	if isMostlyUpper(subject) && len(subject) > 8 {
		score += 10
		reasons = append(reasons, "shouting subject (all caps)")
	}
	if linkCount > 12 {
		score += 15
		reasons = append(reasons, "very high link count")
	} else if linkCount > 6 {
		score += 8
		reasons = append(reasons, "high link count")
	}

	auth := strings.ToLower(headers["Authentication-Results"])
	if auth != "" {
		if strings.Contains(auth, "spf=fail") || strings.Contains(auth, "dkim=fail") || strings.Contains(auth, "dmarc=fail") {
			score += 25
			reasons = append(reasons, "failed SPF/DKIM/DMARC")
		}
	}
	// HTML with no plaintext alternative is a mild spam signal.
	if html != "" && strings.TrimSpace(text) == "" {
		score += 5
		reasons = append(reasons, "no plain-text part")
	}

	if score > 100 {
		score = 100
	}
	if reasons == nil {
		reasons = []string{}
	}
	return Spam{Score: score, Label: label(score), Reasons: reasons}
}

func label(score int) string {
	switch {
	case score >= 60:
		return "high"
	case score >= 30:
		return "medium"
	case score >= 12:
		return "low"
	default:
		return "clean"
	}
}

func isMostlyUpper(s string) bool {
	letters, upper := 0, 0
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			upper++
			letters++
		} else if r >= 'a' && r <= 'z' {
			letters++
		}
	}
	return letters > 0 && upper*100/letters >= 80
}

func plural(n int, word string) string {
	out := strings.TrimSpace(itoa(n) + " " + word)
	if n != 1 {
		out += "s"
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
