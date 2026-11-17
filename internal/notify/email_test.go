package notify

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// fixedMailerSender is an email sender over fixed SMTP settings.
func fixedMailerSender(opts MailerOptions) *EmailSender {
	return &EmailSender{mailer: func(context.Context) *Mailer { return NewMailer(opts) }}
}

func emailDestination() *models.NotificationDestination {
	return &models.NotificationDestination{ID: 1, Name: "mail", Type: models.DestinationTypeEmail, IsActive: true}
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
	sender := fixedMailerSender(MailerOptions{Host: host, Port: portNumber, From: "sender@example.com", Security: "none"})
	err = sender.Send(t.Context(), emailDestination(), "recipient@example.com", 1, Message{Title: "t", Text: "x"})
	<-done
	if err == nil || err.Error() != "email delivery failed: smtp message rejected by server" || !IsPermanent(err) {
		t.Fatalf("delivery error = %v, want a permanent, non-identifying rejection", err)
	}
}

func TestSMTPGreetingHonorsSendDeadline(t *testing.T) {
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
	sender := fixedMailerSender(MailerOptions{Host: host, Port: portNumber, From: "sender@example.com", Security: "none", Timeout: 2 * time.Second})
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := sender.Send(ctx, emailDestination(), "recipient@example.com", 1, Message{}); err == nil {
		t.Error("stalled SMTP greeting succeeded")
	}
	<-done
	if time.Since(started) > time.Second {
		t.Error("SMTP greeting exceeded the send deadline by more than one second")
	}
}
