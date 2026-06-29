package extract

import (
	"strings"
	"testing"
)

func TestCodesAndLinks(t *testing.T) {
	text := "Your verification code is 884217. Confirm: https://app.test/verify?token=abc123 or visit https://app.test/help."
	html := `<p>Code <b>884217</b></p><a href="https://app.test/verify?token=abc123">Confirm</a>`
	r := From(nil, text, html)

	if len(r.Codes) == 0 || r.Codes[0] != "884217" {
		t.Errorf("codes = %v, want first 884217", r.Codes)
	}
	if !contains(r.Links, "https://app.test/verify?token=abc123") {
		t.Errorf("links missing verify url: %v", r.Links)
	}
	// trailing period must be trimmed
	if !contains(r.Links, "https://app.test/help") {
		t.Errorf("links should include trimmed help url: %v", r.Links)
	}
}

func TestUnsubscribeFromHeader(t *testing.T) {
	headers := map[string]string{
		"List-Unsubscribe": "<https://lists.test/u/abc>, <mailto:u@lists.test>",
	}
	r := From(headers, "newsletter body", "")
	if !contains(r.Unsubscribe, "https://lists.test/u/abc") {
		t.Errorf("unsubscribe = %v, want header http url", r.Unsubscribe)
	}
	// mailto: should be excluded from the http-only list
	for _, u := range r.Unsubscribe {
		if strings.HasPrefix(u, "mailto:") {
			t.Errorf("mailto should not appear: %v", r.Unsubscribe)
		}
	}
}

func TestSpamScoring(t *testing.T) {
	clean := Score(nil, "Your receipt", "Thanks for your purchase.", "", 1)
	if clean.Label != "clean" {
		t.Errorf("clean label = %q (score %d)", clean.Label, clean.Score)
	}

	spammy := Score(
		map[string]string{"Authentication-Results": "mx.test; spf=fail; dkim=fail"},
		"CONGRATULATIONS YOU WON!!!",
		"You have won the lottery! Click here now to claim your prize, 100% free, act now!",
		"",
		20,
	)
	if spammy.Label != "high" {
		t.Errorf("spammy label = %q, want high (score %d, reasons %v)", spammy.Label, spammy.Score, spammy.Reasons)
	}
	if len(spammy.Reasons) == 0 {
		t.Error("expected spam reasons")
	}
}

func TestEmptyResultsAreNonNilSlices(t *testing.T) {
	r := From(nil, "nothing here", "")
	if r.Codes == nil || r.Links == nil || r.Unsubscribe == nil {
		t.Error("extraction slices must be non-nil for clean JSON arrays")
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
