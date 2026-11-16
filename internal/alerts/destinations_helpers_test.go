package alerts

import (
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// testWebhooks gives each URL its own destination id, from 1.
func testWebhooks(urls ...string) []WebhookTarget {
	out := make([]WebhookTarget, len(urls))
	for i, url := range urls {
		out[i] = WebhookTarget{Target: webhookTarget(models.DestinationID(i + 1)), URL: url}
	}
	return out
}

// testEmails gives each address its own destination id, from 1.
func testEmails(addresses ...string) []EmailTarget {
	out := make([]EmailTarget, len(addresses))
	for i, address := range addresses {
		out[i] = EmailTarget{Target: emailAddressTarget(models.DestinationID(i+1), address), Address: address}
	}
	return out
}

// webhookDestination stores an active version 1 webhook destination.
func webhookDestination(t *testing.T, db store.Store, url string) models.DestinationID {
	t.Helper()
	d := &models.NotificationDestination{Name: "hook", Type: models.DestinationTypeWebhook, Scope: models.DestinationScopeGlobal,
		Config: models.DestinationConfig{PayloadVersion: 1}, Secret: url, IsActive: true}
	if err := db.CreateNotificationDestination(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	return d.ID
}

// linkDestinations replaces the rule's destinations.
func linkDestinations(t *testing.T, db store.Store, alert *models.Alert, ids ...models.DestinationID) {
	t.Helper()
	if err := db.SetAlertDestinations(t.Context(), alert.ID, ids); err != nil {
		t.Fatal(err)
	}
}

// linkWebhooks gives the rule one new webhook destination per URL.
func linkWebhooks(t *testing.T, db store.Store, alert *models.Alert, urls ...string) []models.DestinationID {
	t.Helper()
	ids := make([]models.DestinationID, len(urls))
	for i, url := range urls {
		ids[i] = webhookDestination(t, db, url)
	}
	linkDestinations(t, db, alert, ids...)
	return ids
}
