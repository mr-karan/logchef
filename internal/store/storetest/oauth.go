package storetest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// Opener opens a new, independent store on the same database each time it is
// called. RunOAuth uses it for concurrency tests (one connection per writer)
// and for restart persistence. The opener registers its own Close cleanup.
type Opener func(t *testing.T) store.Store

// RunOAuth exercises the OAuthStore contract (T-ST-1..7). The database must
// already be migrated; RunOAuth creates its own users and rows.
func RunOAuth(t *testing.T, open Opener) {
	ctx := context.Background()
	o := &oauthFixture{key: models.OAuthHashKey{0x6c, 0x6f, 0x67, 0x63, 0x68, 0x65, 0x66}, base: time.Now().UTC().Truncate(time.Millisecond)}

	t.Run("CodeConsumedOnceConcurrently", func(t *testing.T) { o.testCodeConcurrent(t, ctx, open) })
	t.Run("CodeReplayAndExpiry", func(t *testing.T) { o.testCodeReplay(t, ctx, open) })
	t.Run("AuthRequestApproval", func(t *testing.T) { o.testAuthRequestApproval(t, ctx, open) })
	t.Run("AuthCodeAttachment", func(t *testing.T) { o.testAuthCodeAttachment(t, ctx, open) })
	t.Run("AuthRequestDenialAndExpiry", func(t *testing.T) { o.testAuthRequestDenial(t, ctx, open) })
	t.Run("RefreshRotatedOnceConcurrently", func(t *testing.T) { o.testRefreshConcurrent(t, ctx, open) })
	t.Run("RefreshReplayRevocationSurvives", func(t *testing.T) { o.testRefreshReplay(t, ctx, open) })
	t.Run("RefreshCheckDetectsReplay", func(t *testing.T) { o.testRefreshCheckReplay(t, ctx, open) })
	t.Run("RefreshExpiredAndUnknown", func(t *testing.T) { o.testRefreshInvalid(t, ctx, open) })
	t.Run("AccessTokenAuthentication", func(t *testing.T) { o.testAccessToken(t, ctx, open) })
	t.Run("GrantRevocation", func(t *testing.T) { o.testRevocation(t, ctx, open) })
	t.Run("CleanupKeepsLiveRefreshFamilies", func(t *testing.T) { o.testCleanup(t, ctx, open) })
	t.Run("RestartPersistence", func(t *testing.T) { o.testRestart(t, ctx, open) })
	t.Run("ReplayHandlingRejectedInsideTx", func(t *testing.T) { o.testRootOnly(t, ctx, open) })
}

const (
	mcpResource = "https://logchef.test/mcp"
	apiResource = "https://logchef.test/api"
	webClient   = models.OAuthClientID("chatgpt")
	cliClient   = models.OAuthClientID("logchef-cli")
	raceRounds  = 20
)

type oauthFixture struct {
	key  models.OAuthHashKey
	base time.Time

	mu  sync.Mutex
	seq int
}

// name returns a value unique within this run, for request IDs, codes,
// tokens and emails.
func (o *oauthFixture) name(prefix string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	return fmt.Sprintf("%s-%d", prefix, o.seq)
}

func (o *oauthFixture) hash(secret string) models.OAuthSecretHash {
	return models.HashOAuthSecret(o.key, secret)
}

func (o *oauthFixture) user(t *testing.T, ctx context.Context, s store.StoreOps) *models.User {
	t.Helper()
	return mkUser(t, ctx, s, o.name("oauth-user")+"@test.dev")
}

func (o *oauthFixture) authRequest(t *testing.T, ctx context.Context, s store.StoreOps, expiresAt time.Time) models.OAuthAuthRequestID {
	t.Helper()
	req := &models.OAuthAuthRequest{
		ID:            models.OAuthAuthRequestID(o.name("req")),
		ClientID:      webClient,
		RedirectURI:   "https://chatgpt.com/connector_platform_oauth_redirect",
		Resource:      mcpResource,
		Scopes:        []models.TokenScope{models.TokenScopeLogsRead, models.TokenScopeSourcesRead},
		OfflineAccess: true,
		CodeChallenge: "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		State:         "state-value",
		ExpiresAt:     expiresAt,
		CreatedAt:     o.base,
	}
	if err := s.CreateOAuthAuthRequest(ctx, req); err != nil {
		t.Fatalf("CreateOAuthAuthRequest: %v", err)
	}
	return req.ID
}

