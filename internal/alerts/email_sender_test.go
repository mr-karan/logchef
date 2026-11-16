package alerts

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/notify"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestEmailSenderRetriesOnlyFailedRecipients(t *testing.T) {
	sender := NewEmailSender(notify.NewMailer(notify.MailerOptions{}))
	notification := AlertNotification{Emails: testEmails("first@example.com", "second@example.com")}
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
	sender := NewEmailSender(notify.NewMailer(notify.MailerOptions{Host: host, Port: portNumber, From: "sender@example.com", Security: "none"}))
	result := sender.Send(t.Context(), AlertNotification{Emails: testEmails("recipient@example.com")})
	<-done
	if err := result.Err(); err == nil || err.Error() != "email delivery failed: email recipient 1: smtp message rejected by server" {
		t.Fatalf("delivery error = %v, want a non-identifying rejection", err)
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
	sender := NewEmailSender(notify.NewMailer(notify.MailerOptions{Host: host, Port: portNumber, From: "sender@example.com", Security: "none", Timeout: 2 * time.Second}))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := sender.Send(ctx, AlertNotification{Emails: testEmails("recipient@example.com")}).Err(); err == nil {
		t.Error("stalled SMTP greeting succeeded")
	}
	<-done
	if time.Since(started) > time.Second {
		t.Error("SMTP greeting exceeded the evaluation deadline by more than one second")
	}
}

// TestEmailNamesTheGroup: a grouped rule's email names its group in the
// subject and the body, and a line break in a group value from log data
// cannot start a new header.
func TestEmailNamesTheGroup(t *testing.T) {
	notification := AlertNotification{
		AlertName: "errors", Severity: models.AlertSeverityCritical, Status: models.AlertStatusTriggered,
		Group: map[string]string{"service": "checkout\r\nBcc: victim@example.com"},
	}
	subject := emailSubject(notification)
	if want := "[Logchef] errors [service=checkout  Bcc: victim@example.com] (CRITICAL) TRIGGERED"; subject != want {
		t.Errorf("subject = %q, want %q", subject, want)
	}
	if body := (&EmailSender{}).buildBody(notification); !strings.Contains(body, "Group: service=checkout") {
		t.Errorf("body = %q, want a Group line", body)
	}
}
