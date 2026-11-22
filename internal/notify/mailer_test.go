package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"syscall"
	"testing"
)

func TestSMTPErrorCategories(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"not configured", ErrSMTPNotConfigured, "smtp is not configured"},
		{"refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, "smtp connection refused"},
		{"DNS", &net.DNSError{Name: "private.invalid", Err: "private lookup details"}, "smtp DNS lookup failed"},
		{"TLS", &tls.CertificateVerificationError{Err: errors.New("private certificate details")}, "smtp TLS failed"},
		{"auth", &textproto.Error{Code: 535, Msg: "private authentication details"}, "smtp authentication failed"},
		{"timeout", fmt.Errorf("private server: %w", context.DeadlineExceeded), "smtp request timed out"},
		{"reset", &net.OpError{Op: "read", Err: syscall.ECONNRESET}, "smtp connection failed"},
		{"host unreachable", &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}, "smtp connection failed"},
		{"network unreachable", &net.OpError{Op: "dial", Err: syscall.ENETUNREACH}, "smtp connection failed"},
		{"greeting EOF", fmt.Errorf("private server: %w", io.EOF), "smtp connection failed"},
		{"rejected", &textproto.Error{Code: 550, Msg: "private recipient details"}, "smtp message rejected by server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeSMTPError(tc.err).Error(); got != tc.want {
				t.Errorf("sanitized error = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMailerKeepsHeadersOnOneLine: a line break in a subject cannot start a
// new header.
func TestMailerKeepsHeadersOnOneLine(t *testing.T) {
	m := NewMailer(MailerOptions{Host: "smtp.example.com", Port: 25, From: "logchef@example.com"})
	message := string(m.message("oncall@example.com", MailMessage{Subject: "errors [service=checkout\r\nBcc: victim@example.com]", Text: "body"}))
	headers, body, _ := strings.Cut(message, "\r\n\r\n")
	if strings.Contains(headers, "\r\nBcc:") || !strings.Contains(headers, "Subject: errors [service=checkout  Bcc: victim@example.com]") {
		t.Errorf("headers = %q", headers)
	}
	if body != "body" {
		t.Errorf("body = %q", body)
	}
}

// TestMailerSendsDisplayNameFrom: a From setting with a display name puts the
// bare address in MAIL FROM, which a strict server requires, and keeps the
// name in the From header. Both send paths are covered: Send (the test email)
// and SendMessage (alert and report emails).
func TestMailerSendsDisplayNameFrom(t *testing.T) {
	server := startFakeSMTP(t)
	opts := server.options()
	opts.From = "Logchef <logchef@shop.example>"

	if err := NewMailer(opts).Send(t.Context(), "oncall@example.com", MailMessage{Subject: "test", Text: "body"}); err != nil {
		t.Fatalf("Send = %v", err)
	}
	sender := fixedMailerSender(opts)
	if err := sender.Send(t.Context(), emailDestination(), "oncall@example.com", 1, Message{Title: "alert", Text: "body"}); err != nil {
		t.Fatalf("EmailSender.Send = %v", err)
	}

	server.mu.Lock()
	messages := append([]string(nil), server.messages...)
	server.mu.Unlock()
	if len(messages) != 2 {
		t.Fatalf("server received %d messages, want 2", len(messages))
	}
	for _, message := range messages {
		if !strings.Contains(message, "From: \"Logchef\" <logchef@shop.example>\r\n") {
			t.Errorf("message headers lack the display-name From: %q", message)
		}
	}
}

func TestMailerRefusesAFromThatIsNotAnAddress(t *testing.T) {
	server := startFakeSMTP(t)
	opts := server.options()
	opts.From = "Logchef <not an address"
	err := NewMailer(opts).SendMessage(t.Context(), "oncall@example.com", []byte("Subject: x\r\n\r\nbody"))
	sendErr, ok := errors.AsType[*SendError](err)
	if !ok || sendErr.Class != SendPermanent || !errors.Is(err, ErrSMTPFromInvalid) {
		t.Fatalf("SendMessage = %v, want a permanent ErrSMTPFromInvalid", err)
	}
	if rcpts, _ := server.snapshot(); len(rcpts) != 0 {
		t.Errorf("server saw recipients %v, want no session", rcpts)
	}
}
