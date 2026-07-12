package core

import (
	"context"
	"log/slog"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// fakeProvider is a minimal datasource.Provider stand-in so alert/saved-query
// tests can exercise the real validation + eval wiring (datasource.Service)
// without a live ClickHouse/VictoriaLogs connection. Only the methods actually
// exercised by CreateAlert/UpdateAlert/TestAlertQuery/CreateSavedQuery have
// meaningful bodies; the rest satisfy the interface with zero values since the
// core package never calls them in these tests.
type fakeProvider struct {
	queryLanguages  []models.QueryLanguage
	savedQueryModes []models.SavedQueryEditorMode
	alertModes      []models.AlertEditorMode
	evaluateAlertFn func(ctx context.Context, source *models.Source, req datasource.AlertQueryRequest) (*models.QueryResult, error)
}

func (f *fakeProvider) Type() models.SourceType { return models.SourceTypeClickHouse }

func (f *fakeProvider) Capabilities() []datasource.Capability { return nil }

func (f *fakeProvider) SupportedQueryLanguages() []models.QueryLanguage { return f.queryLanguages }

func (f *fakeProvider) SupportedSavedQueryEditorModes() []models.SavedQueryEditorMode {
	return f.savedQueryModes
}

func (f *fakeProvider) SupportedAlertEditorModes() []models.AlertEditorMode { return f.alertModes }

func (f *fakeProvider) PrepareSource(context.Context, *models.CreateSourceRequest) (*models.Source, error) {
	return nil, nil
}

func (f *fakeProvider) ValidateConnection(context.Context, *models.ValidateConnectionRequest) (*models.ConnectionValidationResult, error) {
	return nil, nil
}

func (f *fakeProvider) UpdateSource(context.Context, *models.Source, *models.UpdateSourceRequest) (*datasource.SourceUpdateResult, error) {
	return nil, nil
}

func (f *fakeProvider) PopulateSourceDetails(context.Context, *models.Source) error { return nil }

func (f *fakeProvider) QueryLogs(context.Context, *models.Source, datasource.QueryRequest) (*models.QueryResult, error) {
	return nil, nil
}

func (f *fakeProvider) GetSourceSchema(context.Context, *models.Source) ([]models.ColumnInfo, error) {
	return nil, nil
}

func (f *fakeProvider) Histogram(context.Context, *models.Source, datasource.HistogramRequest) (*datasource.HistogramResult, error) {
	return nil, nil
}

func (f *fakeProvider) GetFieldValues(context.Context, *models.Source, datasource.FieldValuesRequest) (*datasource.FieldValuesResult, error) {
	return nil, nil
}

func (f *fakeProvider) GetAllFieldValues(context.Context, *models.Source, datasource.AllFieldValuesRequest) (datasource.AllFieldValuesResult, error) {
	return nil, nil
}

func (f *fakeProvider) InspectSource(context.Context, *models.Source) (*datasource.SourceInspection, error) {
	return nil, nil
}

func (f *fakeProvider) EvaluateAlert(ctx context.Context, source *models.Source, req datasource.AlertQueryRequest) (*models.QueryResult, error) {
	if f.evaluateAlertFn != nil {
		return f.evaluateAlertFn(ctx, source, req)
	}
	return &models.QueryResult{}, nil
}

func (f *fakeProvider) InitializeSource(context.Context, *models.Source) error { return nil }

func (f *fakeProvider) RemoveSource(models.SourceID) error { return nil }

func (f *fakeProvider) CheckSourceConnectionStatus(context.Context, *models.Source) bool { return true }

func (f *fakeProvider) GetSourceHealth(context.Context, models.SourceID) models.SourceHealth {
	return models.SourceHealth{}
}

// newFakeDatasourceService returns a *datasource.Service backed by db with a
// fakeProvider registered for clickhouse (the default source type when a test
// source is created without one). queryLanguages/savedQueryModes/alertModes
// default to permissive supersets when nil so callers only need to constrain
// what they're testing.
func newFakeDatasourceService(db store.Store, log *slog.Logger, p *fakeProvider) *datasource.Service {
	if p == nil {
		p = &fakeProvider{}
	}
	if p.queryLanguages == nil {
		p.queryLanguages = []models.QueryLanguage{
			models.QueryLanguageLogchefQL,
			models.QueryLanguageClickHouseSQL,
			models.QueryLanguageLogsQL,
		}
	}
	if p.savedQueryModes == nil {
		p.savedQueryModes = []models.SavedQueryEditorMode{
			models.SavedQueryEditorModeBuilder,
			models.SavedQueryEditorModeNative,
		}
	}
	if p.alertModes == nil {
		p.alertModes = []models.AlertEditorMode{
			models.AlertEditorModeNative,
			models.AlertEditorModeCondition,
		}
	}
	svc := datasource.NewService(db, log)
	svc.Register(p)
	return svc
}
