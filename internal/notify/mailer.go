package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"syscall"
	"time"

	"github.com/mr-karan/logchef/internal/store"
)

// DefaultSMTPTimeout bounds the SMTP dial and each exchange when the settings
// name no timeout.
const DefaultSMTPTimeout = 5 * time.Second

const (
	smtpSecurityNone     = "none"
	smtpSecurityStartTLS = "starttls"
	smtpSecurityTLS      = "tls"
)

var (
	// ErrSMTPNotConfigured means the SMTP host, port or from address is not set.
	ErrSMTPNotConfigured = errors.New("smtp is not configured")
	// ErrSMTPFromInvalid means the from setting does not parse as an address.
	ErrSMTPFromInvalid = errors.New("smtp_from is not an email address")
	errSMTPTLS         = errors.New("smtp TLS failed")
	errSMTPAuth        = errors.New("smtp authentication failed")
)

// SendClass says what a failed send means for a retry.
type SendClass string

const (
	// SendTransient failed before the end of DATA, or got a 4xx reply. A
	// retry cannot deliver a second copy.
	SendTransient SendClass = "transient"
	// SendPermanent got a 5xx reply or an authentication failure, or SMTP is
	// not configured. A retry fails the same way.
	SendPermanent SendClass = "permanent"
	// SendUnknown lost the connection after the end of DATA was written and
	// before the reply: the server may have accepted the message.
	SendUnknown SendClass = "unknown"
)

// SendError is a failed SendMessage. Err is sanitized and safe to store.
type SendError struct {
	Class SendClass
	Err   error
}

func (e *SendError) Error() string { return string(e.Class) + ": " + e.Err.Error() }
func (e *SendError) Unwrap() error { return e.Err }

// MailerOptions are the instance SMTP settings.
type MailerOptions struct {
	Host          string
	Port          int
	Username      string
	Password      string
	From          string
	ReplyTo       string
	Security      string
	Timeout       time.Duration
	SkipTLSVerify bool
}

// MailerOptionsFromSettings reads the instance SMTP settings, which an admin
// can change at any time, so a sender reads them for each send.
func MailerOptionsFromSettings(ctx context.Context, settings store.SettingGetter) MailerOptions {
	return MailerOptions{
		Host:          store.GetSettingWithDefault(ctx, settings, "alerts.smtp_host", ""),
		Port:          store.GetIntSetting(ctx, settings, "alerts.smtp_port", 587),
		Username:      store.GetSettingWithDefault(ctx, settings, "alerts.smtp_username", ""),
		Password:      store.GetSettingWithDefault(ctx, settings, "alerts.smtp_password", ""),
		From:          store.GetSettingWithDefault(ctx, settings, "alerts.smtp_from", ""),
		ReplyTo:       store.GetSettingWithDefault(ctx, settings, "alerts.smtp_reply_to", ""),
		Security:      store.GetSettingWithDefault(ctx, settings, "alerts.smtp_security", "starttls"),
		Timeout:       store.GetDurationSetting(ctx, settings, "alerts.request_timeout", DefaultSMTPTimeout),
		SkipTLSVerify: store.GetBoolSetting(ctx, settings, "alerts.tls_insecure_skip_verify", false),
	}
}

// FromAddress parses the from setting, a bare address or one with a display
// name such as "Logchef <logchef@example.com>". The SMTP envelope takes its
// Address, the From header the whole value.
func (o MailerOptions) FromAddress() (*mail.Address, error) {
	from, err := mail.ParseAddress(strings.TrimSpace(o.From))
	if err != nil {
		return nil, ErrSMTPFromInvalid
	}
	return from, nil
}

// Mailer sends one message to one address through the instance SMTP
// settings. It never retries.
type Mailer struct {
	host          string
	port          int
	username      string
	password      string
	rawFrom       string
	from          *mail.Address
	fromErr       error
	replyTo       string
	security      string
	timeout       time.Duration
	skipTLSVerify bool
}

// MailMessage is one plain-text message for one recipient.
type MailMessage struct {
	Subject string
	Text    string
}

func NewMailer(opts MailerOptions) *Mailer {
	security := strings.ToLower(strings.TrimSpace(opts.Security))
	switch security {
	case smtpSecurityNone, smtpSecurityStartTLS, smtpSecurityTLS:
	default:
		security = smtpSecurityStartTLS
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultSMTPTimeout
	}
	from, fromErr := opts.FromAddress()
	return &Mailer{
		host:          strings.TrimSpace(opts.Host),
		port:          opts.Port,
		username:      strings.TrimSpace(opts.Username),
		password:      opts.Password,
		rawFrom:       strings.TrimSpace(opts.From),
		from:          from,
		fromErr:       fromErr,
		replyTo:       strings.TrimSpace(opts.ReplyTo),
		security:      security,
		timeout:       timeout,
		skipTLSVerify: opts.SkipTLSVerify,
	}
}

// Configured reports whether host, port and from are set.
func (s *Mailer) Configured() bool {
	return s.host != "" && s.port != 0 && s.rawFrom != ""
}

// Send delivers a plain-text message to one address. The error never
// carries server text, credentials or addresses.
func (s *Mailer) Send(ctx context.Context, to string, msg MailMessage) error {
	if !s.Configured() {
		return ErrSMTPNotConfigured
	}
	if s.fromErr != nil {
		return s.fromErr
	}
	if err := s.deliver(ctx, to, s.message(to, msg)); err != nil {
		return sanitizeSMTPError(err)
	}
	return nil
}

