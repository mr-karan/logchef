package reports

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
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
}

// Subject is "[Logchef] <report name>, <date of at in loc>".
func Subject(reportName string, at time.Time, loc *time.Location) string {
	return fmt.Sprintf("[Logchef] %s, %s", reportName, at.In(loc).Format(time.DateOnly))
}

// BuildMessage writes m as a multipart/alternative MIME message with a text
// part and an HTML part. The subject is an RFC 2047 encoded word whenever it
// holds anything but printable ASCII, so a name cannot add a header.
func BuildMessage(m Message) ([]byte, error) {
	if m.From.Address == "" || m.To.Address == "" {
		return nil, fmt.Errorf("a report message needs a from and a to address")
	}
	var body bytes.Buffer
	parts := multipart.NewWriter(&body)
	for _, p := range []struct{ contentType, content string }{
		{"text/plain; charset=utf-8", m.Text},
		{"text/html; charset=utf-8", m.HTML},
	} {
		w, err := parts.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {p.contentType},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, fmt.Errorf("creating a message part: %w", err)
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(p.content)); err != nil {
			return nil, fmt.Errorf("encoding a message part: %w", err)
		}
		if err := qp.Close(); err != nil {
			return nil, fmt.Errorf("encoding a message part: %w", err)
		}
	}
	if err := parts.Close(); err != nil {
		return nil, fmt.Errorf("closing the message: %w", err)
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
		{"Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": parts.Boundary()})},
	} {
		fmt.Fprintf(&out, "%s: %s\r\n", h[0], h[1])
	}
	out.WriteString("\r\n")
	out.Write(body.Bytes())
	return out.Bytes(), nil
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