// approvedCode creates a request, approves it for user and attaches a code
// that expires at codeExpiresAt.
func (o *oauthFixture) approvedCode(t *testing.T, ctx context.Context, s store.StoreOps, user *models.User, codeExpiresAt time.Time) (*models.OAuthGrant, models.OAuthSecretHash) {
	t.Helper()
	id := o.authRequest(t, ctx, s, o.base.Add(10*time.Minute))
	grant, err := s.ApproveOAuthAuthRequest(ctx, id, user.ID, o.base)
	if err != nil {
		t.Fatalf("ApproveOAuthAuthRequest: %v", err)
	}
	code := o.hash(o.name("code"))
	if err := s.SaveOAuthAuthCode(ctx, id, code, codeExpiresAt); err != nil {
		t.Fatalf("SaveOAuthAuthCode: %v", err)
	}
	return grant, code
}

// tokens stores an access token and a refresh token for grant and returns
// their hashes.
func (o *oauthFixture) tokens(t *testing.T, ctx context.Context, s store.StoreOps, grant models.OAuthGrantID, refreshExpiresAt time.Time) (access, refresh models.OAuthSecretHash) {
	t.Helper()
	access = o.hash(o.name("access"))
	refresh = o.hash(o.name("refresh"))
	if err := s.IssueOAuthTokens(ctx, grant, models.OAuthTokenIssue{
		AccessIDHash:     access,
		AccessExpiresAt:  o.base.Add(10 * time.Minute),
		RefreshHash:      &refresh,
		RefreshExpiresAt: refreshExpiresAt,
	}, o.base); err != nil {
		t.Fatalf("IssueOAuthTokens: %v", err)
	}
	return access, refresh
}

func (o *oauthFixture) rotation(refreshExpiresAt time.Time) models.OAuthTokenIssue {
	refresh := o.hash(o.name("refresh"))
	return models.OAuthTokenIssue{
		AccessIDHash:     o.hash(o.name("access")),
		AccessExpiresAt:  o.base.Add(10 * time.Minute),
		RefreshHash:      &refresh,
		RefreshExpiresAt: refreshExpiresAt,
	}
}

// race runs a and b at the same moment on separate goroutines.
func race(a, b func()) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); <-start; a() }()
	go func() { defer wg.Done(); <-start; b() }()
	close(start)
	wg.Wait()
}

func grantActive(t *testing.T, ctx context.Context, s store.StoreOps, user models.UserID, grant models.OAuthGrantID) bool {
	t.Helper()
	grants, err := s.ListGrantsForUser(ctx, user)
	if err != nil {
		t.Fatalf("ListGrantsForUser: %v", err)
	}
	for _, g := range grants {
		if g.ID == grant {
			return true
		}
	}
	return false
}

// T-ST-1: two goroutines on two independent connections; exactly one wins.
func (o *oauthFixture) testCodeConcurrent(t *testing.T, ctx context.Context, open Opener) {
	s1, s2 := open(t), open(t)
	user := o.user(t, ctx, s1)
	for round := range raceRounds {
		_, code := o.approvedCode(t, ctx, s1, user, o.base.Add(2*time.Minute))
		var err1, err2 error
		race(
			func() { _, err1 = s1.ConsumeAuthCode(ctx, code, o.base) },
			func() { _, err2 = s2.ConsumeAuthCode(ctx, code, o.base) },
		)
		if (err1 == nil) == (err2 == nil) {
			t.Fatalf("round %d: want exactly one success, got %v / %v", round, err1, err2)
		}
		loser := err1
		if loser == nil {
			loser = err2
		}
		if !errors.Is(loser, models.ErrOAuthInvalidGrant) {
			t.Fatalf("round %d: loser err = %v, want ErrOAuthInvalidGrant", round, loser)
		}
	}
}

