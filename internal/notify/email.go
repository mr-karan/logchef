package notify

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// userRecipientPrefix marks a Logchef user in a delivery row's recipient. The
// user's address is read at send time, so a changed address is used.
const userRecipientPrefix = "user:"

// UserRecipient is the stored recipient of a Logchef user.
func UserRecipient(id models.UserID) string {
	return userRecipientPrefix + strconv.FormatInt(int64(id), 10)
}

type userGetter interface {
	GetUser(ctx context.Context, id models.UserID) (*models.User, error)
}

// EmailSender mails one message to one recipient of an email destination
// through the instance SMTP settings, which it reads at each send.
type EmailSender struct {
	users  userGetter
	mailer func(ctx context.Context) *Mailer
}

// NewEmailSender reads users and the SMTP settings from db.
func NewEmailSender(db interface {
	userGetter
	store.SettingGetter
}) *EmailSender {
	return &EmailSender{
		users:  db,
		mailer: func(ctx context.Context) *Mailer { return NewMailer(MailerOptionsFromSettings(ctx, db)) },
	}
}

// Send mails msg to recipient, a stored user or an address. A failure after
// the end of DATA wraps ErrOutcomeUnknown.
func (s *EmailSender) Send(ctx context.Context, _ *models.NotificationDestination, recipient string, _ int64, msg Message) error {
	to, err := s.address(ctx, recipient)
	if err != nil {
		return err
	}
	mailer := s.mailer(ctx)
	err = mailer.SendMessage(ctx, to, mailer.message(to, MailMessage{Subject: msg.Title, Text: msg.Text}))
	if err == nil {
		return nil
	}
	sendErr, ok := errors.AsType[*SendError](err)
	if !ok {
		return fmt.Errorf("email delivery failed: %w", err)
	}
	switch sendErr.Class {
	case SendPermanent:
		return Permanent(fmt.Errorf("email delivery failed: %w", sendErr.Err))
	case SendUnknown:
		return fmt.Errorf("email delivery failed: %w: %w", ErrOutcomeUnknown, sendErr.Err)
	default:
		return fmt.Errorf("email delivery failed: %w", sendErr.Err)
	}
}

func (s *EmailSender) address(ctx context.Context, recipient string) (string, error) {
	raw, isUser := strings.CutPrefix(recipient, userRecipientPrefix)
	if !isUser {
		if recipient == "" {
			return "", Permanent(errors.New("email delivery failed: the row has no recipient"))
		}
		return recipient, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return "", Permanent(fmt.Errorf("email delivery failed: invalid recipient %q", recipient))
	}
	user, err := s.users.GetUser(ctx, models.UserID(id))
	switch {
	case models.IsNotFound(err):
		return "", Permanent(fmt.Errorf("email delivery failed: recipient user %d no longer exists", id))
	case err != nil:
		return "", fmt.Errorf("email delivery failed: resolve recipient user %d: %w", id, err)
	}
	address := strings.TrimSpace(user.Email)
	if address == "" {
		return "", Permanent(fmt.Errorf("email delivery failed: recipient user %d has no email address", id))
	}
	return address, nil
}
