package provisioning

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/loki"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

// datasourcesFor registers every provider, as the app does.
func datasourcesFor(db store.Store) *datasource.Service {
	ds := datasource.NewService(db, quietLogger())
	ds.Register(datasource.NewClickHouseProvider(clickhouse.NewManager(quietLogger()), quietLogger()))
	ds.Register(victorialogs.NewProvider(quietLogger()))
	ds.Register(loki.NewProvider(quietLogger()))
	return ds
}

func testDatasources() *datasource.Service { return datasourcesFor(nil) }

// noLogchefQL is a ClickHouse provider whose descriptor lists no LogchefQL.
type noLogchefQL struct{ datasource.Provider }

func (p noLogchefQL) Describe() datasource.Descriptor {
	d := p.Provider.Describe()
	d.QueryLanguages = slices.DeleteFunc(slices.Clone(d.QueryLanguages), func(l models.QueryLanguage) bool {
		return l == models.QueryLanguageLogchefQL
	})
	return d
}

// viewSource is a ClickHouse source on an unreachable port, so the
// best-effort connection check fails fast.
func viewSource(name string, views ...config.ProvisionView) config.ProvisionSource {
	return config.ProvisionSource{
		Name:        name,
		MetaTSField: "timestamp",
		Connection: map[string]any{
			"host":       "127.0.0.1:1",
			"database":   "default",
			"table_name": "logs",
		},
		Views: views,
	}
}

// The RFC example, read through config.Load so the TOML decoding of the
// inline column and sort tables is covered.
func TestSourceViewsFromTOML(t *testing.T) {
	dir := t.TempDir()
	mainConfig := `
[auth]
admin_emails = ["admin@example.com"]
api_token_secret = "0123456789abcdef0123456789abcdef"

[oidc]
provider_url = "http://localhost/dex"
auth_url = "http://localhost/dex/auth"
token_url = "http://localhost/dex/token"
client_id = "logchef"
redirect_url = "http://localhost/callback"

[provisioning]
file = "provisioning.toml"
`
	provisioningTOML := `
manage_sources = true

[[sources]]
name = "nginx"
[sources.connection]
host = "127.0.0.1:9000"
database = "default"
table_name = "nginx"

  [[sources.views]]
  name = "  Access  "
  description = "Requests that failed"
  default = true
  display_mode = "table"
  time_range = "1h"
  filter = 'status >= 400'
  sort = [{ column = "timestamp", desc = true }]
  columns = [{ name = "timestamp", width = 190 }, { name = "status" }, { name = "request_uri", width = 520 }]

  [[sources.views]]
  name = "Compact"
  display_mode = "compact"
`
	for name, body := range map[string]string{"config.toml": mainConfig, "provisioning.toml": provisioningTOML} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	ResolveConfig(&cfg.Provisioning)
	if err := ValidateConfig(&cfg.Provisioning, testDatasources()); err != nil {
		t.Fatalf("the RFC example should pass: %v", err)
	}

	views := cfg.Provisioning.Sources[0].Views
	if len(views) != 2 {
		t.Fatalf("decoded %d views, want 2", len(views))
	}
	access := views[0]
	if access.Name != "Access" || access.Description != "Requests that failed" || !access.Default {
		t.Errorf("access view = %+v, want the name trimmed and default set", access)
	}
	table := models.DisplayModeTable
	want := models.SavedViewDefinition{
		Version:     1,
		Columns:     []models.SavedViewColumn{{Name: "timestamp", Width: new(190)}, {Name: "status"}, {Name: "request_uri", Width: new(520)}},
		Sort:        []models.SavedViewSort{{Column: "timestamp", Desc: true}},
		DisplayMode: &table,
		Filter:      new("status >= 400"),
		TimeRange:   new("1h"),
	}
	if got := access.Definition(); !reflect.DeepEqual(got, want) {
		t.Errorf("definition = %+v, want %+v", got, want)
	}
	compact := models.DisplayModeCompact
	if got := views[1].Definition(); !reflect.DeepEqual(got, models.SavedViewDefinition{Version: 1, DisplayMode: &compact}) {
		t.Errorf("compact definition = %+v", got)
	}
}

