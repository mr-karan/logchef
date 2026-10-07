package server

import (
	"crypto/subtle"
	"regexp"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/pkg/models"
)

const publicDemoReadOnlyMessage = "This public demo is read-only. Explore dashboards and logs freely; self-host LogChef to create or change resources."

// #nosec G101 -- HTTP header name, not a credential value
const demoProvisioningTokenHeader = "X-Logchef-Demo-Provisioning-Token"

var publicDemoQueryPath = regexp.MustCompile(`^/api/v1/teams/[^/]+/sources/[^/]+/(?:logs/(?:query|histogram|context)|logs/query/[^/]+/cancel|generate-sql|logchefql/(?:query|translate|validate))$`)

// OAuth consent and Connected apps revocation change only the visitor's own
// grants, so a read-only demo can still connect an MCP host. Both routes are
// session-only and check the browser Origin.
var (
	publicDemoOAuthDecisionPath = regexp.MustCompile(`^/api/v1/oauth/requests/[^/]+/decision$`)
	publicDemoConnectedAppPath  = regexp.MustCompile(`^/api/v1/me/connected-apps/[^/]+$`)
)

// enforceDemoReadOnly rejects metadata mutations when the instance advertises
// public demo mode. Query endpoints remain POST because their bodies contain
// queries, but they do not mutate Logchef metadata or the underlying log
// stores. Authentication login/logout, the OAuth consent decision and
// Connected apps revocation must also remain available.
func (s *Server) enforceDemoReadOnly(c fiber.Ctx) error {
	if s.config == nil || !s.config.Demo.ReadOnly {
		return c.Next()
	}

	// A demo's private bootstrap job must be able to converge seeded resources
	// after upgrades. This token is separate from user authentication and must
	// only be shared with that trusted internal job; public reverse proxies
	// should never inject it.
	configuredToken := s.config.Demo.ProvisioningToken
	providedToken := c.Get(demoProvisioningTokenHeader)
	if configuredToken != "" && providedToken != "" && subtle.ConstantTimeCompare([]byte(configuredToken), []byte(providedToken)) == 1 {
		return c.Next()
	}

	switch c.Method() {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return c.Next()
	case fiber.MethodPost:
		path := c.Path()
		if path == "/api/v1/auth/local/login" || path == "/api/v1/auth/logout" ||
			publicDemoQueryPath.MatchString(path) || publicDemoOAuthDecisionPath.MatchString(path) {
			return c.Next()
		}
	case fiber.MethodDelete:
		if publicDemoConnectedAppPath.MatchString(c.Path()) {
			return c.Next()
		}
	}

	return SendErrorWithType(c, fiber.StatusForbidden, publicDemoReadOnlyMessage, models.DemoInstanceErrorType)
}