// message renders the headers and body. A header value can carry text from
// log data, such as a group value in a subject, so it is kept on one line and
// cannot add a header.
func (s *Mailer) message(to string, msg MailMessage) []byte {
	headers := []string{
		"From: " + oneLine(s.fromHeader()),
		"To: " + oneLine(to),
		"Subject: " + oneLine(msg.Subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=\"UTF-8\"",
	}
	if s.replyTo != "" {
		headers = append(headers, "Reply-To: "+oneLine(s.replyTo))
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + msg.Text)
}

func (s *Mailer) fromHeader() string {
	if s.from == nil {
		return s.rawFrom
	}
	return s.from.String()
}

func oneLine(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, value)
}

// SendMessage sends a prepared MIME message to one address and classifies a
// failure. A 250 reply to the end of DATA is a delivery, whatever QUIT then
// returns, so a caller that retries on SendTransient never sends a second
// copy.
func (s *Mailer) SendMessage(ctx context.Context, to string, message []byte) error {
	if !s.Configured() {
		return &SendError{Class: SendPermanent, Err: ErrSMTPNotConfigured}
	}
	if s.fromErr != nil {
		return &SendError{Class: SendPermanent, Err: s.fromErr}
	}
	client, err := s.connect(ctx)
	if err != nil {
		return classifyBeforeDataEnd(err)
	}
	defer client.Close()
	if err := client.Mail(s.from.Address); err != nil {
		return classifyBeforeDataEnd(err)
	}
	if err := client.Rcpt(to); err != nil {
		return classifyBeforeDataEnd(err)
	}
	writer, err := client.Data()
	if err != nil {
		return classifyBeforeDataEnd(err)
	}
	// On a failed write the end of DATA is never sent, so the server drops
	// the partial message when the connection closes.
	if _, err := writer.Write(message); err != nil {
		return classifyBeforeDataEnd(err)
	}
	if err := writer.Close(); err != nil {
		if _, ok := errors.AsType[*textproto.Error](err); ok {
			return classifyBeforeDataEnd(err)
		}
		return &SendError{Class: SendUnknown, Err: sanitizeSMTPError(err)}
	}
	_ = client.Quit()
	return nil
}

func classifyBeforeDataEnd(err error) error {
	class := SendTransient
	if reply, ok := errors.AsType[*textproto.Error](err); ok && reply.Code >= 500 {
		class = SendPermanent
	}
	if errors.Is(err, errSMTPAuth) {
		class = SendPermanent
	}
	return &SendError{Class: class, Err: sanitizeSMTPError(err)}
}

func (s *Mailer) deliver(ctx context.Context, recipient string, message []byte) error {
	client, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Mail(s.from.Address); err != nil {
		return err
	}
	if err := client.Rcpt(recipient); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (s *Mailer) connect(ctx context.Context) (*smtp.Client, error) {
	address := fmt.Sprintf("%s:%d", s.host, s.port)
	dialer := &net.Dialer{Timeout: s.timeout}
	var (
		conn net.Conn
		err  error
	)
	if s.security == smtpSecurityTLS {
		tlsConfig := &tls.Config{ServerName: s.host, InsecureSkipVerify: s.skipTLSVerify} // #nosec G402
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: tlsConfig}
		conn, err = tlsDialer.DialContext(ctx, "tcp", address)
		if err != nil {
			err = fmt.Errorf("%w: %w", errSMTPTLS, err)
		}
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(s.timeout)
	if evaluationDeadline, ok := ctx.Deadline(); ok && evaluationDeadline.Before(deadline) {
		deadline = evaluationDeadline
	}
	_ = conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if s.security == smtpSecurityStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			return nil, errSMTPTLS
		}
		tlsConfig := &tls.Config{ServerName: s.host, InsecureSkipVerify: s.skipTLSVerify} // #nosec G402
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("%w: %w", errSMTPTLS, err)
		}
	}
	if s.username != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err := client.Auth(auth); err != nil {
			_ = client.Close()
			return nil, fmt.Errorf("%w: %w", errSMTPAuth, err)
		}
	}
	return client, nil
}

func sanitizeSMTPError(err error) error {
	if errors.Is(err, ErrSMTPNotConfigured) {
		return ErrSMTPNotConfigured
	}
	if errors.Is(err, context.Canceled) {
		return errors.New("smtp request canceled")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("smtp request timed out")
	}
	if networkErr, ok := errors.AsType[net.Error](err); ok && networkErr.Timeout() {
		return errors.New("smtp request timed out")
	}
	if _, ok := errors.AsType[*net.DNSError](err); ok {
		return errors.New("smtp DNS lookup failed")
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return errors.New("smtp connection refused")
	}
	if errors.Is(err, errSMTPTLS) {
		return errSMTPTLS
	}
	if _, ok := errors.AsType[*tls.CertificateVerificationError](err); ok {
		return errSMTPTLS
	}
	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return errSMTPTLS
	}
	if errors.Is(err, errSMTPAuth) {
		return errSMTPAuth
	}
	if reply, ok := errors.AsType[*textproto.Error](err); ok && (reply.Code == 530 || reply.Code == 534 || reply.Code == 535 || reply.Code == 538) {
		return errSMTPAuth
	}
	if reply, ok := errors.AsType[*textproto.Error](err); ok && reply.Code >= 400 && reply.Code < 600 {
		return errors.New("smtp message rejected by server")
	}
	return errors.New("smtp connection failed")
}