func (o *oauthFixture) testCodeReplay(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)

	grant, code := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	got, err := s.ConsumeAuthCode(ctx, code, o.base)
	if err != nil {
		t.Fatalf("first ConsumeAuthCode: %v", err)
	}
	if got.GrantID == nil || *got.GrantID != grant.ID || got.Resource != mcpResource || got.UserID == nil || *got.UserID != user.ID {
		t.Fatalf("consumed request = %+v, want grant %d, resource %s, user %d", got, grant.ID, mcpResource, user.ID)
	}
	if _, err := s.ConsumeAuthCode(ctx, code, o.base); !errors.Is(err, models.ErrOAuthCodeReplay) {
		t.Fatalf("replayed code err = %v, want ErrOAuthCodeReplay", err)
	}
	if grantActive(t, ctx, open(t), user.ID, grant.ID) {
		t.Fatalf("grant %d still active after code replay", grant.ID)
	}

	// An expired code is invalid but is not a replay: the grant stays.
	expiredGrant, expired := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	if _, err := s.ConsumeAuthCode(ctx, expired, o.base.Add(2*time.Minute)); !errors.Is(err, models.ErrOAuthInvalidGrant) || errors.Is(err, models.ErrOAuthCodeReplay) {
		t.Fatalf("expired code err = %v, want ErrOAuthInvalidGrant without replay", err)
	}
	if !grantActive(t, ctx, s, user.ID, expiredGrant.ID) {
		t.Fatalf("expired code revoked grant %d", expiredGrant.ID)
	}

	if _, err := s.ConsumeAuthCode(ctx, o.hash("never-issued"), o.base); !errors.Is(err, models.ErrOAuthInvalidGrant) {
		t.Fatalf("unknown code err = %v, want ErrOAuthInvalidGrant", err)
	}

	// A code under a revoked grant cannot be consumed.
	revoked, revokedCode := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	if err := s.RevokeGrant(ctx, revoked.ID, user.ID, o.base); err != nil {
		t.Fatalf("RevokeGrant: %v", err)
	}
	if _, err := s.ConsumeAuthCode(ctx, revokedCode, o.base); !errors.Is(err, models.ErrOAuthInvalidGrant) {
		t.Fatalf("code under revoked grant err = %v, want ErrOAuthInvalidGrant", err)
	}
}

func (o *oauthFixture) testAuthRequestApproval(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)
	other := o.user(t, ctx, s)

	id := o.authRequest(t, ctx, s, o.base.Add(10*time.Minute))
	pending, err := s.GetOAuthAuthRequest(ctx, id)
	if err != nil || pending.DecidedAt != nil || pending.UserID != nil || len(pending.Scopes) != 2 || !pending.OfflineAccess {
		t.Fatalf("pending request = %+v / %v", pending, err)
	}
	grant, err := s.ApproveOAuthAuthRequest(ctx, id, user.ID, o.base)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if grant.UserID != user.ID || grant.ClientID != webClient || grant.Resource != mcpResource || len(grant.Scopes) != 2 || !grant.OfflineAccess {
		t.Fatalf("grant = %+v", grant)
	}
	// The first decision binds the user; a second one is rejected.
	if _, err := s.ApproveOAuthAuthRequest(ctx, id, other.ID, o.base); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("second approve err = %v, want ErrNotFound", err)
	}
	if err := s.DenyOAuthAuthRequest(ctx, id, other.ID, o.base); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("deny after approve err = %v, want ErrNotFound", err)
	}
	decided, err := s.GetOAuthAuthRequest(ctx, id)
	if err != nil || decided.UserID == nil || *decided.UserID != user.ID || decided.GrantID == nil || *decided.GrantID != grant.ID || decided.Denied {
		t.Fatalf("decided request = %+v / %v", decided, err)
	}
}

// A code attaches only to an approved request, once.
func (o *oauthFixture) testAuthCodeAttachment(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)
	id := o.authRequest(t, ctx, s, o.base.Add(10*time.Minute))
	if err := s.SaveOAuthAuthCode(ctx, id, o.hash(o.name("code")), o.base.Add(time.Minute)); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("code on undecided request err = %v, want ErrNotFound", err)
	}
	if _, err := s.ApproveOAuthAuthRequest(ctx, id, user.ID, o.base); err != nil {
		t.Fatalf("approve: %v", err)
	}
	code := o.hash(o.name("code"))
	if err := s.SaveOAuthAuthCode(ctx, id, code, o.base.Add(time.Minute)); err != nil {
		t.Fatalf("SaveOAuthAuthCode: %v", err)
	}
	if err := s.SaveOAuthAuthCode(ctx, id, o.hash(o.name("code")), o.base.Add(time.Minute)); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("second code err = %v, want ErrNotFound", err)
	}
}

