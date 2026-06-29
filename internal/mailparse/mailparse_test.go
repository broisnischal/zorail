package mailparse

import (
	"strings"
	"testing"
)

func TestParseMultipartWithAttachment(t *testing.T) {
	raw := strings.ReplaceAll(`From: "Alice Example" <alice@example.com>
To: test-inbox@zorail.test
Subject: =?UTF-8?B?SGVsbG8g8J+Riw==?=
Message-Id: <abc123@example.com>
Date: Mon, 02 Jan 2006 15:04:05 -0700
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="BOUND"

--BOUND
Content-Type: text/plain; charset=utf-8

Your code is 489213.
--BOUND
Content-Type: text/html; charset=utf-8

<p>Your code is <b>489213</b>.</p>
--BOUND
Content-Type: application/octet-stream
Content-Disposition: attachment; filename="note.txt"
Content-Transfer-Encoding: base64

aGVsbG8=
--BOUND--
`, "\n", "\r\n")

	m, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	if got, want := m.Subject, "Hello 👋"; got != want {
		t.Errorf("Subject = %q, want %q (RFC 2047 decode failed)", got, want)
	}
	if !strings.Contains(m.Text, "489213") {
		t.Errorf("Text body missing OTP content: %q", m.Text)
	}
	if !strings.Contains(m.HTML, "<b>489213</b>") {
		t.Errorf("HTML body missing content: %q", m.HTML)
	}
	if m.MessageID != "abc123@example.com" {
		t.Errorf("MessageID = %q", m.MessageID)
	}
	if len(m.To) != 1 || m.To[0] != "test-inbox@zorail.test" {
		t.Errorf("To = %v", m.To)
	}
	if len(m.Attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(m.Attachments))
	}
	a := m.Attachments[0]
	if a.Filename != "note.txt" {
		t.Errorf("attachment filename = %q", a.Filename)
	}
	if string(a.Content) != "hello" {
		t.Errorf("attachment content = %q (base64 decode failed)", string(a.Content))
	}
	if a.ID == "" {
		t.Error("attachment ID not assigned")
	}
}

func TestParsePlainNonMime(t *testing.T) {
	raw := strings.ReplaceAll(`From: bob@example.com
To: catch@zorail.test
Subject: plain

just a plain body line
`, "\n", "\r\n")

	m, err := Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if m.Subject != "plain" {
		t.Errorf("Subject = %q", m.Subject)
	}
	if !strings.Contains(m.Text, "just a plain body line") {
		t.Errorf("Text = %q", m.Text)
	}
	if m.Size != int64(len(raw)) {
		t.Errorf("Size = %d, want %d", m.Size, len(raw))
	}
}

func TestParseGarbageStillPreservesRaw(t *testing.T) {
	raw := []byte("this is not a valid email at all \x00\x01")
	m, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse should not error on garbage: %v", err)
	}
	if m.Size != int64(len(raw)) {
		t.Errorf("Size = %d, want %d", m.Size, len(raw))
	}
	if string(m.Raw) != string(raw) {
		t.Error("Raw not preserved for garbage input")
	}
}
