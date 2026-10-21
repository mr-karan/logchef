package alerts

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEmailSenderRetriesOnlyFailedRecipients(t *testing.T) {
	sender := NewEmailSender(EmailSenderOptions{})
	notification := AlertNotification{RecipientEmails: []string{"first@example.com", "second@example.com"}}
	results := sender.Send(t.Context(), notification)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.Err == nil {
			t.Fatal("unconfigured SMTP must fail each recipient")
		}
	}
	results[0].Err = nil
	raw, err := json.Marshal(map[string]any{"delivery_pending": results.failedTargets()})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	notification.deliveryTargets = pendingDeliveryTargets(payload)
	retry := sender.Send(t.Context(), notification)
	if len(retry) != 1 || retry[0].Target != results[1].Target {
		t.Fatalf("retry results = %+v, want only second recipient", retry)
	}
}

func TestEmailSenderSanitizesRecipientErrors(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = fmt.Fprint(conn, "220 localhost ESMTP\r\n")
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "EHLO") {
				_, _ = fmt.Fprint(conn, "250 localhost\r\n")
			} else {
				_, _ = fmt.Fprint(conn, "550 secret server diagnostic recipient@example.com\r\n")
				return
			}
		}
	}()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var portNumber int
	if _, err := fmt.Sscan(port, &portNumber); err != nil {
		t.Fatal(err)
	}
	sender := NewEmailSender(EmailSenderOptions{Host: host, Port: portNumber, From: "sender@example.com", Security: "none"})
	result := sender.Send(t.Context(), AlertNotification{RecipientEmails: []string{"recipient@example.com"}})
	<-done
	if err := result.Err(); err == nil || err.Error() != "email delivery failed: email recipient 1: smtp message rejected by server" {
		t.Fatalf("delivery error = %v, want a non-identifying rejection", err)
	}
}

func TestSMTPErrorCategories(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"not configured", errSMTPNotConfigured, "smtp is not configured"},
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

func TestSMTPGreetingHonorsEvaluationDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	var portNumber int
	if _, err := fmt.Sscan(port, &portNumber); err != nil {
		t.Fatal(err)
	}
	sender := NewEmailSender(EmailSenderOptions{Host: host, Port: portNumber, From: "sender@example.com", Security: "none", Timeout: 2 * time.Second})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := sender.Send(ctx, AlertNotification{RecipientEmails: []string{"recipient@example.com"}}).Err(); err == nil {
		t.Error("stalled SMTP greeting succeeded")
	}
	<-done
	if time.Since(started) > time.Second {
		t.Error("SMTP greeting exceeded the evaluation deadline by more than one second")
	}
}