func (o *oauthFixture) testAuthRequestDenial(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)

	denied := o.authRequest(t, ctx, s, o.base.Add(10*time.Minute))
	if err := s.DenyOAuthAuthRequest(ctx, denied, user.ID, o.base); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if err := s.SaveOAuthAuthCode(ctx, denied, o.hash(o.name("code")), o.base.Add(time.Minute)); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("code on denied request err = %v, want ErrNotFound", err)
	}

	expired := o.authRequest(t, ctx, s, o.base.Add(time.Minute))
	if _, err := s.ApproveOAuthAuthRequest(ctx, expired, user.ID, o.base.Add(time.Minute)); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("approve expired err = %v, want ErrNotFound", err)
	}
	if _, err := s.GetOAuthAuthRequest(ctx, "missing"); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("missing request err = %v, want ErrNotFound", err)
	}
}

// T-ST-2: concurrent rotation of one refresh token; exactly one wins and the
// other is a replay.
func (o *oauthFixture) testRefreshConcurrent(t *testing.T, ctx context.Context, open Opener) {
	s1, s2 := open(t), open(t)
	user := o.user(t, ctx, s1)
	for round := range raceRounds {
		grant, _ := o.approvedCode(t, ctx, s1, user, o.base.Add(2*time.Minute))
		_, refresh := o.tokens(t, ctx, s1, grant.ID, o.base.Add(time.Hour))
		var out1, out2 models.RefreshOutcome
		var err1, err2 error
		race(
			func() { out1, err1 = s1.RotateRefreshToken(ctx, refresh, o.rotation(o.base.Add(time.Hour)), o.base) },
			func() { out2, err2 = s2.RotateRefreshToken(ctx, refresh, o.rotation(o.base.Add(time.Hour)), o.base) },
		)
		if err1 != nil || err2 != nil {
			t.Fatalf("round %d: errors %v / %v", round, err1, err2)
		}
		kinds := map[models.RefreshOutcomeKind]int{out1.Kind: 1}
		kinds[out2.Kind]++
		if kinds[models.RefreshOK] != 1 || kinds[models.RefreshReplay] != 1 {
			t.Fatalf("round %d: outcomes %v / %v, want one OK and one replay", round, out1.Kind, out2.Kind)
		}
	}
}

// T-ST-3: a replay revokes the grant, and the revocation is visible on a new
// connection after the call returns.
func (o *oauthFixture) testRefreshReplay(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)
	grant, _ := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	_, r0 := o.tokens(t, ctx, s, grant.ID, o.base.Add(time.Hour))

	if out, err := s.CheckRefreshToken(ctx, r0, o.base); err != nil || out.Kind != models.RefreshOK || out.Grant.ID != grant.ID {
		t.Fatalf("CheckRefreshToken(r0) = %+v / %v", out, err)
	}
	next := o.rotation(o.base.Add(2 * time.Hour))
	out, err := s.RotateRefreshToken(ctx, r0, next, o.base)
	if err != nil || out.Kind != models.RefreshOK || out.Grant.ID != grant.ID || out.Grant.UserID != user.ID {
		t.Fatalf("rotate r0 = %+v / %v", out, err)
	}
	if _, _, err := s.AuthenticateOAuthAccessToken(ctx, next.AccessIDHash, mcpResource, o.base); err != nil {
		t.Fatalf("rotated access token rejected: %v", err)
	}
	out, err = s.RotateRefreshToken(ctx, r0, o.rotation(o.base.Add(2*time.Hour)), o.base)
	if err != nil || out.Kind != models.RefreshReplay {
		t.Fatalf("replayed r0 = %+v / %v, want RefreshReplay", out, err)
	}

	fresh := open(t)
	if grantActive(t, ctx, fresh, user.ID, grant.ID) {
		t.Fatalf("grant %d active on a new connection after replay", grant.ID)
	}
	if _, _, err := fresh.AuthenticateOAuthAccessToken(ctx, next.AccessIDHash, mcpResource, o.base); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("access token after replay err = %v, want ErrNotFound", err)
	}
	if out, err := fresh.RotateRefreshToken(ctx, *next.RefreshHash, o.rotation(o.base.Add(3*time.Hour)), o.base); err != nil || out.Kind != models.RefreshInvalid {
		t.Fatalf("rotate successor after replay = %+v / %v, want RefreshInvalid", out, err)
	}
}

