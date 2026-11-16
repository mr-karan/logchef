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
