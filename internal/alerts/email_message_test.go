package alerts

import (
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

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
	if body := emailBody(notification); !strings.Contains(body, "Group: service=checkout") {
		t.Errorf("body = %q, want a Group line", body)
	}
}
