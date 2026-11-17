// Package notify owns notification destinations and the SMTP mailer (RFC
// 0006): what a destination may hold, who it reaches, and how one email
// reaches one address. internal/alerts and internal/reports decide what to
// say.
package notify

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// MaxEmailRecipients bounds the users and addresses of one email destination.
const MaxEmailRecipients = 20

// Webhook payload versions. Version 1 is the payload of a rule's webhook
// before destinations existed, kept for receivers that parse it; version 2
// is the RFC 0006 body, the default for a new destination.
const (
	WebhookPayloadV1 = 1
	WebhookPayloadV2 = 2
)

// ValidateWebhookURL accepts an absolute http or https URL with a host.
func ValidateWebhookURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return errors.New("the webhook URL must be an absolute URL with a host")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("the webhook URL must use http or https")
	}
	return nil
}

// slackWebhookHosts are the hosts of Slack incoming webhooks.
var slackWebhookHosts = []string{"hooks.slack.com", "hooks.slack-gov.com"}

// ValidateSlackWebhookURL accepts an https Slack incoming webhook URL, so a
// Slack destination cannot post its message to another host.
func ValidateSlackWebhookURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || !slices.Contains(slackWebhookHosts, strings.ToLower(parsed.Host)) {
		return errors.New("the Slack webhook URL must start with https://hooks.slack.com/ or https://hooks.slack-gov.com/")
	}
	return nil
}

// pagerDutyRoutingKey is the integration key of a PagerDuty Events API v2
// integration.
var pagerDutyRoutingKey = regexp.MustCompile(`^[A-Za-z0-9]{32}$`)

// ValidatePagerDutyRoutingKey accepts a 32-character routing key.
func ValidatePagerDutyRoutingKey(key string) error {
	if !pagerDutyRoutingKey.MatchString(key) {
		return errors.New("the PagerDuty routing key is the 32 letters and digits of an Events API v2 integration key")
	}
	return nil
}

// NormalizeEmailConfig trims, deduplicates and checks the recipients of an
// email destination. It does not check that the users exist.
func NormalizeEmailConfig(config models.DestinationConfig) (models.DestinationConfig, error) {
	out := models.DestinationConfig{}
	for _, id := range config.UserIDs {
		if id <= 0 {
			return out, fmt.Errorf("invalid user id %d", id)
		}
		if !slices.Contains(out.UserIDs, id) {
			out.UserIDs = append(out.UserIDs, id)
		}
	}
	for _, raw := range config.Addresses {
		address := strings.TrimSpace(raw)
		if address == "" {
			continue
		}
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address {
			return out, fmt.Errorf("invalid email address %q", address)
		}
		if !slices.Contains(out.Addresses, address) {
			out.Addresses = append(out.Addresses, address)
		}
	}
	switch n := len(out.UserIDs) + len(out.Addresses); {
	case n == 0:
		return out, errors.New("an email destination needs at least one recipient")
	case n > MaxEmailRecipients:
		return out, fmt.Errorf("an email destination has at most %d recipients", MaxEmailRecipients)
	}
	return out, nil
}

// Recipient is one recipient of an email destination: a Logchef user, kept
// as a user id, or an external address.
type Recipient struct {
	UserID  *models.UserID
	Address string
}

// ExpandEmailDestination returns the recipients of an email destination.
// A user is resolved to the current address, as at send time; a user who no
// longer exists, or has no address, is returned with an empty Address.
func ExpandEmailDestination(ctx context.Context, db store.StoreOps, d *models.NotificationDestination) ([]Recipient, error) {
	if d.Type != models.DestinationTypeEmail {
		return nil, fmt.Errorf("destination %d is not an email destination", d.ID)
	}
	out := make([]Recipient, 0, len(d.Config.UserIDs)+len(d.Config.Addresses))
	for _, id := range d.Config.UserIDs {
		userID := id
		recipient := Recipient{UserID: &userID}
		user, err := db.GetUser(ctx, id)
		switch {
		case err == nil:
			recipient.Address = strings.TrimSpace(user.Email)
		case !models.IsNotFound(err):
			return nil, fmt.Errorf("failed to resolve recipient user %d: %w", id, err)
		}
		out = append(out, recipient)
	}
	for _, address := range d.Config.Addresses {
		out = append(out, Recipient{Address: address})
	}
	return out, nil
}
