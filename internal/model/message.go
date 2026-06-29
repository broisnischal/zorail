// Package model defines Zorail's core domain types. These are deliberately
// storage- and transport-agnostic so that the SMTP ingest layer, the storage
// layer, and (later) the API/AI layers all share one vocabulary.
package model

import "time"

// Message is a single email received by Zorail and bound to exactly one inbox.
//
// An inbound SMTP transaction may have several recipients; the ingest layer
// fans those out into one Message per recipient, each with its own ID, so that
// every inbox owns an independent copy it can read or delete in isolation.
type Message struct {
	ID         string            `json:"id"`
	Inbox      string            `json:"inbox"`       // normalized recipient address that owns this message
	EnvFrom    string            `json:"env_from"`    // envelope MAIL FROM (return-path)
	From       string            `json:"from"`        // From: header (display form)
	To         []string          `json:"to"`          // To: header addresses
	Subject    string            `json:"subject"`     // Subject: header
	MessageID  string            `json:"message_id"`  // Message-ID: header
	Date       time.Time         `json:"date"`        // Date: header (sender-asserted)
	ReceivedAt time.Time         `json:"received_at"` // when Zorail accepted the message
	Text       string            `json:"text"`        // decoded text/plain body
	HTML       string            `json:"html"`        // decoded text/html body
	Headers    map[string]string `json:"headers"`     // selected headers, last value wins
	Size       int64             `json:"size"`        // size of Raw in bytes
	Raw        []byte            `json:"-"`           // full RFC 5322 source; excluded from JSON by default

	Attachments []Attachment `json:"attachments,omitempty"`
}

// Attachment is a non-inline MIME part extracted from a Message.
type Attachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Content     []byte `json:"-"` // raw bytes; excluded from JSON by default
}

// InboxSummary is a lightweight roll-up of one inbox, used by listing endpoints.
type InboxSummary struct {
	Inbox        string    `json:"inbox"`
	MessageCount int       `json:"message_count"`
	LastReceived time.Time `json:"last_received"`
}

// Clone returns a deep-enough copy of the message for per-recipient fan-out.
// Raw and Attachment content are shared by reference because they are treated
// as immutable once parsed; everything the ingest layer mutates per recipient
// (ID, Inbox) is set on the copy by the caller.
func (m *Message) Clone() *Message {
	cp := *m
	if m.To != nil {
		cp.To = append([]string(nil), m.To...)
	}
	if m.Headers != nil {
		cp.Headers = make(map[string]string, len(m.Headers))
		for k, v := range m.Headers {
			cp.Headers[k] = v
		}
	}
	if m.Attachments != nil {
		cp.Attachments = append([]Attachment(nil), m.Attachments...)
	}
	return &cp
}