func TestValidateSourceViews(t *testing.T) {
	ok := config.ProvisionView{Name: "ok", TimeRange: new("15m")}
	tooMany := make([]config.ProvisionView, models.MaxSavedViewsPerScope+1)
	for i := range tooMany {
		tooMany[i] = config.ProvisionView{Name: strings.Repeat("v", i+1)}
	}
	cases := []struct {
		name    string
		src     config.ProvisionSource
		ds      *datasource.Service
		wantErr string
	}{
		{name: "valid", src: viewSource("s", ok, config.ProvisionView{Name: "weekly", TimeRange: new("2w"), Default: true})},
		{name: "two defaults", src: viewSource("s", config.ProvisionView{Name: "a", Default: true}, config.ProvisionView{Name: "b", Default: true}),
			wantErr: `views[1] ("b"): default is also set on view "a"`},
		{name: "duplicate name", src: viewSource("s", ok, ok), wantErr: `views[1] ("ok"): duplicate view name`},
		{name: "empty name", src: viewSource("s", config.ProvisionView{Name: ""}), wantErr: "name is required"},
		{name: "invalid filter", src: viewSource("s", config.ProvisionView{Name: "f", Filter: new("level = ")}),
			wantErr: `views[0] ("f"): invalid saved view definition: filter is not valid LogchefQL`},
		{name: "no LogchefQL on the source type", src: viewSource("s", config.ProvisionView{Name: "f", Filter: new(`level = "error"`)}),
			ds: func() *datasource.Service {
				ds := testDatasources()
				ch, _ := ds.ProviderForSourceType(models.SourceTypeClickHouse)
				ds.Register(noLogchefQL{ch})
				return ds
			}(),
			wantErr: "this source does not support LogchefQL"},
		{name: "seconds", src: viewSource("s", ok, config.ProvisionView{Name: "fast", TimeRange: new("30s")}),
			wantErr: `views[1] ("fast"): invalid saved view definition: time_range "30s" is in seconds`},
		{name: "absolute range", src: viewSource("s", config.ProvisionView{Name: "t", TimeRange: new("2026-01-01")}),
			wantErr: "time_range must be a relative range"},
		{name: "display mode", src: viewSource("s", config.ProvisionView{Name: "d", DisplayMode: new(models.DisplayModePreference("grid"))}),
			wantErr: "display_mode must be"},
		{name: "empty columns", src: viewSource("s", config.ProvisionView{Name: "c", Columns: []models.SavedViewColumn{}}),
			wantErr: "columns must have at least one entry"},
		{name: "narrow column", src: viewSource("s", config.ProvisionView{Name: "c", Columns: []models.SavedViewColumn{{Name: "msg", Width: new(10)}}}),
			wantErr: "columns[0].width must be from 50 to 3000"},
		{name: "too many views", src: viewSource("s", tooMany...), wantErr: "declares 51 views, the limit is 50"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds := tc.ds
			if ds == nil {
				ds = testDatasources()
			}
			cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{tc.src}}
			ResolveConfig(cfg)
			err := ValidateConfig(cfg, ds)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateConfig = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ValidateConfig = %v, want an error containing %q", err, tc.wantErr)
			}
			if !strings.Contains(err.Error(), `sources[0] ("s")`) {
				t.Errorf("error %q does not name the source", err)
			}
		})
	}
}

func TestReconcileSourceViewsSQLite(t *testing.T) {
	runSourceViewReconcile(t, newReconcileTestDB(t))
}

func TestReconcileSourceViewsPostgres(t *testing.T) {
	db, _, _ := newProvisioningPostgres(t)
	runSourceViewReconcile(t, db)
}

func TestExportImportSourceViewsSQLite(t *testing.T) {
	runSourceViewExportImport(t, newReconcileTestDB(t))
}

func TestExportImportSourceViewsPostgres(t *testing.T) {
	db, _, _ := newProvisioningPostgres(t)
	runSourceViewExportImport(t, db)
}

