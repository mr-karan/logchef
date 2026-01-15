package alerts

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"log/slog"
)

const (
	smtpSecurityNone     = "none"
	smtpSecurityStartTLS = "starttls"
	smtpSecurityTLS      = "tls"
)

type EmailSenderOptions struct {
	Host          string
	Port          int
	Username      string
	Password      string
	From          string
	ReplyTo       string
	Security      string
	Timeout       time.Duration
	SkipTLSVerify bool
	Logger        *slog.Logger
}

type EmailSender struct {
	host          string
	port          int
	username      string
	password      string
	from          string
	replyTo       string
	security      string
	timeout       time.Duration
	skipTLSVerify bool
	logger        *slog.Logger
}

func NewEmailSender(opts EmailSenderOptions) *EmailSender {
	security := strings.ToLower(strings.TrimSpace(opts.Security))
	switch security {
	case smtpSecurityNone, smtpSecurityStartTLS, smtpSecurityTLS:
	case "":
		security = smtpSecurityStartTLS
	default:
		security = smtpSecurityStartTLS
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &EmailSender{
		host:          strings.TrimSpace(opts.Host),
		port:          opts.Port,
		username:      strings.TrimSpace(opts.Username),
		password:      opts.Password,
		from:          strings.TrimSpace(opts.From),
		replyTo:       strings.TrimSpace(opts.ReplyTo),
		security:      security,
		timeout:       timeout,
		skipTLSVerify: opts.SkipTLSVerify,
		logger:        logger.With("component", "alert_email_sender"),
	}
}

func (s *EmailSender) Send(ctx context.Context, notification AlertNotification) error {
	if len(notification.RecipientUserIDs) == 0 && len(notification.RecipientEmails) == 0 && notification.RecipientResolutionErr == "" {
		return nil
	}
	if notification.RecipientResolutionErr != "" {
		return fmt.Errorf("recipient resolution failed: %s", notification.RecipientResolutionErr)
	}
	if len(notification.RecipientEmails) == 0 {
		if len(notification.RecipientUserIDs) > 0 {
			return fmt.Errorf("no valid email recipients resolved")
		}
		return nil
	}
	if s.host == "" || s.port == 0 || s.from == "" {
		return fmt.Errorf("smtp is not configured")
	}

	recipients := uniqueEmails(notification.RecipientEmails)
	var errs []string
	for _, recipient := range recipients {
		message := s.buildMessage(notification, recipient)
		if err := s.sendEmail(ctx, recipient, message); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", recipient, err))
		}
	}
	if len(notification.MissingRecipientUserIDs) > 0 {
		errs = append(errs, fmt.Sprintf("missing recipient user IDs: %v", notification.MissingRecipientUserIDs))
	}
	if len(errs) > 0 {
		return fmt.Errorf("email delivery failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *EmailSender) buildMessage(notification AlertNotification, recipient string) []byte {
	subject := fmt.Sprintf("[LogChef] %s (%s) %s", notification.AlertName, strings.ToUpper(string(notification.Severity)), strings.ToUpper(string(notification.Status)))
	body := s.buildBody(notification)
	headers := []string{
		fmt.Sprintf("From: %s", s.from),
		fmt.Sprintf("To: %s", recipient),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=\"UTF-8\"",
	}
	if s.replyTo != "" {
		headers = append(headers, fmt.Sprintf("Reply-To: %s", s.replyTo))
	}
	return []byte(strings.Join(headers, "\r\n") + "\r\n\r\n" + body)
}

func (s *EmailSender) buildBody(notification AlertNotification) string {
	status := strings.ToUpper(string(notification.Status))
	severity := strings.ToUpper(string(notification.Severity))
	lines := []string{
		fmt.Sprintf("Alert: %s", notification.AlertName),
		fmt.Sprintf("Status: %s", status),
		fmt.Sprintf("Severity: %s", severity),
	}
	if notification.TeamName != "" {
		lines = append(lines, fmt.Sprintf("Team: %s", notification.TeamName))
	} else if notification.TeamID > 0 {
		lines = append(lines, fmt.Sprintf("Team ID: %d", notification.TeamID))
	}
	if notification.SourceName != "" {
		lines = append(lines, fmt.Sprintf("Source: %s", notification.SourceName))
	} else if notification.SourceID > 0 {
		lines = append(lines, fmt.Sprintf("Source ID: %d", notification.SourceID))
	}
	lines = append(lines,
		fmt.Sprintf("Value: %.4f", notification.Value),
		fmt.Sprintf("Threshold: %s %.4f", notification.ThresholdOp, notification.ThresholdValue),
	)
	if notification.FrequencySecs > 0 {
		lines = append(lines, fmt.Sprintf("Frequency: %ds", notification.FrequencySecs))
	}
	if notification.LookbackSecs > 0 {
		lines = append(lines, fmt.Sprintf("Lookback: %ds", notification.LookbackSecs))
	}
	lines = append(lines, fmt.Sprintf("Triggered At: %s", notification.TriggeredAt.Format(time.RFC3339)))
	if notification.ResolvedAt != nil {
		lines = append(lines, fmt.Sprintf("Resolved At: %s", notification.ResolvedAt.Format(time.RFC3339)))
	}
	if notification.Message != "" {
		lines = append(lines, fmt.Sprintf("Message: %s", notification.Message))
	}
	if notification.Description != "" {
		lines = append(lines, fmt.Sprintf("Description: %s", notification.Description))
	}
	if notification.Query != "" {
		lines = append(lines, fmt.Sprintf("Query: %s", notification.Query))
	}
	if notification.GeneratorURL != "" {
		lines = append(lines, fmt.Sprintf("View: %s", notification.GeneratorURL))
	}
	return strings.Join(lines, "\n") + "\n"
}

func (s *EmailSender) sendEmail(ctx context.Context, recipient string, message []byte) error {
	client, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	if err := client.Mail(s.from); err != nil {
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

func (s *EmailSender) connect(ctx context.Context) (*smtp.Client, error) {
	address := fmt.Sprintf("%s:%d", s.host, s.port)
	dialer := &net.Dialer{Timeout: s.timeout}
	var (
		conn net.Conn
		err  error
	)
	if s.security == smtpSecurityTLS {
		tlsConfig := &tls.Config{ServerName: s.host, InsecureSkipVerify: s.skipTLSVerify} // #nosec G402
		conn, err = tls.DialWithDialer(dialer, "tcp", address, tlsConfig)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, err
	}
	if s.timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(s.timeout))
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if s.security == smtpSecurityStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			_ = client.Close()
			return nil, fmt.Errorf("smtp server does not support STARTTLS")
		}
		tlsConfig := &tls.Config{ServerName: s.host, InsecureSkipVerify: s.skipTLSVerify} // #nosec G402
		if err := client.StartTLS(tlsConfig); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	if s.username != "" {
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err := client.Auth(auth); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	return client, nil
}

func uniqueEmails(emails []string) []string {
	seen := make(map[string]struct{}, len(emails))
	out := make([]string, 0, len(emails))
	for _, email := range emails {
		normalized := strings.TrimSpace(email)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}