// CheckRefreshToken detects a replay too, and its revocation persists.
func (o *oauthFixture) testRefreshCheckReplay(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)
	g2, _ := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	_, r2 := o.tokens(t, ctx, s, g2.ID, o.base.Add(time.Hour))
	if out, err := s.RotateRefreshToken(ctx, r2, o.rotation(o.base.Add(time.Hour)), o.base); err != nil || out.Kind != models.RefreshOK {
		t.Fatalf("rotate r2 = %+v / %v", out, err)
	}
	if out, err := s.CheckRefreshToken(ctx, r2, o.base); err != nil || out.Kind != models.RefreshReplay {
		t.Fatalf("CheckRefreshToken(consumed) = %+v / %v, want RefreshReplay", out, err)
	}
	if grantActive(t, ctx, open(t), user.ID, g2.ID) {
		t.Fatalf("grant %d active after CheckRefreshToken replay", g2.ID)
	}
}

// Expired and unknown tokens are invalid, not replays.
func (o *oauthFixture) testRefreshInvalid(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)
	g3, _ := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	_, r3 := o.tokens(t, ctx, s, g3.ID, o.base.Add(time.Hour))
	if out, err := s.RotateRefreshToken(ctx, r3, o.rotation(o.base.Add(2*time.Hour)), o.base.Add(time.Hour)); err != nil || out.Kind != models.RefreshInvalid {
		t.Fatalf("rotate expired = %+v / %v, want RefreshInvalid", out, err)
	}
	if !grantActive(t, ctx, s, user.ID, g3.ID) {
		t.Fatalf("expired refresh token revoked grant %d", g3.ID)
	}
	if out, err := s.CheckRefreshToken(ctx, o.hash("never-issued"), o.base); err != nil || out.Kind != models.RefreshInvalid {
		t.Fatalf("check unknown = %+v / %v, want RefreshInvalid", out, err)
	}
	if _, err := s.RotateRefreshToken(ctx, r3, models.OAuthTokenIssue{AccessIDHash: o.hash(o.name("access"))}, o.base); err == nil {
		t.Fatalf("rotation without a new refresh token succeeded")
	}
}

func (o *oauthFixture) testAccessToken(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)
	grant, _ := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	access, _ := o.tokens(t, ctx, s, grant.ID, o.base.Add(time.Hour))

	g, u, err := s.AuthenticateOAuthAccessToken(ctx, access, mcpResource, o.base)
	if err != nil || g.ID != grant.ID || u.ID != user.ID || u.Email != user.Email {
		t.Fatalf("authenticate = %+v / %+v / %v", g, u, err)
	}
	if err := s.TouchOAuthGrant(ctx, grant.ID, o.base.Add(time.Minute)); err != nil {
		t.Fatalf("TouchOAuthGrant: %v", err)
	}
	if g, _, err := s.AuthenticateOAuthAccessToken(ctx, access, mcpResource, o.base); err != nil || g.LastUsedAt == nil || !g.LastUsedAt.Equal(o.base.Add(time.Minute)) {
		t.Fatalf("last_used_at = %v / %v", g, err)
	}

	for name, check := range map[string]struct {
		hash     models.OAuthSecretHash
		resource string
		now      time.Time
	}{
		"wrong audience": {access, apiResource, o.base},
		"expired":        {access, mcpResource, o.base.Add(10 * time.Minute)},
		"unknown":        {o.hash("never-issued"), mcpResource, o.base},
	} {
		if _, _, err := s.AuthenticateOAuthAccessToken(ctx, check.hash, check.resource, check.now); !errors.Is(err, models.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}

	user.Status = models.UserStatusInactive
	if err := s.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, _, err := s.AuthenticateOAuthAccessToken(ctx, access, mcpResource, o.base); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("inactive user err = %v, want ErrNotFound", err)
	}

	svc := &models.User{Email: o.name("oauth-svc") + "@test.dev", FullName: "svc", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: models.UserAccountTypeService}
	if err := s.CreateUser(ctx, svc); err != nil {
		t.Fatalf("CreateUser(service): %v", err)
	}
	svcGrant, _ := o.approvedCode(t, ctx, s, svc, o.base.Add(2*time.Minute))
	svcAccess, _ := o.tokens(t, ctx, s, svcGrant.ID, o.base.Add(time.Hour))
	if _, _, err := s.AuthenticateOAuthAccessToken(ctx, svcAccess, mcpResource, o.base); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("service account err = %v, want ErrNotFound", err)
	}
}

