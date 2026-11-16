package server

import (
	"context"
	"fmt"
	"maps"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// contextRecordingProvider records the log context request it receives.
type contextRecordingProvider struct {
	failingLokiProvider
	got datasource.LogContextRequest
}

func (p *contextRecordingProvider) GetLogContext(_ context.Context, _ *models.Source, req datasource.LogContextRequest) (*models.LogContextResponse, error) {
	p.got = req
	return &models.LogContextResponse{}, nil
}

func TestLogContextPassesStreamLabels(t *testing.T) {
	provider := &contextRecordingProvider{}
	s, source := newAggregationTestServer(t, provider, models.SourceTypeLoki)
	app := fiber.New()
	user, src := authorizeTestSource(t, s.store, source.ID)
	withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/logs/context", user, s.handleGetLogContext)
	path := fmt.Sprintf("/teams/%d/sources/%d/logs/context", src.TeamID(), source.ID)

	status, _, _ := postJSON(t, app, path, `{"timestamp":1793500000000,"stream_labels":{"app":"api","pod":"a"}}`)
	if status != fiber.StatusOK {
		t.Fatalf("status %d", status)
	}
	if want := map[string]string{"app": "api", "pod": "a"}; !maps.Equal(provider.got.StreamLabels, want) {
		t.Fatalf("stream labels %v, want %v", provider.got.StreamLabels, want)
	}
}
