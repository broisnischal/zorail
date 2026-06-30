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

func TestLinkAmpEntitiesDecoded(t *testing.T) {
	// HTML hrefs encode query separators as &amp;; the extracted link must use
	// real & so provider sign-in links (mode=…&oobCode=…) resolve correctly.
	html := `<a href="https://proj.firebaseapp.com/__/auth/action?apiKey=K&amp;mode=signIn&amp;oobCode=XYZ&amp;lang=en">Sign in</a>`
	r := From(nil, "", html)
	want := "https://proj.firebaseapp.com/__/auth/action?apiKey=K&mode=signIn&oobCode=XYZ&lang=en"
	if !contains(r.Links, want) {
		t.Errorf("link not decoded.\n got: %v\nwant: %s", r.Links, want)
	}
	for _, l := range r.Links {
		if strings.Contains(l, "&amp;") {
			t.Errorf("link still contains &amp;: %s", l)
		}
	}
}

func TestCodeDetectionAccuracy(t *testing.T) {
	cases := []struct {
		name, text, want string
	}{
		{"keyword beats year", "© 2024 Acme. Your verification code is 553201. Expires in 10 minutes.", "553201"},
		{"keyword beats order number", "Order 100245 confirmed. Your OTP: 778899", "778899"},
		{"ignores digits in URLs", "Open https://app.test/v/123456 — your code 998877", "998877"},
		{"colon separator", "Your login code: 401923", "401923"},
		{"bare six-digit fallback", "Here it is: 246810 — enter it on the site", "246810"},
		{"alphanumeric code", "Use code 7H2K9Q to continue", "7H2K9Q"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := From(nil, c.text, "")
			if len(r.Codes) == 0 || r.Codes[0] != c.want {
				t.Errorf("codes=%v, want first %q", r.Codes, c.want)
			}
		})
	}

	// A plain year with no code keyword anywhere must NOT be reported as a code.
	if r := From(nil, "Copyright 2026 Acme Inc. All rights reserved.", ""); len(r.Codes) != 0 {
		t.Errorf("expected no codes for a bare year, got %v", r.Codes)
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