func (o *oauthFixture) testRevocation(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)
	other := o.user(t, ctx, s)

	grant, _ := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	access, refresh := o.tokens(t, ctx, s, grant.ID, o.base.Add(time.Hour))
	if err := s.RevokeGrant(ctx, grant.ID, other.ID, o.base); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("revoke by other user err = %v, want ErrNotFound", err)
	}
	if err := s.RevokeGrant(ctx, grant.ID, user.ID, o.base); err != nil {
		t.Fatalf("RevokeGrant: %v", err)
	}
	if err := s.RevokeGrant(ctx, grant.ID, user.ID, o.base); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("second revoke err = %v, want ErrNotFound", err)
	}
	if _, _, err := s.AuthenticateOAuthAccessToken(ctx, access, mcpResource, o.base); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("access after revoke err = %v, want ErrNotFound", err)
	}
	if out, err := s.RotateRefreshToken(ctx, refresh, o.rotation(o.base.Add(time.Hour)), o.base); err != nil || out.Kind != models.RefreshInvalid {
		t.Fatalf("refresh after revoke = %+v / %v, want RefreshInvalid", out, err)
	}
	if err := s.IssueOAuthTokens(ctx, grant.ID, o.rotation(o.base.Add(time.Hour)), o.base); !errors.Is(err, models.ErrOAuthInvalidGrant) {
		t.Fatalf("issue under revoked grant err = %v, want ErrOAuthInvalidGrant", err)
	}

	// /oauth/revoke: only the owning client can revoke, by either token.
	for _, byRefresh := range []bool{false, true} {
		g, _ := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
		a, r := o.tokens(t, ctx, s, g.ID, o.base.Add(time.Hour))
		token := a
		if byRefresh {
			token = r
		}
		if err := s.RevokeGrantByToken(ctx, token, cliClient, o.base); err != nil {
			t.Fatalf("RevokeGrantByToken(other client): %v", err)
		}
		if !grantActive(t, ctx, s, user.ID, g.ID) {
			t.Fatalf("other client revoked grant %d", g.ID)
		}
		if err := s.RevokeGrantByToken(ctx, token, webClient, o.base); err != nil {
			t.Fatalf("RevokeGrantByToken: %v", err)
		}
		if grantActive(t, ctx, s, user.ID, g.ID) {
			t.Fatalf("grant %d active after RevokeGrantByToken (refresh=%v)", g.ID, byRefresh)
		}
	}
	if err := s.RevokeGrantByToken(ctx, o.hash("never-issued"), webClient, o.base); err != nil {
		t.Fatalf("RevokeGrantByToken(unknown): %v", err)
	}

	first, _ := o.approvedCode(t, ctx, s, other, o.base.Add(2*time.Minute))
	second, _ := o.approvedCode(t, ctx, s, other, o.base.Add(2*time.Minute))
	grants, err := s.ListGrantsForUser(ctx, other.ID)
	if err != nil || len(grants) != 2 {
		t.Fatalf("ListGrantsForUser = %d / %v, want 2", len(grants), err)
	}
	// Same created_at: ties break by id, newest first.
	if grants[0].ID != second.ID || grants[1].ID != first.ID {
		t.Fatalf("grant order = [%d %d], want [%d %d]", grants[0].ID, grants[1].ID, second.ID, first.ID)
	}
}

