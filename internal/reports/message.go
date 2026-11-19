package reports

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// Message is one email to one recipient.
type Message struct {
	From    mail.Address
	To      mail.Address
	Subject string
	Date    time.Time
	Text    string
	HTML    string
	// Attachments make the message multipart/mixed around the text and
	// HTML parts.
	Attachments []Attachment
}

// Attachment is one file of a message.
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// Subject is "[Logchef] <report name>, <date of at in loc>".
func Subject(reportName string, at time.Time, loc *time.Location) string {
	return fmt.Sprintf("[Logchef] %s, %s", reportName, at.In(loc).Format(time.DateOnly))
}

// BuildMessage writes m as a multipart/alternative MIME message with a text
// part and an HTML part, inside a multipart/mixed one with the attachments
// when m has any. The subject is an RFC 2047 encoded word whenever it holds
// anything but printable ASCII, so a name cannot add a header.
func BuildMessage(m Message) ([]byte, error) {
	if m.From.Address == "" || m.To.Address == "" {
		return nil, fmt.Errorf("a report message needs a from and a to address")
	}
	contentType, body, err := alternativeBody(m)
	if err != nil {
		return nil, err
	}
	if len(m.Attachments) > 0 {
		if contentType, body, err = mixedBody(contentType, body, m.Attachments); err != nil {
			return nil, err
		}
	}

	id, err := messageID(m.From.Address)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	for _, h := range [][2]string{
		{"From", foldWords(m.From.String())},
		{"To", foldWords(m.To.String())},
		{"Subject", foldWords(mime.QEncoding.Encode("utf-8", m.Subject))},
		{"Date", m.Date.Format(time.RFC1123Z)},
		{"Message-ID", id},
		{"MIME-Version", "1.0"},
		{"Content-Type", contentType},
	} {
		fmt.Fprintf(&out, "%s: %s\r\n", h[0], h[1])
	}
	out.WriteString("\r\n")
	out.Write(body)
	return out.Bytes(), nil
}

// alternativeBody is the text and HTML parts with their content type.
func alternativeBody(m Message) (contentType string, body []byte, err error) {
	var buf bytes.Buffer
	parts := multipart.NewWriter(&buf)
	for _, p := range []struct{ contentType, content string }{
		{"text/plain; charset=utf-8", m.Text},
		{"text/html; charset=utf-8", m.HTML},
	} {
		w, err := parts.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {p.contentType},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return "", nil, fmt.Errorf("creating a message part: %w", err)
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(p.content)); err != nil {
			return "", nil, fmt.Errorf("encoding a message part: %w", err)
		}
		if err := qp.Close(); err != nil {
			return "", nil, fmt.Errorf("encoding a message part: %w", err)
		}
	}
	if err := parts.Close(); err != nil {
		return "", nil, fmt.Errorf("closing the message: %w", err)
	}
	return mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": parts.Boundary()}), buf.Bytes(), nil
}

// base64LineLength keeps encoded attachment lines within the 78 characters
// of RFC 5322.
const base64LineLength = 76

// mixedBody wraps the alternative part and the attachments, each in base64.
func mixedBody(altType string, alt []byte, attachments []Attachment) (contentType string, body []byte, err error) {
	var buf bytes.Buffer
	parts := multipart.NewWriter(&buf)
	w, err := parts.CreatePart(textproto.MIMEHeader{"Content-Type": {altType}})
	if err != nil {
		return "", nil, fmt.Errorf("creating a message part: %w", err)
	}
	if _, err := w.Write(alt); err != nil {
		return "", nil, fmt.Errorf("writing a message part: %w", err)
	}
	for _, a := range attachments {
		w, err := parts.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {a.ContentType},
			"Content-Transfer-Encoding": {"base64"},
			"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": a.Name})},
		})
		if err != nil {
			return "", nil, fmt.Errorf("creating an attachment part: %w", err)
		}
		encoded := base64.StdEncoding.EncodeToString(a.Data)
		for len(encoded) > 0 {
			n := min(base64LineLength, len(encoded))
			if _, err := io.WriteString(w, encoded[:n]+"\r\n"); err != nil {
				return "", nil, fmt.Errorf("writing an attachment: %w", err)
			}
			encoded = encoded[n:]
		}
	}
	if err := parts.Close(); err != nil {
		return "", nil, fmt.Errorf("closing the message: %w", err)
	}
	return mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": parts.Boundary()}), buf.Bytes(), nil
}

// foldWords puts each RFC 2047 encoded word of a header value on its own
// line. Each word is at most 75 bytes, so a long name never makes a header
// line longer than SMTP allows.
func foldWords(value string) string {
	return strings.ReplaceAll(value, "?= =?", "?=\r\n =?")
}

func messageID(from string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating a message id: %w", err)
	}
	domain := "logchef"
	if at := strings.LastIndexByte(from, '@'); at >= 0 && at < len(from)-1 {
		domain = from[at+1:]
	}
	return fmt.Sprintf("<%s.report@%s>", hex.EncodeToString(b[:]), domain), nil
}
