package reports

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestBuildMessage(t *testing.T) {
	loc := kolkata(t)
	at := time.Date(2026, 11, 15, 22, 30, 0, 0, time.UTC) // 2026-11-16 04:00 IST
	name := "Résumé des erreurs 5xx, 日次"
	text := "Plain body with a long line " + strings.Repeat("é", 120) + "\n"
	html := `<p style="color:#000">` + strings.Repeat("x", 200) + "</p>"
	raw, err := BuildMessage(Message{
		From:    mail.Address{Name: "Logchef", Address: "logchef@example.com"},
		To:      mail.Address{Name: "Ünïcode User", Address: "user@example.com"},
		Subject: Subject(name, at, loc),
		Date:    at,
		Text:    text,
		HTML:    html,
	})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(msg.Header["Subject"]); n != 1 {
		t.Fatalf("message has %d Subject headers, want 1", n)
	}
	encoded := msg.Header.Get("Subject")
	if strings.ContainsFunc(encoded, func(r rune) bool { return r > 127 }) {
		t.Errorf("Subject header is not ASCII: %q", encoded)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[Logchef] " + name + ", 2026-11-16"; subject != want {
		t.Errorf("subject = %q, want %q (the date in the report timezone)", subject, want)
	}
	if to, err := msg.Header.AddressList("To"); err != nil || len(to) != 1 || to[0].Address != "user@example.com" || to[0].Name != "Ünïcode User" {
		t.Errorf("To = %v, %v", to, err)
	}
	if msg.Header.Get("Message-ID") == "" || msg.Header.Get("MIME-Version") != "1.0" {
		t.Errorf("headers = %v", msg.Header)
	}
	if d, err := msg.Header.Date(); err != nil || !d.Equal(at) {
		t.Errorf("Date = %v, %v", d, err)
	}

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/alternative" {
		t.Fatalf("Content-Type = %q, %v", mediaType, err)
	}
	reader := multipart.NewReader(msg.Body, params["boundary"])
	var parts []string
	var bodies []string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, part.Header.Get("Content-Type"))
		bodies = append(bodies, string(body))
	}
	if strings.Join(parts, ",") != "text/plain; charset=utf-8,text/html; charset=utf-8" {
		t.Fatalf("parts = %v, want text then html", parts)
	}
	// Quoted-printable text mode writes line breaks as CRLF, as SMTP wants.
	if got := strings.ReplaceAll(bodies[0], "\r\n", "\n"); got != text {
		t.Errorf("decoded text = %q, want %q", got, text)
	}
	if bodies[1] != html {
		t.Errorf("decoded html = %q, want %q", bodies[1], html)
	}
	for i, line := range strings.Split(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Errorf("line %d is %d bytes, over the SMTP limit", i, len(line))
		}
	}
}

// TestBuildMessageLongNames sends the longest name validation allows, in
// three-byte runes, to a user with a long non-ASCII name. Every header line
// must stay within the SMTP limit and decode back.
func TestBuildMessageLongNames(t *testing.T) {
	name := strings.Repeat("日", 200)
	person := strings.Repeat("Ü", 120)
	raw, err := BuildMessage(Message{
		From:    mail.Address{Name: person, Address: "logchef@example.com"},
		To:      mail.Address{Name: person, Address: "user@example.com"},
		Subject: Subject(name, time.Unix(0, 0), time.UTC),
		Date:    time.Unix(0, 0),
		Text:    "t",
		HTML:    "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(raw), "\r\n\r\n")
	for line := range strings.SplitSeq(head, "\r\n") {
		if len(line) > 998 {
			t.Errorf("a header line is %d bytes, over the SMTP limit of 998", len(line))
		}
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "[Logchef] "+name+", 1970-01-01" {
		t.Errorf("subject = %q, %v", subject, err)
	}
	if to, err := msg.Header.AddressList("To"); err != nil || len(to) != 1 || to[0].Name != person {
		t.Errorf("To = %v, %v", to, err)
	}
}

func TestBuildMessageHeaderInjection(t *testing.T) {
	raw, err := BuildMessage(Message{
		From:    mail.Address{Address: "logchef@example.com"},
		To:      mail.Address{Address: "user@example.com"},
		Subject: "Report\r\nBcc: attacker@example.com",
		Date:    time.Unix(0, 0),
		Text:    "t",
		HTML:    "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Bcc") != "" || len(msg.Header["Subject"]) != 1 {
		t.Errorf("a subject with CR LF added a header: %v", msg.Header)
	}
}

// TestBuildMessageWithAnAttachment parses a message with a CSV: a
// multipart/mixed body holds the text and HTML alternative, then the file.
func TestBuildMessageWithAnAttachment(t *testing.T) {
	data := []byte("status,n\n" + strings.Repeat("503,1\n", 200))
	raw, err := BuildMessage(Message{
		From: mail.Address{Address: "logchef@example.com"}, To: mail.Address{Address: "user@example.com"},
		Subject: "x", Date: time.Now(), Text: "text body", HTML: "<p>html body</p>",
		Attachments: []Attachment{{Name: "Résumé-2026-11-16.csv", ContentType: "text/csv; charset=utf-8", Data: data}},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/mixed" {
		t.Fatalf("content type = %q, %v; want multipart/mixed", mediaType, err)
	}
	outer := multipart.NewReader(msg.Body, params["boundary"])
	alt, err := outer.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	altType, altParams, err := mime.ParseMediaType(alt.Header.Get("Content-Type"))
	if err != nil || altType != "multipart/alternative" {
		t.Fatalf("first part = %q, %v; want multipart/alternative", altType, err)
	}
	inner := multipart.NewReader(alt, altParams["boundary"])
	for _, want := range []string{"text/plain", "text/html"} {
		p, err := inner.NextPart()
		if err != nil || !strings.HasPrefix(p.Header.Get("Content-Type"), want) {
			t.Fatalf("alternative part = %v, %v; want %s", p, err, want)
		}
	}
	file, err := outer.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if file.FileName() != "Résumé-2026-11-16.csv" || !strings.HasPrefix(file.Header.Get("Content-Type"), "text/csv") {
		t.Errorf("attachment = %q, %q", file.FileName(), file.Header.Get("Content-Type"))
	}
	encoded, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(encoded)), "\r\n") {
		if len(line) > base64LineLength {
			t.Fatalf("base64 line of %d bytes", len(line))
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(encoded), "\r\n", ""))
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatalf("attachment decodes to %d bytes, %v; want the %d bytes sent", len(decoded), err, len(data))
	}
	for line := range strings.SplitSeq(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line of %d bytes, over the SMTP limit", len(line))
		}
	}
	if _, err := outer.NextPart(); !errors.Is(err, io.EOF) {
		t.Errorf("want two parts, got more: %v", err)
	}
}