type viewWorld struct {
	t  *testing.T
	db store.Store
	ds *datasource.Service
}

func (w viewWorld) reconcile(cfg *config.ProvisioningConfig) {
	w.t.Helper()
	if err := Reconcile(w.t.Context(), cfg, w.db, w.ds, testRecorder(w.db), quietLogger(), nil); err != nil {
		w.t.Fatalf("Reconcile: %v", err)
	}
}

func (w viewWorld) source(name string) *models.Source {
	w.t.Helper()
	src, err := w.db.GetSourceByNameForProvisioning(w.t.Context(), name)
	if err != nil {
		w.t.Fatalf("source %q: %v", name, err)
	}
	return src
}

// views returns the source views of the nginx source by name.
func (w viewWorld) views() map[string]*models.SavedView {
	w.t.Helper()
	views, err := listSourceScopeViews(w.t.Context(), w.db, w.source("nginx").ID)
	if err != nil {
		w.t.Fatal(err)
	}
	out := make(map[string]*models.SavedView, len(views))
	for _, v := range views {
		out[v.Name] = v
	}
	return out
}

func runSourceViewReconcile(t *testing.T, db store.Store) {
	w := viewWorld{t: t, db: db, ds: datasourcesFor(db)}
	access := config.ProvisionView{Name: "Access", Default: true, Filter: new("status >= 400"), TimeRange: new("1h"),
		Columns: []models.SavedViewColumn{{Name: "timestamp", Width: new(190)}, {Name: "status"}}}
	errorsView := config.ProvisionView{Name: "Errors", Filter: new(`level = "error"`)}
	cfg := func(prune bool, views ...config.ProvisionView) *config.ProvisioningConfig {
		return &config.ProvisioningConfig{ManageSources: true, Prune: prune, Sources: []config.ProvisionSource{viewSource("nginx", views...)}}
	}

	// A declared view is created managed, with its default.
	w.reconcile(cfg(false, access, errorsView))
	got := w.views()
	if len(got) != 2 || !got["Access"].Managed || !got["Errors"].Managed || got["Access"].Revision != 1 {
		t.Fatalf("after the first run: %+v", got)
	}
	if !got["Access"].IsDefault || got["Errors"].IsDefault {
		t.Fatalf("default: Access %v, Errors %v; want true, false", got["Access"].IsDefault, got["Errors"].IsDefault)
	}
	if !reflect.DeepEqual(got["Access"].Definition, access.Definition()) || got["Access"].CreatedBy != nil {
		t.Fatalf("stored Access = %+v", got["Access"])
	}

	// A second run with the same file writes nothing.
	w.reconcile(cfg(false, access, errorsView))
	if again := w.views(); again["Access"].Revision != 1 || again["Errors"].Revision != 1 || !again["Access"].IsDefault {
		t.Fatalf("an unchanged file changed views: Access %+v, Errors %+v", again["Access"], again["Errors"])
	}

	// A changed definition increases the revision; the default moves.
	changed := access
	changed.Default = false
	changed.TimeRange = new("6h")
	errorsDefault := errorsView
	errorsDefault.Default = true
	w.reconcile(cfg(false, changed, errorsDefault))
	got = w.views()
	if got["Access"].Revision != 2 || *got["Access"].Definition.TimeRange != "6h" || got["Errors"].Revision != 1 {
		t.Fatalf("after a change: Access %+v, Errors %+v", got["Access"], got["Errors"])
	}
	if got["Access"].IsDefault || !got["Errors"].IsDefault {
		t.Fatalf("default did not move: Access %v, Errors %v", got["Access"].IsDefault, got["Errors"].IsDefault)
	}

	// An unmanaged view with a declared name is adopted with its id; one that
	// the file does not name is left alone.
	nginx := w.source("nginx")
	manual := &models.SavedView{SourceID: nginx.ID, Scope: models.SavedViewScopeSource, Name: "Manual", Definition: models.SavedViewDefinition{Version: 1}}
	adoptee := &models.SavedView{SourceID: nginx.ID, Scope: models.SavedViewScopeSource, Name: "Slow", Definition: models.SavedViewDefinition{Version: 1}}
	for _, v := range []*models.SavedView{manual, adoptee} {
		if err := db.CreateSavedView(w.t.Context(), v); err != nil {
			t.Fatal(err)
		}
	}
	slow := config.ProvisionView{Name: "Slow", Filter: new("duration_ms > 1000")}
	w.reconcile(cfg(false, changed, errorsDefault, slow))
	got = w.views()
	if got["Slow"].ID != adoptee.ID || !got["Slow"].Managed || got["Slow"].Revision != 2 || *got["Slow"].Definition.Filter != "duration_ms > 1000" {
		t.Fatalf("adopted view = %+v, want id %d, managed, revision 2", got["Slow"], adoptee.ID)
	}
	if got["Manual"].Managed {
		t.Fatal("an undeclared unmanaged view became managed")
	}

	// Without prune an undeclared managed view stays and loses a default the
	// file no longer declares.
	w.reconcile(cfg(false, changed, slow))
	got = w.views()
	if _, kept := got["Errors"]; !kept || got["Errors"].IsDefault {
		t.Fatalf("prune=false: Errors = %+v, want kept with no default", got["Errors"])
	}

	// A file with no default leaves an unmanaged default alone.
	if _, err := db.SetSavedViewDefault(w.t.Context(), manual.ID); err != nil {
		t.Fatal(err)
	}
	w.reconcile(cfg(false, changed, slow))
	if !w.views()["Manual"].IsDefault {
		t.Fatal("a file with no default cleared an unmanaged default")
	}

	// Prune deletes the undeclared managed view and keeps the unmanaged one.
	w.reconcile(cfg(true, changed, slow))
	got = w.views()
	if _, kept := got["Errors"]; kept {
		t.Fatal("prune kept an undeclared managed view")
	}
	if _, kept := got["Manual"]; !kept || len(got) != 3 {
		t.Fatalf("after prune: %v, want Access, Manual and Slow", slices.Sorted(maps.Keys(got)))
	}

	// A declared default takes the source default from an unmanaged view.
	slowDefault := slow
	slowDefault.Default = true
	w.reconcile(cfg(true, changed, slowDefault))
	got = w.views()
	if !got["Slow"].IsDefault || got["Manual"].IsDefault {
		t.Fatalf("declared default: Slow %v, Manual %v; want true, false", got["Slow"].IsDefault, got["Manual"].IsDefault)
	}
}

