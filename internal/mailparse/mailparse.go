// Package mailparse turns a raw RFC 5322 byte stream into a model.Message.
//
// It is deliberately tolerant: malformed or non-MIME messages still produce a
// usable Message (raw is always preserved) so that ingest never drops mail just
// because a sender was sloppy. That robustness matters for a temp-mail sink,
// which sees every kind of garbage on the public internet.
package mailparse

import (
	"bytes"
	"io"
	"mime"
	"net/mail"
	"strings"

	// charset registers decoders for non-UTF-8 charsets (so go-message body
	// decoding does not fail on e.g. ISO-8859-1 or Shift_JIS) and provides a
	// Reader we reuse for RFC 2047 header word decoding.
	"github.com/emersion/go-message/charset"
	gomail "github.com/emersion/go-message/mail"

	"github.com/nees/zorail/internal/id"
	"github.com/nees/zorail/internal/model"
)

// headersOfInterest are captured into Message.Headers. Everything is still kept
// verbatim in Raw; this set is just the convenient subset for API/AI use.
var headersOfInterest = []string{
	"From", "To", "Cc", "Reply-To", "Subject", "Date", "Message-Id",
	"Return-Path", "Content-Type", "List-Unsubscribe", "Auto-Submitted",
	"X-Mailer", "Received", "DKIM-Signature", "Authentication-Results",
}

// Parse decodes raw message bytes. It always returns a non-nil Message with Raw
// and Size populated; err is non-nil only on a hard read failure of `raw`
// itself (which, given a byte slice, effectively never happens).
func Parse(raw []byte) (*model.Message, error) {
	m := &model.Message{
		Raw:     raw,
		Size:    int64(len(raw)),
		Headers: map[string]string{},
	}

	// First pass with net/mail: cheap, always-available header extraction that
	// works even when MIME structure is broken.
	if msg, err := mail.ReadMessage(bytes.NewReader(raw)); err == nil {
		captureHeaders(m, msg.Header)
		m.Subject = decodeWord(msg.Header.Get("Subject"))
		m.From = decodeWord(msg.Header.Get("From"))
		m.To = splitAddressList(msg.Header.Get("To"))
		m.MessageID = strings.Trim(msg.Header.Get("Message-Id"), "<> ")
		if t, err := mail.ParseDate(msg.Header.Get("Date")); err == nil {
			m.Date = t.UTC()
		}
	}

	// Second pass with go-message for proper MIME walking and transfer/charset
	// decoding of the bodies and attachments.
	if mr, err := gomail.CreateReader(bytes.NewReader(raw)); err == nil {
		walkParts(m, mr)
	} else {
		// Not a MIME multipart we can walk — treat the whole body as text.
		if body := bodyAfterHeaders(raw); body != "" && m.Text == "" {
			m.Text = body
		}
	}

	return m, nil
}

func walkParts(m *model.Message, mr *gomail.Reader) {
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break // give up on the rest, keep what we have
		}
		switch h := p.Header.(type) {
		case *gomail.InlineHeader:
			ct, _, _ := h.ContentType()
			b, _ := io.ReadAll(p.Body)
			switch {
			case strings.HasPrefix(ct, "text/html"):
				if m.HTML == "" {
					m.HTML = string(b)
				}
			case strings.HasPrefix(ct, "text/plain"):
				if m.Text == "" {
					m.Text = string(b)
				}
			}
		case *gomail.AttachmentHeader:
			filename, _ := h.Filename()
			ct, _, _ := h.ContentType()
			b, _ := io.ReadAll(p.Body)
			m.Attachments = append(m.Attachments, model.Attachment{
				ID:          id.New(),
				Filename:    filename,
				ContentType: ct,
				Size:        int64(len(b)),
				Content:     b,
			})
		}
	}
}

func captureHeaders(m *model.Message, h mail.Header) {
	for _, key := range headersOfInterest {
		if v := h.Get(key); v != "" {
			m.Headers[key] = decodeWord(v)
		}
	}
}

// splitAddressList parses a header address list, falling back to the raw string
// when parsing fails so a malformed To: is not silently dropped.
func splitAddressList(s string) []string {
	if s == "" {
		return nil
	}
	if addrs, err := mail.ParseAddressList(s); err == nil {
		out := make([]string, 0, len(addrs))
		for _, a := range addrs {
			out = append(out, a.Address)
		}
		return out
	}
	return []string{strings.TrimSpace(s)}
}

// decodeWord decodes RFC 2047 encoded-words (e.g. =?UTF-8?B?...?=) in headers.
func decodeWord(s string) string {
	dec := new(mime.WordDecoder)
	dec.CharsetReader = charset.Reader
	if out, err := dec.DecodeHeader(s); err == nil {
		return out
	}
	return s
}

// bodyAfterHeaders returns the text following the first blank line, for the
// non-MIME fallback path.
func bodyAfterHeaders(raw []byte) string {
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return strings.TrimSpace(string(raw[i+4:]))
	}
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		return strings.TrimSpace(string(raw[i+2:]))
	}
	return ""
}
