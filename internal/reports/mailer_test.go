package reports

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// fakeSMTP is a plain SMTP server that answers each step as a test says.
// A reply of "drop" closes the connection instead of answering.
type fakeSMTP struct {
	t        *testing.T
	ln       net.Listener
	rcpt     string
	dataEnd  string
	quit     string
	mu       sync.Mutex
	sessions int
	messages []string
	wg       sync.WaitGroup
}

func newFakeSMTP(t *testing.T, rcpt, dataEnd, quit string) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{t: t, ln: ln, rcpt: rcpt, dataEnd: dataEnd, quit: quit}
	f.wg.Go(f.serve)
	t.Cleanup(func() {
		_ = ln.Close()
		f.wg.Wait()
	})
	return f
}

func (f *fakeSMTP) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.sessions++
		f.mu.Unlock()
		f.wg.Go(func() { f.session(conn) })
	}
}

func (f *fakeSMTP) session(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	reply := func(line string) bool {
		if line == "drop" {
			return false
		}
		_, err := io.WriteString(conn, line+"\r\n")
		return err == nil
	}
	if !reply("220 fake ESMTP") {
		return
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimSpace(line))
		var ok bool
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			ok = reply("250 fake")
		case strings.HasPrefix(cmd, "MAIL FROM"):
			ok = reply("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO"):
			ok = reply(f.rcpt)
		case cmd == "DATA":
			if !reply("354 go ahead") {
				return
			}
			var msg strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				msg.WriteString(l)
			}
			f.mu.Lock()
			f.messages = append(f.messages, msg.String())
			f.mu.Unlock()
			ok = reply(f.dataEnd)
		case cmd == "QUIT":
			reply(f.quit)
			return
		default:
			ok = reply("250 ok")
		}
		if !ok {
			return
		}
	}
}

func (f *fakeSMTP) counts() (sessions, messages int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sessions, len(f.messages)
}

type settingsMap map[string]string

func (m settingsMap) GetSetting(_ context.Context, key string) (string, error) {
	if v, ok := m[key]; ok {
		return v, nil
	}
	return "", models.ErrNotFound
}