// T-ST-6: cleanup removes expired rows but keeps a consumed refresh token
// while its family is live, so its replay is still detected.
func (o *oauthFixture) testCleanup(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)

	grant, _ := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	_, r0 := o.tokens(t, ctx, s, grant.ID, o.base.Add(time.Hour))
	next := o.rotation(o.base.Add(2 * time.Hour))
	if out, err := s.RotateRefreshToken(ctx, r0, next, o.base); err != nil || out.Kind != models.RefreshOK {
		t.Fatalf("rotate = %+v / %v", out, err)
	}
	pendingReq := o.authRequest(t, ctx, s, o.base.Add(10*time.Minute))

	// r0's own expiry has passed but r1 keeps the family live.
	if err := s.DeleteExpiredOAuthRows(ctx, o.base.Add(90*time.Minute)); err != nil {
		t.Fatalf("DeleteExpiredOAuthRows: %v", err)
	}
	if _, err := s.GetOAuthAuthRequest(ctx, pendingReq); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("expired auth request survived cleanup: %v", err)
	}
	if out, err := s.CheckRefreshToken(ctx, r0, o.base.Add(90*time.Minute)); err != nil || out.Kind != models.RefreshReplay {
		t.Fatalf("consumed r0 after cleanup = %+v / %v, want RefreshReplay (row kept)", out, err)
	}

	// Once the whole family has expired, all its rows go.
	g2, _ := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	_, r2 := o.tokens(t, ctx, s, g2.ID, o.base.Add(time.Hour))
	if out, err := s.RotateRefreshToken(ctx, r2, o.rotation(o.base.Add(2*time.Hour)), o.base); err != nil || out.Kind != models.RefreshOK {
		t.Fatalf("rotate r2 = %+v / %v", out, err)
	}
	if err := s.DeleteExpiredOAuthRows(ctx, o.base.Add(3*time.Hour)); err != nil {
		t.Fatalf("DeleteExpiredOAuthRows: %v", err)
	}
	if out, err := s.CheckRefreshToken(ctx, r2, o.base.Add(3*time.Hour)); err != nil || out.Kind != models.RefreshInvalid {
		t.Fatalf("r2 after family expiry = %+v / %v, want RefreshInvalid (row deleted)", out, err)
	}
	if !grantActive(t, ctx, s, user.ID, g2.ID) {
		t.Fatalf("cleanup revoked grant %d", g2.ID)
	}
}

// T-ST-7: rows written on one connection survive closing it.
func (o *oauthFixture) testRestart(t *testing.T, ctx context.Context, open Opener) {
	first := open(t)
	user := o.user(t, ctx, first)
	grant, code := o.approvedCode(t, ctx, first, user, o.base.Add(2*time.Minute))
	access, refresh := o.tokens(t, ctx, first, grant.ID, o.base.Add(time.Hour))
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := open(t)
	if !grantActive(t, ctx, second, user.ID, grant.ID) {
		t.Fatalf("grant %d lost across restart", grant.ID)
	}
	if _, _, err := second.AuthenticateOAuthAccessToken(ctx, access, mcpResource, o.base); err != nil {
		t.Fatalf("access token lost across restart: %v", err)
	}
	if _, err := second.ConsumeAuthCode(ctx, code, o.base); err != nil {
		t.Fatalf("auth code lost across restart: %v", err)
	}
	if out, err := second.RotateRefreshToken(ctx, refresh, o.rotation(o.base.Add(time.Hour)), o.base); err != nil || out.Kind != models.RefreshOK {
		t.Fatalf("refresh token lost across restart = %+v / %v", out, err)
	}
}

func (o *oauthFixture) testRootOnly(t *testing.T, ctx context.Context, open Opener) {
	s := open(t)
	user := o.user(t, ctx, s)
	grant, code := o.approvedCode(t, ctx, s, user, o.base.Add(2*time.Minute))
	_, refresh := o.tokens(t, ctx, s, grant.ID, o.base.Add(time.Hour))

	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		if _, err := tx.ConsumeAuthCode(ctx, code, o.base); err == nil {
			t.Errorf("ConsumeAuthCode inside WithTx succeeded")
		}
		if _, err := tx.RotateRefreshToken(ctx, refresh, o.rotation(o.base.Add(time.Hour)), o.base); err == nil {
			t.Errorf("RotateRefreshToken inside WithTx succeeded")
		}
		if _, err := tx.CheckRefreshToken(ctx, refresh, o.base); err == nil {
			t.Errorf("CheckRefreshToken inside WithTx succeeded")
		}
		// Other OAuth writes join the caller's transaction.
		_, err := tx.ApproveOAuthAuthRequest(ctx, o.authRequest(t, ctx, tx, o.base.Add(time.Minute)), user.ID, o.base)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
}