// Export writes the source views back out, so importing the export changes
// nothing.
func runSourceViewExportImport(t *testing.T, db store.Store) {
	w := viewWorld{t: t, db: db, ds: datasourcesFor(db)}
	declared := []config.ProvisionView{
		{Name: "Access", Description: "Failed requests", Default: true, DisplayMode: new(models.DisplayModeTable), Filter: new("status >= 400"), TimeRange: new("1h"),
			Sort:    []models.SavedViewSort{{Column: "timestamp", Desc: true}, {Column: "status"}},
			Columns: []models.SavedViewColumn{{Name: "timestamp", Width: new(190)}, {Name: "status"}}},
		{Name: "Bare"},
		{Name: "Empty filter", Filter: new("")},
	}
	w.reconcile(&config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{viewSource("nginx", declared...)}})
	before := w.views()

	exported, err := ExportConfig(w.t.Context(), db)
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	if len(exported.Sources) != 1 || !reflect.DeepEqual(exported.Sources[0].Views, declared) {
		t.Fatalf("exported views = %+v, want %+v", exported.Sources[0].Views, declared)
	}

	w.reconcile(exported)
	after := w.views()
	if len(after) != len(before) {
		t.Fatalf("import changed the view count from %d to %d", len(before), len(after))
	}
	for name, b := range before {
		a := after[name]
		if a == nil || a.ID != b.ID || a.Revision != b.Revision || a.IsDefault != b.IsDefault || !a.Managed || !a.UpdatedAt.Equal(b.UpdatedAt) {
			t.Errorf("view %q changed on import: before %+v, after %+v", name, b, a)
		}
	}
}
