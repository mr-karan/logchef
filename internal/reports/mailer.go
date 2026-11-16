package reports

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"time"

	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/internal/store"
)

// MailMessage is one report email before it is addressed.
type MailMessage struct {
	Subject string
	Date    time.Time
	Text    string
	HTML    string
}

// Mailer sends one message to one address through the instance SMTP
// settings. A failure is a *MailError.
type Mailer interface {
	Send(ctx context.Context, to mail.Address, msg MailMessage) error
}

// MailClass says what a failed send means for a retry.
type MailClass string

const (
	// MailTransient failed before the end of DATA, or got a 4xx reply.
	MailTransient MailClass = "transient"
	// MailPermanent got a 5xx reply or an authentication failure, or SMTP
	// is not configured.
	MailPermanent MailClass = "permanent"
	// MailUnknown lost the reply to the end of DATA: the server may have
	// accepted the message, so it is never retried.
	MailUnknown MailClass = "unknown"
)

// MailError is a failed send. Err is safe to store in the run history.
type MailError struct {
	Class MailClass
	Err   error
}

func (e *MailError) Error() string { return string(e.Class) + ": " + e.Err.Error() }
func (e *MailError) Unwrap() error { return e.Err }

// ErrMailNotConfigured means the SMTP host, port or from address is not set.
var ErrMailNotConfigured = errors.New("smtp is not configured")

// SMTPMailer sends through notify.Mailer with the SMTP settings read at each
// send.
type SMTPMailer struct {
	settings store.SettingGetter
}

func NewSMTPMailer(settings store.SettingGetter) *SMTPMailer {
	return &SMTPMailer{settings: settings}
}

func (m *SMTPMailer) Send(ctx context.Context, to mail.Address, msg MailMessage) error {
	opts := notify.MailerOptionsFromSettings(ctx, m.settings)
	if opts.Host == "" || opts.Port == 0 || opts.From == "" {
		return &MailError{Class: MailPermanent, Err: ErrMailNotConfigured}
	}
	from, err := mail.ParseAddress(opts.From)
	if err != nil {
		return &MailError{Class: MailPermanent, Err: fmt.Errorf("smtp_from is not an email address")}
	}
	raw, err := BuildMessage(Message{From: *from, To: to, Subject: msg.Subject, Date: msg.Date, Text: msg.Text, HTML: msg.HTML})
	if err != nil {
		return &MailError{Class: MailPermanent, Err: err}
	}
	err = notify.NewMailer(opts).SendMessage(ctx, to.Address, raw)
	if err == nil {
		return nil
	}
	sendErr, ok := errors.AsType[*notify.SendError](err)
	if !ok {
		return &MailError{Class: MailTransient, Err: err}
	}
	class := map[notify.SendClass]MailClass{
		notify.SendTransient: MailTransient,
		notify.SendPermanent: MailPermanent,
		notify.SendUnknown:   MailUnknown,
	}[sendErr.Class]
	return &MailError{Class: class, Err: sendErr.Err}
}
