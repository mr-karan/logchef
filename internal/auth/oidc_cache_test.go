package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestCallbackReusesSigningKeys(t *testing.T) {
	fake := newFakeOIDCServer(t)
	fake.registerCode("code", OIDCClaims{Email: "user@example.com", EmailVerified: new(true)})
	log := discardLoggerAP()
	p, err := NewOIDCProvider(t.Context(), testOIDCConfig(fake), log)
	if err != nil {
		t.Fatal(err)
	}
	db := newAuthTestDB(t)
	for i := range 20 {
		_, _, err := p.HandleCallback(t.Context(), db, log, &config.AuthConfig{}, "code", "state")
		if !errors.Is(err, ErrUnauthorizedUser) {
			t.Fatalf("signed login %d = %v, want unauthorized user after verification", i+1, err)
		}
	}
	if got := fake.jwksRequests.Load(); got != 1 {
		t.Errorf("20 signed logins fetched JWKS %d times, want 1", got)
	}
	fake.jwksUnavailable.Store(true)
	_, _, err = p.HandleCallback(t.Context(), db, log, &config.AuthConfig{}, "code", "state")
	if !errors.Is(err, ErrUnauthorizedUser) {
		t.Fatalf("cached signing key during JWKS outage = %v, want unauthorized user after verification", err)
	}
	if got := fake.jwksRequests.Load(); got != 1 {
		t.Errorf("cached signing key fetched JWKS during outage, count = %d", got)
	}
}

func TestCallbackDatabaseOutlivesIDPDeadline(t *testing.T) {
	fake := newFakeOIDCServer(t)
	claims := OIDCClaims{Email: "user@example.com", EmailVerified: new(true)}
	fake.registerCode("code", claims)
	log := discardLoggerAP()
	p, err := NewOIDCProvider(t.Context(), testOIDCConfig(fake), log)
	if err != nil {
		t.Fatal(err)
	}
	p.requestTimeout = 100 * time.Millisecond
	raw, err := fake.signIDToken(claims)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.verify(t.Context(), raw); err != nil {
		t.Fatal(err)
	}
	db := newAuthTestDB(t)
	user := &models.User{Email: claims.Email, FullName: "Test User", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := db.CreateUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	locked := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- db.WithTx(t.Context(), func(store.StoreOps) error {
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-finished:
		t.Fatalf("acquiring database writer: %v", err)
	}
	timer := time.AfterFunc(3*p.requestTimeout, func() { close(release) })
	defer func() {
		if timer.Stop() {
			close(release)
		}
		if err := <-finished; err != nil {
			t.Errorf("database writer transaction: %v", err)
		}
	}()
	_, session, err := p.HandleCallback(t.Context(), db, log, &config.AuthConfig{SessionDuration: time.Hour, MaxConcurrentSessions: 5}, "code", "state")
	if err != nil {
		t.Fatalf("database work inherited the IdP deadline: %v", err)
	}
	if _, err := db.GetSession(t.Context(), session.ID); err != nil {
		t.Fatalf("callback did not persist its session: %v", err)
	}
}
