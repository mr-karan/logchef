package storetest

import (
	"context"
	"net/url"
	"slices"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// AlertDestinationsUpgradeSeed is portable SQL that seeds rules with targets
// at the schema version before add_notification_destinations (RFC 0006 unit
// D1): two rules of one creator share a webhook URL, a rule has recipients,
// a rule without a creator uses the same URL, and two rules hold null, [] and
// the JSON null.
var AlertDestinationsUpgradeSeed = []string{
	`INSERT INTO users (id, email, full_name, role, status) VALUES
		(1, 'owner@test.dev', 'Owner', 'member', 'active'),
		(2, 'second@test.dev', 'Second', 'member', 'active')`,
	`INSERT INTO sources (id, name, _meta_is_auto_created, _meta_ts_field, source_type, connection_config, identity_key)
		VALUES (1, 'logs', FALSE, 'timestamp', 'clickhouse', '{}', 'clickhouse:ch:9000/logs/app')`,
	`INSERT INTO alerts (id, source_id, name, query_language, editor_mode, query, lookback_seconds, threshold_operator,
			threshold_value, frequency_seconds, severity, is_active, last_state, recipient_user_ids_json, webhook_urls_json, created_by)
		VALUES
		(1, 1, 'two hooks', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 60, 'warning', TRUE, 'resolved',
			NULL, '["https://user:pass@hooks.example.com:8443/a?token=x","https://ci.example.org/b"]', 1),
		(2, 1, 'shared hook and mail', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 60, 'warning', TRUE, 'resolved',
			'[2,1,2]', '["https://user:pass@hooks.example.com:8443/a?token=x"]', 1),
		(3, 1, 'no creator', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 60, 'warning', TRUE, 'resolved',
			'[2]', '["https://user:pass@hooks.example.com:8443/a?token=x"]', NULL),
		(4, 1, 'empty arrays', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 60, 'warning', TRUE, 'resolved', '[]', '[]', 1),
		(5, 1, 'json null', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 60, 'warning', TRUE, 'resolved', 'null', NULL, 1),
		(6, 1, 'query before path', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 60, 'warning', TRUE, 'resolved',
			NULL, '["https://hooks.example.net?token=SECRET/path"]', 2),
		(7, 1, 'userinfo with delimiters', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 60, 'warning', TRUE, 'resolved',
			NULL, '["https://user:pa@ss@hooks.example.org/path","https://user:pa/ss@hooks.example.io/path","https://user:pa?ss@hooks.example.dev/","https://[2001:db8::1]:8443/hook"]', 1)`,
}

// CheckAlertDestinationsUpgrade asserts the destinations and links that the
// migration made from AlertDestinationsUpgradeSeed.
func CheckAlertDestinationsUpgrade(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	// The seed's shared URL, with credentials that the name must leave out.
	sharedURL := (&url.URL{Scheme: "https", User: url.UserPassword("user", "pass"), Host: "hooks.example.com:8443", Path: "/a", RawQuery: "token=x"}).String()
	all, err := s.ListNotificationDestinations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 10 {
		t.Fatalf("destinations = %d, want 8 webhooks and 2 email destinations: %+v", len(all), all)
	}
	checkMigratedWebhookNames(t, all)
	find := func(typ models.DestinationType, match func(*models.NotificationDestination) bool) *models.NotificationDestination {
		t.Helper()
		for _, d := range all {
			if d.Type == typ && match(d) {
				return d
			}
		}
		t.Fatalf("no %s destination matches", typ)
		return nil
	}
	creator := func(id models.UserID) func(*models.NotificationDestination) bool {
		return func(d *models.NotificationDestination) bool { return d.CreatedBy != nil && *d.CreatedBy == id }
	}
	sharedOwned := find(models.DestinationTypeWebhook, func(d *models.NotificationDestination) bool { return creator(1)(d) && d.Secret == sharedURL })
	second := find(models.DestinationTypeWebhook, func(d *models.NotificationDestination) bool { return d.Secret == "https://ci.example.org/b" })
	sharedOrphan := find(models.DestinationTypeWebhook, func(d *models.NotificationDestination) bool { return d.CreatedBy == nil })
	for _, d := range []*models.NotificationDestination{sharedOwned, second, sharedOrphan} {
		if d.Scope != models.DestinationScopePrivate || d.Config.PayloadVersion != 1 || !d.IsActive || d.TeamID != nil {
			t.Errorf("webhook destination %+v, want private, version 1, active", d)
		}
	}
	if sharedOwned.Name != "Webhook hooks.example.com" || second.Name != "Webhook ci.example.org" || sharedOrphan.Secret != sharedURL {
		t.Errorf("webhook names %q, %q; orphan secret %q", sharedOwned.Name, second.Name, sharedOrphan.Secret)
	}
	// The query comes before any path, and the name stops at the host.
	if queryFirst := find(models.DestinationTypeWebhook, creator(2)); queryFirst.Name != "Webhook hooks.example.net" {
		t.Errorf("name of a URL whose query precedes its path = %q, want the host only", queryFirst.Name)
	}

	mail2, mail3 := checkMigratedEmail(t, find, creator)

	for alertID, want := range map[models.AlertID][]models.DestinationID{
		1: sortedDestinationIDs(sharedOwned.ID, second.ID),
		2: sortedDestinationIDs(sharedOwned.ID, mail2.ID),
		3: sortedDestinationIDs(sharedOrphan.ID, mail3.ID),
		4: {},
		5: {},
		6: {find(models.DestinationTypeWebhook, creator(2)).ID},
		7: webhookIDs(all, "https://user:pa@ss@hooks.example.org/path", "https://user:pa/ss@hooks.example.io/path",
			"https://user:pa?ss@hooks.example.dev/", "https://[2001:db8::1]:8443/hook"),
	} {
		alert, err := s.GetAlert(ctx, alertID)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(alert.DestinationIDs, want) {
			t.Errorf("rule %d destinations = %v, want %v", alertID, alert.DestinationIDs, want)
		}
	}
	checkDestinationIDsAfterUpgrade(t, s, all)
}

// checkMigratedWebhookNames: a name carries the URL's host and never a byte
// of its userinfo. The userinfo ends at the last '@' of the authority. When a
// '/' or '?' inside the userinfo makes the authority unclear, the name has no
// host at all.
func checkMigratedWebhookNames(t *testing.T, all []*models.NotificationDestination) {
	t.Helper()
	want := map[string]string{
		"https://user:pa@ss@hooks.example.org/path": "Webhook hooks.example.org",
		"https://user:pa/ss@hooks.example.io/path":  "Webhook",
		"https://user:pa?ss@hooks.example.dev/":     "Webhook",
		"https://[2001:db8::1]:8443/hook":           "Webhook [2001:db8::1]",
	}
	for _, d := range all {
		name, ok := want[d.Secret]
		if !ok {
			continue
		}
		delete(want, d.Secret)
		if d.Name != name {
			t.Errorf("name of %s = %q, want %q", d.Secret, d.Name, name)
		}
	}
	for url := range want {
		t.Errorf("no destination migrated from %s", url)
	}
}

// webhookIDs returns the sorted ids of the webhook destinations with these URLs.
func webhookIDs(all []*models.NotificationDestination, urls ...string) []models.DestinationID {
	var ids []models.DestinationID
	for _, d := range all {
		if d.Type == models.DestinationTypeWebhook && slices.Contains(urls, d.Secret) {
			ids = append(ids, d.ID)
		}
	}
	return sortedDestinationIDs(ids...)
}

type destinationFinder func(models.DestinationType, func(*models.NotificationDestination) bool) *models.NotificationDestination

func checkMigratedEmail(t *testing.T, find destinationFinder, creator func(models.UserID) func(*models.NotificationDestination) bool) (mail2, mail3 *models.NotificationDestination) {
	t.Helper()
	mail2 = find(models.DestinationTypeEmail, func(d *models.NotificationDestination) bool { return d.Name == "Email for shared hook and mail" })
	mail3 = find(models.DestinationTypeEmail, func(d *models.NotificationDestination) bool { return d.Name == "Email for no creator" })
	if !slices.Equal(mail2.Config.UserIDs, []models.UserID{1, 2}) || !creator(1)(mail2) || mail2.Scope != models.DestinationScopePrivate || mail2.HasSecret {
		t.Errorf("email destination of rule 2 = %+v, want users 1 and 2 owned by user 1", mail2)
	}
	if !slices.Equal(mail3.Config.UserIDs, []models.UserID{2}) || mail3.CreatedBy != nil {
		t.Errorf("email destination of rule 3 = %+v, want user 2 and no creator", mail3)
	}

	return mail2, mail3
}

// checkDestinationIDsAfterUpgrade: a destination created after the upgrade
// takes an id above the moved ones.
func checkDestinationIDsAfterUpgrade(t *testing.T, s store.Store, migrated []*models.NotificationDestination) {
	t.Helper()
	ctx := context.Background()
	freshURL := "https://new.example.com/"
	fresh := &models.NotificationDestination{Name: "after upgrade", Type: models.DestinationTypeWebhook, Scope: models.DestinationScopeGlobal,
		Config: models.DestinationConfig{PayloadVersion: 1}, Secret: freshURL, IsActive: true}
	if err := s.CreateNotificationDestination(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	for _, d := range migrated {
		if fresh.ID <= d.ID {
			t.Errorf("new destination id %d is not above migrated id %d", fresh.ID, d.ID)
		}
	}
}