// smtpMailer is the adapter under test. The suite below takes any Mailer
// built for an SMTP host, so a later adapter runs the same tests.
func smtpMailer(host string, port int) Mailer {
	return NewSMTPMailer(settingsMap{
		"alerts.smtp_host":     host,
		"alerts.smtp_port":     strconv.Itoa(port),
		"alerts.smtp_from":     "Logchef <reports@example.com>",
		"alerts.smtp_security": "none",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func fakeAddr(t *testing.T, f *fakeSMTP) (host string, port int) {
	t.Helper()
	addr := f.ln.Addr().(*net.TCPAddr)
	return addr.IP.String(), addr.Port
}

var testMail = MailMessage{Subject: "[Logchef] Daily 5xx, 2026-11-16", Date: time.Now(), Text: "hello", HTML: "<p>hello</p>"}

var testRecipient = Recipient{Address: mail.Address{Address: "owner@example.com"}, Class: models.ReportDeliveryUser, Target: "7", Via: "direct"}

func runMailerSuite(t *testing.T, newMailer func(host string, port int) Mailer) {
	for _, tc := range []struct {
		name                 string
		rcpt, dataEnd, quit  string
		wantClass            MailClass
		wantSessions, wantDS int
	}{
		{"accepted", "250 ok", "250 queued", "221 bye", "", 1, 1},
		{"quit fails after a 250 is still sent", "250 ok", "250 queued", "drop", "", 1, 1},
		{"4xx before data is transient", "451 try later", "250 queued", "221 bye", MailTransient, 1, 0},
		{"5xx before data is permanent", "550 no such user", "250 queued", "221 bye", MailPermanent, 1, 0},
		{"4xx to the end of data is transient", "250 ok", "451 try later", "221 bye", MailTransient, 1, 1},
		{"5xx to the end of data is permanent", "250 ok", "554 rejected", "221 bye", MailPermanent, 1, 1},
		{"a drop after the end of data is unknown", "250 ok", "drop", "221 bye", MailUnknown, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSMTP(t, tc.rcpt, tc.dataEnd, tc.quit)
			err := newMailer(fakeAddr(t, f)).Send(context.Background(), testRecipient.Address, testMail)
			if tc.wantClass == "" {
				if err != nil {
					t.Fatalf("Send = %v, want nil", err)
				}
			} else {
				mailErr, ok := errors.AsType[*MailError](err)
				if !ok || mailErr.Class != tc.wantClass {
					t.Fatalf("Send = %v, want a %s MailError", err, tc.wantClass)
				}
			}
			sessions, messages := f.counts()
			if sessions != tc.wantSessions || messages != tc.wantDS {
				t.Errorf("server saw %d sessions and %d messages, want %d and %d", sessions, messages, tc.wantSessions, tc.wantDS)
			}
		})
	}
	t.Run("not configured is permanent", func(t *testing.T) {
		err := newMailer("", 0).Send(context.Background(), testRecipient.Address, testMail)
		mailErr, ok := errors.AsType[*MailError](err)
		if !ok || mailErr.Class != MailPermanent || !errors.Is(err, ErrMailNotConfigured) {
			t.Fatalf("Send = %v, want a permanent ErrMailNotConfigured", err)
		}
	})
	t.Run("connection refused is transient", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		_ = ln.Close()
		err = newMailer("127.0.0.1", port).Send(context.Background(), testRecipient.Address, testMail)
		if mailErr, ok := errors.AsType[*MailError](err); !ok || mailErr.Class != MailTransient {
			t.Fatalf("Send = %v, want a transient MailError", err)
		}
	})
}

func TestSMTPMailer(t *testing.T) {
	runMailerSuite(t, smtpMailer)
}

// TestDeliverRetries runs Deliver over the SMTP adapter: a transient
// failure is tried three times, a permanent one once, and an unknown one
// once, so a recipient never gets an edition twice.
func TestDeliverRetries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rcpt         string
		dataEnd      string
		wantStatus   models.ReportDeliveryStatus
		wantAttempts int
		wantWaits    []time.Duration
	}{
		{"sent", "250 ok", "250 queued", models.ReportDeliverySent, 1, nil},
		{"transient", "451 try later", "250 queued", models.ReportDeliveryFailed, 3, []time.Duration{2 * time.Second, 8 * time.Second}},
		{"permanent", "550 no such user", "250 queued", models.ReportDeliveryFailed, 1, nil},
		{"unknown", "250 ok", "drop", models.ReportDeliveryUnknown, 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSMTP(t, tc.rcpt, tc.dataEnd, "221 bye")
			var waits []time.Duration
			d := Delivery{
				Mailer: smtpMailer(fakeAddr(t, f)),
				Sleep:  func(_ context.Context, w time.Duration) error { waits = append(waits, w); return nil },
			}
			got := d.Deliver(context.Background(), testRecipient, testMail, time.Now().Add(time.Minute))
			if got.Status != tc.wantStatus || got.Attempts != tc.wantAttempts {
				t.Fatalf("delivery = %+v, want %s after %d attempts", got, tc.wantStatus, tc.wantAttempts)
			}
			if sessions, _ := f.counts(); sessions != tc.wantAttempts {
				t.Errorf("server saw %d sessions, want %d", sessions, tc.wantAttempts)
			}
			if !slices.Equal(waits, tc.wantWaits) {
				t.Errorf("waits = %v, want %v", waits, tc.wantWaits)
			}
			if got.Target != "7" || got.Via != "direct" || got.Class != models.ReportDeliveryUser {
				t.Errorf("delivery identity = %+v", got)
			}
		})
	}
}

func TestDeliverStopsAtTheDeadline(t *testing.T) {
	f := newFakeSMTP(t, "451 try later", "250 queued", "221 bye")
	d := Delivery{Mailer: smtpMailer(fakeAddr(t, f)), Sleep: func(context.Context, time.Duration) error { return nil }}

	got := d.Deliver(context.Background(), testRecipient, testMail, time.Now().Add(-time.Second))
	if got.Status != models.ReportDeliverySkipped || got.Reason != "deadline" || got.Attempts != 0 {
		t.Errorf("past the deadline = %+v, want skipped with reason deadline", got)
	}
	// A retry that would wait past the deadline is not made.
	got = d.Deliver(context.Background(), testRecipient, testMail, time.Now().Add(time.Second))
	if got.Status != models.ReportDeliveryFailed || got.Attempts != 1 {
		t.Errorf("retry past the deadline = %+v, want failed after 1 attempt", got)
	}
}
