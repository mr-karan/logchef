package oauth

import (
	"errors"
	"slices"

	"github.com/mr-karan/logchef/pkg/models"
)

// ReadScopes are the only resource scopes an OAuth client can obtain, in the
// order the consent screen shows them. OAuth never grants write, admin or
// token scopes.
var ReadScopes = []models.TokenScope{
	models.TokenScopeProfileRead,
	models.TokenScopeTeamsRead,
	models.TokenScopeSourcesRead,
	models.TokenScopeLogsRead,
	models.TokenScopeSavedQueriesRead,
	models.TokenScopeCollectionsRead,
	models.TokenScopeAlertsRead,
}

// scopeOfflineAccess requests a refresh token. It is not a resource scope.
const scopeOfflineAccess = "offline_access"

var scopeDescriptions = map[models.TokenScope]string{
	models.TokenScopeProfileRead:      "See your name and email address",
	models.TokenScopeTeamsRead:        "See the teams you belong to",
	models.TokenScopeSourcesRead:      "See the log sources you can access and their schemas",
	models.TokenScopeLogsRead:         "Search and read logs in sources you can access",
	models.TokenScopeSavedQueriesRead: "Read saved queries in sources you can access",
	models.TokenScopeCollectionsRead:  "Read your collections",
	models.TokenScopeAlertsRead:       "Read alerts and alert history in sources you can access",
}

var errInvalidScope = errors.New("scope must name only Logchef read scopes and offline_access")

// parseScopes validates the requested scopes. Every value must be a read
// scope or offline_access, and at least one read scope is required. The
// result is deduplicated and in ReadScopes order.
func parseScopes(requested []string) ([]models.TokenScope, bool, error) {
	var offline bool
	wanted := make(map[models.TokenScope]bool, len(requested))
	for _, raw := range requested {
		if raw == scopeOfflineAccess {
			offline = true
			continue
		}
		scope := models.TokenScope(raw)
		if !slices.Contains(ReadScopes, scope) {
			return nil, false, errInvalidScope
		}
		wanted[scope] = true
	}
	var scopes []models.TokenScope
	for _, scope := range ReadScopes {
		if wanted[scope] {
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) == 0 {
		return nil, false, errInvalidScope
	}
	return scopes, offline, nil
}

// scopeStrings is the inverse of parseScopes, for the ZITADEL request types.
func scopeStrings(scopes []models.TokenScope, offline bool) []string {
	out := make([]string, 0, len(scopes)+1)
	for _, scope := range scopes {
		out = append(out, string(scope))
	}
	if offline {
		out = append(out, scopeOfflineAccess)
	}
	return out
}

// scopeMask encodes a set of read scopes as a bit mask over ReadScopes. It
// keeps the access-token ID free of ':' characters, which ZITADEL uses to
// separate the token ID from the subject.
func scopeMask(scopes []string) uint {
	var mask uint
	for i, scope := range ReadScopes {
		if slices.Contains(scopes, string(scope)) {
			mask |= 1 << i
		}
	}
	return mask
}

func scopesFromMask(mask uint) []models.TokenScope {
	var scopes []models.TokenScope
	for i, scope := range ReadScopes {
		if mask&(1<<i) != 0 {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}
