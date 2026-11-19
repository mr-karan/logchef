# RFC 0007: Explorer saved views

Status: Draft
Date: 2026-11-15
Authors: Karan Sharma

## Summary

The Explorer keeps a table layout per team and source in the browser. Nobody
can name a layout, move it to another browser or give it to a teammate.

This RFC adds the saved view. A saved view is a named Explorer setup for one
source. It holds the visible columns with their order and widths, the sort,
the display mode, a default filter and a default time range. A view has one of
three scopes. A personal view belongs to one user. A team view belongs to one
team. A source view is visible to every team that reaches the source. Each
scope can mark one view as the default for the source.

A saved view is a new stored object, not an extension of saved queries. The
Explorer URL names the active view as `view=<id>`. A share link carries a copy
of the layout, not a reference to a view. The CLI, MCP and provisioning TOML
each get a small addition.

## Problem

The Explorer has three kinds of state today, and each lives in a different
place.

| State | Where it lives | Evidence |
|---|---|---|
| Column order, widths and visibility | `localStorage`, one key per team and source | `frontend/src/composables/useColumnState.ts:55` |
| Sort | A component ref. A reload resets it to timestamp descending | `frontend/src/views/explore/table/data-table.vue:76`, `frontend/src/views/explore/table/data-table.vue:481` |
| Display mode (table, compact, JSON) | One user preference for every source | `frontend/src/views/explore/LogExplorer.vue:171`, `pkg/models/preferences.go:51` |
| Query text, time range, limit | The URL, a saved query, a share, or a local draft | `frontend/src/lib/exploreLocation.ts:8`, `frontend/src/stores/explore.ts:206` |

Four things follow.

- A layout does not leave the browser. A user who tunes 12 columns for an
  nginx source tunes them again on a second machine.
- A layout has no name. One source often needs two layouts, such as "access"
  and "errors". The browser holds one.
- A team cannot agree on a layout. A new member sees the automatic column
  order, not the columns the team reads every day.
- A source cannot open in a useful state. Every source opens with an empty
  query and a 15 minute range (`frontend/src/stores/explore.ts:64`).

Saved queries do not fill the gap. A saved query stores a query, a time range,
a limit and variables (`pkg/models/query.go:284`). It stores no columns, no
sort and no display mode.

## Goals and non-goals

Goals:

- A user saves the current Explorer layout under a name and applies it later,
  on any browser.
- A team shares views for a source. Every team member sees them.
- A source, a team and a user can each have one default view for a source.
- The Explorer URL names the active view, so a link opens with it.
- A view keeps working when the source schema loses a column that the view
  names.
- The CLI opens the Explorer with a view. MCP lists views. Provisioning TOML
  declares source views.

Non-goals:

- **A stored column-filter structure.** Issue #6 asks for filters on table
  columns. They are query state. "Column filters" below gives the rule.
- **Views in collections.** A view is found from its source, in the Explorer.
  "Alternatives rejected" gives the reason.
- **Native SQL as the default filter.** The default filter is LogchefQL. A
  SQL query names its own columns, so it is a saved query.
- **Histogram group-by, wrap, row limit and the fields panel.** The assignment
  names five parts of a view. The definition is versioned, so a later RFC can
  add a part without a migration.
- **Views for dashboards and alerts.** Neither renders the Explorer table.
- **`logchef query --view`.** The CLI does not use a view to pick output
  columns in this RFC.

## Design

### A saved view is a new stored object

The assignment asks whether a saved view is a new object or an extension of
saved queries. It is a new object. Five differences decide it.

| | Saved query | Saved view |
|---|---|---|
| What it is | One query | A way to look at a source, across many queries |
| Lifetime in the Explorer | Ends when the user edits the query | Stays while the user runs other queries |
| Visibility | Every user with access to the source (`pkg/models/query.go:294`) | Personal, team or source |
| Required content | A query text and a limit above zero (`internal/core/saved_queries.go:54`) | Nothing. A view can hold only columns |
| Language | LogchefQL, ClickHouse SQL or LogsQL | LogchefQL filter only |

The lifetime is the deciding difference. Opening a saved query replaces the
editor text (`frontend/src/stores/explore.ts:455`). A view must not do that.
A user picks the "errors" view and then types ten queries under it. If the
columns lived in a saved query, the Explorer would need an action that applies
the columns of a saved query and ignores its text. That action is a view under
another name, with a query attached that nobody wants.

The visibility difference is the second reason. A saved query has no private
form. A personal view needs one. Adding a private form to saved queries would
change every saved-query list query in both stores, the collection item
routes, the CLI and two MCP tools.

RFC 0003 decision 7 makes collections the home for dashboards and saved
queries. It does not name views, and this RFC keeps views out of collections.
A user browses a collection. A user picks a view from a menu in the Explorer,
for the source on screen. "Alternatives rejected" covers collections and user
preferences.

### The Explorer state that a view captures

A view definition has five optional parts. An absent part means that the view
says nothing about that part, and the Explorer keeps what it has.

| Part | Field | Applies to | Source of truth today |
|---|---|---|---|
| Visible columns, in order, with widths | `columns` | Table mode | `frontend/src/composables/useColumnState.ts:15` |
| Sort | `sort` | Table mode | `frontend/src/views/explore/table/data-table.vue:76` |
| Display mode | `display_mode` | The results pane | `pkg/models/preferences.go:38` |
| Default filter | `filter` | The LogchefQL editor | `frontend/src/stores/explore.ts:84` |
| Default time range | `time_range` | The time picker | `frontend/src/stores/explore.ts:81` |

The first three parts are the layout. The last two parts are the query
defaults. "Applying a view" gives each group its rule. The compact list and
the JSON pane ignore `columns` and `sort`.

The table sorts in the browser
(`frontend/src/views/explore/table/tableFeatures.ts:19`). A view's sort orders
the rows that the query returned and never changes an `ORDER BY`.

### Stored object

One table, `saved_views`, in both metadata stores.

| Column | SQLite type | Postgres type | Meaning |
|---|---|---|---|
| `id` | `INTEGER PRIMARY KEY AUTOINCREMENT` | `BIGSERIAL PRIMARY KEY` | View id |
| `source_id` | `INTEGER NOT NULL` | `BIGINT NOT NULL` | The source. `ON DELETE CASCADE` |
| `scope` | `TEXT NOT NULL` | `TEXT NOT NULL` | `personal`, `team` or `source` |
| `user_id` | `INTEGER` | `BIGINT` | The owner of a personal view. `ON DELETE CASCADE` |
| `team_id` | `INTEGER` | `BIGINT` | The team of a team view. `ON DELETE CASCADE` |
| `name` | `TEXT NOT NULL` | `TEXT NOT NULL` | 1 to 200 code points |
| `description` | `TEXT NOT NULL DEFAULT ''` | same | Up to 2,000 code points |
| `definition` | `TEXT NOT NULL` | `TEXT NOT NULL` | JSON, see below |
| `is_default` | `INTEGER NOT NULL DEFAULT 0` | `BOOLEAN NOT NULL DEFAULT FALSE` | The default view of its scope for this source |
| `managed` | `INTEGER NOT NULL DEFAULT 0` | `BOOLEAN NOT NULL DEFAULT FALSE` | Provisioning owns the row |
| `revision` | `INTEGER NOT NULL DEFAULT 1` | `INTEGER NOT NULL DEFAULT 1` | Increases by one on every update |
| `created_by` | `INTEGER` | `BIGINT` | `ON DELETE SET NULL` |
| `created_at`, `updated_at` | `DATETIME` | `TIMESTAMPTZ` | As other tables |

A `CHECK` constraint ties the scope to its owner columns. A personal view
has a `user_id` and no `team_id`. A team view has a `team_id` and no
`user_id`. A source view has neither.

Six partial unique indexes enforce two rules. A name is unique inside its
scope and source. A scope has at most one default per source.

```sql
CREATE UNIQUE INDEX idx_saved_views_personal_name ON saved_views(source_id, user_id, name) WHERE scope = 'personal';
CREATE UNIQUE INDEX idx_saved_views_team_name     ON saved_views(source_id, team_id, name) WHERE scope = 'team';
CREATE UNIQUE INDEX idx_saved_views_source_name   ON saved_views(source_id, name)          WHERE scope = 'source';
CREATE UNIQUE INDEX idx_saved_views_personal_default ON saved_views(source_id, user_id) WHERE scope = 'personal' AND is_default;
CREATE UNIQUE INDEX idx_saved_views_team_default     ON saved_views(source_id, team_id) WHERE scope = 'team' AND is_default;
CREATE UNIQUE INDEX idx_saved_views_source_default   ON saved_views(source_id)          WHERE scope = 'source' AND is_default;
```

SQLite writes the last three predicates as `is_default = 1`. One more index,
on `(source_id)`, serves the list query.

A team view has no foreign key to `team_sources`. When a team loses its link
to a source, the team's views for that source stay in the table and no route
can reach them. They return when the link returns. A cascade would delete
them on an unlink that provisioning later undoes.

Each store gets one migration pair named `NNNNNN_add_saved_views`, with the
next free number at build time. The up migration creates the table and its
indexes. The down migration drops the table. No existing row changes.

### View definition

The `definition` column holds this JSON.

```json
{
  "version": 1,
  "columns": [{ "name": "timestamp", "width": 190 }, { "name": "level" }, { "name": "msg", "width": 640 }],
  "sort": [{ "column": "timestamp", "desc": true }],
  "display_mode": "table",
  "filter": "level = \"error\"",
  "time_range": "1h"
}
```

| Field | Rule |
|---|---|
| `version` | Required. `1` |
| `columns` | Optional. 1 to 200 entries. The list is the visible columns in order. Every column not in the list is hidden |
| `columns[].name` | Required. 1 to 512 bytes. No duplicates in one view. The server treats the name as an opaque string |
| `columns[].width` | Optional. An integer from 50 to 3,000 pixels, the table's own bounds (`frontend/src/views/explore/table/data-table.vue:107`). Absent means automatic width |
| `sort` | Optional. 1 to 3 entries, most significant first. `column` follows the name rule. `desc` is a boolean |
| `display_mode` | Optional. `table`, `compact` or `json`, the values of `models.DisplayModePreference` |
| `filter` | Optional. LogchefQL, up to 8,192 bytes. It must pass `logchefql.Validate`. The source must support LogchefQL |
| `time_range` | Optional. A relative range that matches `^\d+[smhdw]$`, the rule that saved queries use (`internal/core/saved_queries.go:18`) |

An absolute time range is not allowed. A default that names fixed dates is
stale on the day after it is saved.

The server rejects an unknown key with 400. A stored definition never holds a
key that the server does not validate. A definition is at most 65,536 bytes.

The server does not check column names against the source schema. "Missing
columns" defines what happens at read time.

### Models

New file `pkg/models/saved_view.go`.

| Type | Shape |
|---|---|
| `SavedViewScope` | A string type with the constants `SavedViewScopePersonal`, `SavedViewScopeTeam` and `SavedViewScopeSource` |
| `SavedViewColumn` | `Name string`, `Width *int` |
| `SavedViewSort` | `Column string`, `Desc bool` |
| `SavedViewDefinition` | `Version int`, `Columns []SavedViewColumn`, `Sort []SavedViewSort`, `DisplayMode *DisplayModePreference`, `Filter *string`, `TimeRange *string`. Every field but `Version` has `omitempty` |

`SavedView` mirrors the table columns, with `Definition SavedViewDefinition`
in place of the JSON text. It adds two computed fields for the caller,
`CanEdit` and `CanSetDefault`, as saved queries do
(`pkg/models/query.go:322`).

`SavedViewDefinition.Validate() error` holds every rule in the table above
except the LogchefQL check, which needs the `logchefql` package and runs in
core. `ParseSavedViewDefinition(raw []byte)` decodes with
`DisallowUnknownFields` and calls `Validate`. The API, provisioning and the
share payload all parse through this one function.

The API carries `definition` as a JSON object. Saved queries carry a JSON
string (`pkg/models/query.go:309`), and every client parses it twice.

### API

All routes sit in the existing group for one team and one source
(`internal/server/server.go:469`). `requireTeamMember` and
`requireTeamHasSource` run first, so every route already knows that the caller
reaches the source through the team.

| Method and path | Scope | Purpose |
|---|---|---|
| `GET /api/v1/teams/:teamID/sources/:sourceID/views` | `saved_views:read` | List the views that the caller can see here |
| `POST /api/v1/teams/:teamID/sources/:sourceID/views` | `saved_views:write` | Create a view |
| `GET /api/v1/teams/:teamID/sources/:sourceID/views/:viewID` | `saved_views:read` | Read one view |
| `PUT /api/v1/teams/:teamID/sources/:sourceID/views/:viewID` | `saved_views:write` | Update name, description and definition |
| `DELETE /api/v1/teams/:teamID/sources/:sourceID/views/:viewID` | `saved_views:write` | Delete a view |
| `PUT /api/v1/teams/:teamID/sources/:sourceID/views/:viewID/default` | `saved_views:write` | Make the view the default of its scope |
| `DELETE /api/v1/teams/:teamID/sources/:sourceID/views/:viewID/default` | `saved_views:write` | Clear the default flag |

Two token scopes are new, `saved_views:read` and `saved_views:write`.
`saved_views:read` joins the read-only preset
(`internal/core/api_tokens.go:103`).

**List.** The response holds the caller's personal views for the source, the
team views of `:teamID` for the source, and the source views. It also holds
the id that the default rules pick. The response body is `{"views": [...], "default_view_id": 12}`.

`default_view_id` is the caller's personal default if one exists, else the
team default, else the source default, else `null`. The server computes it so
that the web UI, the CLI and MCP agree. The list is ordered by scope
(personal, team, source) and then by name. It has no paging, because the caps
under "Limits and failure modes" bound it at 150 rows.

**Create.** The body is `name`, `description`, `scope`, `definition` and an
optional `is_default`. The server sets `user_id` from the caller for a
personal view and `team_id` from the path for a team view. A body cannot name
another user or another team. `is_default` needs the same authority as the
default route, and answers 403 without it. The response is the view, with
status 201.

**Update.** The body is `name`, `description`, `definition` and `revision`.
The update is one `UPDATE ... SET revision = revision + 1 WHERE id = ? AND
revision = ?`. Zero rows changed means that someone saved first. The scope of
a view never changes. To move a view to another scope, the user duplicates it.

**Default.** `PUT .../default` clears the flag on the current default of the
same scope, owner and source, and sets it on this view, in one transaction.
The request is idempotent. The transaction reads the row that it would clear.
If that row is managed, the request answers 403 and changes nothing, because
provisioning owns the source default once it declares one. Create with
`is_default` follows the same rule.

Errors:

| Status | When |
|---|---|
| 400 | The body does not parse, a definition rule fails, the filter is not valid LogchefQL, `revision` is missing, or the source does not support LogchefQL and the definition has a filter |
| 403 | The caller can see the view but may not change it, the view is managed by provisioning, or the request would clear the default flag of a managed view |
| 404 | The view does not exist, belongs to another source, is another user's personal view, or is another team's view. A private view answers 404, not 403, so the route does not confirm that the id exists |
| 409 | The name is taken in that scope, the revision is stale, the scope is at its cap, or a concurrent request changed the default. "Unverified claims" item 2 covers the last case |

### Backend logic

New file `internal/core/saved_views.go` holds validation, the permission
rules and the default resolution. Handlers go in a new file,
`internal/server/saved_view_handlers.go`.

One function decides every permission. Its inputs are the view, the
`access.Principal`, the `access.AuthorizedSource` that the middleware stored
(`internal/core/access/access.go:141`) and the caller's role in that team. It
checks in this order and returns `canRead`, `canEdit` and `canSetDefault`.

1. `view.SourceID` equals `AuthorizedSource.SourceID()`. If not, nothing is
   allowed.
2. The scope's owner matches. A personal view needs `view.UserID` equal to
   the principal's user id. A team view needs `view.TeamID` equal to
   `AuthorizedSource.TeamID()`. A source view needs no owner.
3. The role rules in the table under "Access and security".

A failure of step 1 or 2 is a 404. The list fills `can_edit` and
`can_set_default` from the same function, so the UI and the server cannot
disagree.

The list is one store query. Its `WHERE` clause has three branches joined by
`OR`: the caller's personal views, the team's views and the source views. It
does not run one query per scope.

One function, `core.ValidateSavedViewDefinition(def, descriptor)`, holds the
checks that need other packages. It calls `def.Validate`. If the definition
has a filter, it calls `logchefql.Validate` and requires
`models.QueryLanguageLogchefQL` in the provider descriptor's
`QueryLanguages`. It does not call `ValidateSavedQuerySupport`
(`internal/datasource/service.go:181`), which also wants an editor mode, and a
view has none. The API gets the descriptor from `ds.ProviderForSource`.
Provisioning gets it from `ds.ProviderForSourceType`
(`internal/datasource/service.go:93`), which needs no connection.

`internal/store/store.go` gets a `SavedViewStore` interface with create, get,
list, update by revision, delete, set default, count in scope and list
managed. Both stores implement it.

### Web UI

**View picker.** A new component, `ViewPicker.vue`, sits in
`ResultsToolbar.vue` beside the display mode buttons. It shows the name of the
active view, or "No view". Its menu lists the views in three groups, with a
mark on the default. It has these actions.

| Action | Shown when | Effect |
|---|---|---|
| Pick a view | Always | Applies the view |
| No view | A view is active | Returns to the browser's own layout |
| Save as new view | Always | Opens a dialog with name, description, scope and the parts to include |
| Update view | The view is modified and `can_edit` is true | Writes the working layout to the view |
| Reset to view | A view is active | Applies the layout and the query defaults again, replacing the editor text |
| Manage views | Always | Opens the list dialog |

The save dialog has one checkbox per part. Filter and time range are
unchecked by default, because most layouts apply to any query.

**Manage views dialog.** A list of the views for the source, built from the
shared pieces in `frontend/src/components/layout/`. It uses `SearchInput`
and `useTableSearchSort` with `initialSort` name A-Z and the `persistKey`
`saved-views`, `RowActionsMenu` for Duplicate and Delete, `ConfirmDialog`
for Delete, and `EmptyState` and `LoadingState`. Rename and Set as default
are inline. A duplicated name uses `common.copyName`. A managed view shows
no edit actions.

**Working layout.** The browser keeps two layouts per team and source.

| Key | Holds | Changes |
|---|---|---|
| `logchef-tableState-<team>-<source>` | The layout with no view. Today's key | Unchanged by this RFC |
| `logchef-viewState-<team>-<source>` | The working copy of the active view, with `viewId`, `revision`, `sort` and `displayMode` | New |

Picking a view never overwrites the first key. A user who tuned a layout
before this feature gets it back with "No view".

`useColumnState` takes the storage key and an optional seed. When a view is
active, the composable reads the second key. If the stored `viewId` and
`revision` match the active view, it restores the working copy, so a reload
keeps unsaved column changes. If they differ, it discards the copy and seeds
from the view. A view that a teammate updated therefore replaces a member's
local changes the next time the Explorer loads the list.

The seed maps a definition to table state. The column order is the view's
columns, then every other column in the automatic order, with the order
source `explicit`. Visibility is true for the view's columns and false for
the rest. Sizing holds the view's widths. The rule that puts the timestamp
column first still applies (`frontend/src/composables/useColumnState.ts:80`).

The view is "modified" when the working copy, turned back into a definition,
differs from the view's definition. The picker shows a dot on a modified view.

**Display mode.** With no view, the display mode is the user preference, as
today. With a view that sets `display_mode`, the mode comes from the working
copy, and a change of mode updates the working copy only. It does not write
the preference.

**Remembered choice.** The browser stores the last pick per team and source
under `logchef-activeView-<team>-<source>`, as a view id or `null` for "No
view".

### Applying a view

**Which layout is active.** The Explorer picks the first of these that
applies.

1. `view=<id>` in the URL, when the caller can read the view. An explicit
   view wins over a share's layout.
2. The `layout` of the share that the URL names. See "URL state and share
   links".
3. The remembered choice for this team and source in this browser. A
   remembered `null` means no view and stops the search.
4. `default_view_id` from the list response.
5. No view.

**The layout applies whenever the view is active.** Columns, sort and display
mode come from the view or from its working copy.

**The query defaults fill, and they never overwrite.**

| Default | Applies when |
|---|---|
| `filter` | The view becomes active and both editors are empty. The Explorer switches to LogchefQL mode, sets the text and runs it |
| `time_range` | The Explorer opens a location that names no time range and has no stored draft. Otherwise the range in use stays |

A URL that names a query (`q`, `sql`, `id` or `share`) or a range (`t`,
`start`, `end`) keeps what it names. A stored draft keeps its text and its
range (`frontend/src/stores/explore.ts:443`). Picking a view while a query is
in the editor changes the layout and leaves the query alone. "Reset to view"
is the one action that replaces the editor text and the range.

### URL state and share links

**URL.** The Explorer URL gets one parameter, `view`. It is not a new target
kind. `ExploreTarget` stays a choice of draft, query, saved query and share
(`frontend/src/lib/exploreLocation.ts:8`), and `ExploreLocation` gets a
`viewId` field beside `teamId` and `sourceId`. A view therefore combines with
every target, such as `?team=2&source=5&view=12&q=status>=500&t=1h`.

- `normalizeQueryParams` keeps `view`, so `sameExploreLocation` treats two
  URLs with different views as different locations
  (`frontend/src/lib/exploreLocation.ts:105`).
- `queryParameters` writes `view` when a view is active
  (`frontend/src/stores/explore.ts:332`). "No view" writes nothing.
- The URL holds the id only. Unsaved changes to the working copy are not in
  the URL.
- A URL with only `team`, `source` and `view` is still a draft target. The
  stored draft opens under the view.

When the URL names a view that the caller cannot read, the Explorer opens the
rest of the location, shows one toast that says the view is not available,
and removes `view` from the URL. The query still runs. A personal view in a
link sent to a teammate takes this path.

**Share links.** A share is a snapshot. It works for any recipient with
access to the source, and it must not break when its author deletes a view.
So a share carries a copy of the layout and no view id.

`QuerySharePayload` (`pkg/models/query.go:405`) gets one optional field,
`layout`, of type `SavedViewDefinition`. The sender fills `columns`, `sort`
and `display_mode` from the working layout. The server rejects a `layout`
that has `filter` or `time_range`, because the payload already has `query`
and `time_range`. The server parses `layout` with
`ParseSavedViewDefinition`. The query and the layout share one byte budget,
the limit on the whole payload
(`internal/server/query_share_handlers.go:36`). A share that exceeds it
answers 400 with the existing message, and the UI offers to share without
the layout.

A recipient who opens the share gets the layout in memory, with no view id.
The picker shows "Shared layout" and offers "Save as new view". The shared
layout writes neither storage key and does not change the remembered choice.
It ends when the recipient picks a view or "No view", or opens a location
without the share. The Explorer then resolves the layout again from step 3.
While a shared layout is active, the Explorer writes no `view` to the URL. A
URL that names both `share` and `view` opens the share's query under the
view. A share without `layout`, which includes every share created before
this change, skips step 2 and opens under the recipient's remembered choice
or default.

### Missing columns

A named column can be absent from a result for three reasons. The schema
dropped or renamed it. A native SQL query selects other columns. A
VictoriaLogs field has no value in the rows that came back. One rule covers
all three. The view is applied to the columns that the result has.

| Case | Behavior |
|---|---|
| A view column is absent from the result | The table skips it. The other columns keep their order and widths |
| A sort column is absent | The table drops that sort entry for this result. If no entry is left, it sorts by timestamp descending, as today |
| Every view column is absent | The table falls back to the automatic layout for this result |
| A result column is not in the view | It is hidden and listed in the column selector |

The stored definition does not change. The working copy keeps the absent
names in their positions, so "Update view" does not drop them without the
user's intent.

The picker shows a warning mark when a result has rows and lacks one or more
view columns. Its tooltip lists the names under "Not in this result". The
Manage views dialog offers "Remove missing columns" for a view that the
caller can edit. It rewrites `columns` and `sort` without the absent names
and saves.

The server never rewrites a view after a schema change, so a column that
disappears for a day and returns keeps its place.

### Default views

Three scopes can each mark one default per source.

| Default | Set by | Seen by |
|---|---|---|
| Personal | The user | That user, on every browser |
| Team | A team admin or a global admin | Members of the team, for this source |
| Source | A global admin | Every user who reaches the source |

The personal default wins over the team default, and the team default wins
over the source default. A user who wants a team view as a personal default
duplicates it and marks the copy. A user who wants no view picks "No view",
and the browser remembers that. A default applies only when the browser has
no remembered choice for that team and source, so a new team default does not
move users who already picked a view.

### Column filters

Decision: a view has no column-filter structure. Header filters are query
state.

At this commit the header filters narrow only the loaded rows, and a new
query clears them (`frontend/src/views/explore/table/columnFilter.ts:3`,
`frontend/src/views/explore/table/data-table.vue:95`). The work on issue #6,
on branch `feat/table-column-filters`, replaces them with filters that write
LogchefQL conditions into the query and run it again. This RFC builds on that
behavior.

| Action | What happens to header conditions |
|---|---|
| Save a view with the filter part checked | The view's `filter` is the editor text, so it holds them |
| Pick a view | The editor text stays, so they stay |
| Reset to view | The editor text becomes the view's `filter`, so the saved conditions return |
| Share | They travel in the payload's `query`. `layout` holds none |
| Seed the table from a view | The seed sets order, visibility, widths and sort. It never sets the table's `columnFilters` state |

Header indicators follow the editor text, as the issue #6 work defines.

### CLI

`logchef open` builds the Explorer URL from flags
(`cli/crates/logchef-cli/src/commands/open.rs:85`). It gets `--view <id or
name>`, which adds `view=<id>` to the URL. A number is an id. Any other value
is a name, resolved through the list route. If the name matches views in more
than one scope, the command exits non-zero and prints each match with its id
and scope.

A new read-only command, `logchef views list -t <team> -S <source>`, prints
id, name, scope, default mark and the parts that each view sets. `--output
json` prints the list response. `logchef-core` gets `list_saved_views` beside
`list_saved_queries` (`cli/crates/logchef-core/src/api/mod.rs:532`). The CLI
does not create, edit or delete views in this RFC.

### MCP

One new read tool, `list_saved_views`, with parameters `team_id` and
`source_id`. It returns the list response. Its entry in `toolScopes` is
`saved_views:read` (`internal/mcp/server.go:154`). An agent uses it to learn
which columns a team reads for a source, and to pass a view id to a person.
MCP has no write tool for views.

### Provisioning TOML

A source declared in provisioning can declare source views.

```toml
[[provisioning.sources]]
name = "nginx"

  [[provisioning.sources.views]]
  name = "Access"
  default = true
  display_mode = "table"
  time_range = "1h"
  filter = 'status >= 400'
  sort = [{ column = "timestamp", desc = true }]
  columns = [{ name = "timestamp", width = 190 }, { name = "status" }, { name = "request_uri", width = 520 }]
```

`config.ProvisionSource` (`internal/config/provisioning.go:51`) gets
`Views []ProvisionView`. `ValidateConfig` checks each view with
`core.ValidateSavedViewDefinition`, the function that the API uses, with the
descriptor for the source's declared `source_type`. So an invalid filter, or
a filter on a provider without LogchefQL, fails at startup and never becomes
a managed view. It also checks that names are unique per source and that at
most one view per source has `default = true`.

The reconciler handles source views inside the transaction that reconciles
sources (`internal/provisioning/reconciler.go:102`). It matches a view by
source and name and stores it with `managed = 1`. A changed definition
increases `revision`. An unmanaged view with the same name is adopted. With
`prune = true`, a managed view that the file no longer declares is deleted.
Without it, the reconciler logs the view and leaves it, as it does for
sources (`internal/provisioning/reconciler.go:199`). `ExportConfig` writes
source views back out.

The API answers 403 to an update, a delete or a default change of a managed
view. A managed default also reserves the source default, as "API" states. A
source whose file declares no default leaves the default to global admins.
Team views and personal views are never managed.

### Other surfaces

| Surface | Change |
|---|---|
| Ask Chef (`internal/askchef`) | Not affected |
| Dashboards and alerts | Not affected |
| Docs | A new page `docs/src/content/docs/features/saved-views.mdx`. Additions to `integration/cli.mdx`, `integration/mcp-server.mdx`, `getting-started/provisioning.md` and `features/service-tokens.mdx` |
| Metrics | Not affected. The routes appear in the HTTP metrics that exist. No new metric |
| `CHANGELOG.md` | One entry. The two token scopes are new, so a token with an explicit scope list needs them to read views |

## Access and security

| Action | Personal view | Team view | Source view |
|---|---|---|---|
| Read | The owner | A member of the team, through that team's route | Any caller who passes `requireTeamHasSource` for the source |
| Create, update, delete | The owner | A team admin, a team editor, or a global admin | A global admin |
| Set or clear the default | The owner | A team admin or a global admin | A global admin |

- The route middleware proves team membership and the team's link to the
  source (`internal/server/middleware.go:358`,
  `internal/server/middleware.go:431`). Core then checks the scope rules.
- A global admin passes the membership check without a membership row,
  except through an OAuth access token, and still needs the team's link to
  the source (`internal/core/access/access.go:166`). An OAuth token of an
  admin user therefore manages no source views.
- A global admin cannot read another user's personal view. Deleting the user
  deletes that user's personal views.
- A view grants no access to data. Every view route sits behind
  `requireTeamHasSource`, so a caller who loses a source loses its views at
  the same moment.
- A view's `filter` is a starting query that the user can clear. It is not a
  row restriction. RFC 0002 keeps row restriction in the log database.
- A restricted source in the sense of RFC 0002 is its own source with its own
  id, and views key on `source_id`. The views of the shared source, with
  their column names and filters, are not visible on the restricted source.
- A view holds no secrets and makes no outbound request.

## Limits and failure modes

| Bound | Value | When hit |
|---|---|---|
| Personal views per user and source | 50 | Create answers 409 with "You have 50 views for this source. Delete one first." |
| Team views per team and source | 50 | Create answers 409 with the same wording for the team |
| Source views per source | 50 | Create answers 409. Provisioning fails validation at startup |
| Name | 200 code points | 400 |
| Description | 2,000 code points | 400 |
| Columns per view | 200 | 400 |
| Column name | 512 bytes | 400 |
| Column width | 50 to 3,000 pixels | 400 |
| Sort entries | 3 | 400 |
| Filter | 8,192 bytes | 400 |
| Definition | 65,536 bytes | 400 |

- **Stale update.** Two users edit one team view. The second save answers
  409. The UI shows "This view changed. Reload it and apply your changes
  again." and offers "Save as new view".
- **Deleted view.** A view that is deleted while a user has it active stays
  on screen until the next list load. The Explorer then falls back through
  the rules in "Applying a view".
- **Several replicas on Postgres.** Views are plain rows, and no replica
  caches them. The revision compare makes an update safe across replicas. Two
  requests that set different defaults in one scope each clear and set in a
  transaction. The partial unique index lets one commit. Core maps the
  other's store error to 409. Core counts and inserts in one transaction, so concurrent
  creates can exceed a cap by the number of concurrent requests. The cap
  bounds growth. It is not a quota.
- **The list request fails.** The Explorer opens with no view and shows one
  toast. The query runs.

## Backend differences

A view is metadata. It never reaches the log database, so no capability flag
gates it.

| Part | ClickHouse | VictoriaLogs | Loki, if RFC 0011 lands |
|---|---|---|---|
| Columns, sort, display mode | Portable | Portable. Fields are sparse, so the missing-column rule applies more often | Portable, by the same rule |
| `filter` | LogchefQL | LogchefQL | Allowed only if the provider descriptor lists LogchefQL in `QueryLanguages` |
| `time_range` | Portable | Portable | Portable |

A view belongs to one source. Two sources with the same columns need two
views.

## Seams

- **Requires from RFC 0009.** Units P2 (audit package) and P3 (route
  coverage). This RFC follows that contract. Each mutating route gets a route
  name and an action constant in `internal/audit/actions.go`. A
  `SavedViewFields` function goes in `internal/audit/views.go`. Each handler
  gets one `auditSet` line. Provisioning calls the recorder directly. The
  ordering rule is under "Audit events".
- **Requires from RFC 0005.** Nothing. A view's `filter` accepts whatever
  `logchefql.Validate` accepts. If that comes to include aggregations, the
  result has other columns, and the missing-column rule handles it.
- **Requires from RFC 0011.** Nothing. A Loki provider that supports LogchefQL
  filters lists it in its descriptor's `QueryLanguages`.
- **RFC 0008, future integration.** RFC 0008 defers reports on saved views
  and requires a server-side way to run a view. This RFC does not design one,
  and reading a view does not satisfy that requirement. A later design has
  three facts to work with. A personal view is deleted with its user. A
  view's `sort` orders loaded rows in the browser and is not an `ORDER BY`. A
  view can have no filter and no time range.
- **Issue #6.** Depends on the query-writing header filters of branch
  `feat/table-column-filters`. See "Column filters".
- **RFC 0003 decision 7 and RFC 0002.** No conflict with either. Decision 7
  names dashboards and saved queries, and open question 3 asks whether views
  should join them. "Access and security" covers RFC 0002.

## Audit events

| Event | Actor | Target |
|---|---|---|
| `saved_view.create` | The caller | The view, with `source_id`, `scope` and `team_id` |
| `saved_view.update` | The caller | The view, with the old and new `revision` |
| `saved_view.delete` | The caller | The view |
| `saved_view.set_default` | The caller | The view, with `scope` and the id of the view that lost the flag |
| `saved_view.clear_default` | The caller | The view, with `scope` |
| `saved_view.provision` | The system | The view, with the action `create`, `update`, `adopt` or `prune` |

`SavedViewFields` records the name, scope, source, team, revision, default
flag, column count, display mode and time range. For the filter it records
the language and a SHA-256 digest, and never the text.

**Ordering.** RFC 0009's route test rejects a mutating route with no name. If
P3 has landed when V2 is built, V2 includes the audit lines and V12 is only
the provisioning call. If V2 lands first, V12 adds everything after P3.

## Alternatives rejected

| Alternative | Why it loses |
|---|---|
| Add a `view` block to `SavedQueryContent` | A saved query cannot be private, cannot be empty, and replaces the editor text when it opens. See "A saved view is a new stored object" |
| Store views in user preferences | A preference cannot be shared with a team. The preference object has no bound and no foreign keys |
| Put views in collections, with pin and managed links | It needs a third link table, the pin and managed rules, and a second place to look for a view. `team_id` on the row gives team sharing |
| A separate table that maps a scope owner and source to a default view | A flag on the row with a partial unique index gives one default per scope with no second table. The browser's remembered choice covers "use this team view here" |
| Reference the view in a share link | A share must open for any recipient with access to the source. A personal view is not readable by the recipient, and a deleted view would break old links |
| Validate column names against the schema on save | The check needs a call to the log database on every save. VictoriaLogs has no fixed column list. A view must keep working after a column is dropped |
| Keep unsaved view changes in memory only | A member who cannot edit a team view would lose a resized column on every reload |
| Apply the default filter whenever a view is picked | Picking a view to change the columns would delete the query that the user typed |

## Implementation plan

Units V1 to V4 are the smallest slice that a user can use. They give
personal, team and source views with a picker. V3 can be built in parallel with V1 and V2. V5 to
V11 can be built in parallel with each other once their dependencies land.

| Id | Title | Depends on | Parallel with |
|---|---|---|---|
| V1 | Store and model for saved views | none | V3 |
| V2 | API, permissions and token scopes | V1 | V3 |
| V3 | Table state takes a view seed | none | V1, V2 |
| V4 | View picker and the `view` URL parameter | V2, V3 | none |
| V5 | Query defaults and default view resolution | V4, issue #6 branch | V6 to V11 |
| V6 | Manage views dialog and missing columns | V4 | V5, V7 to V11 |
| V7 | Layout snapshot in share links | V3, V4 | V5, V6, V8 to V11 |
| V8 | CLI `open --view` and `views list` | V2 | V4 to V7, V9 to V11 |
| V9 | MCP tool `list_saved_views` | V2 | V4 to V8, V10, V11 |
| V10 | Source views in provisioning TOML | V2 | V4 to V9, V11 |
| V11 | Docs page and changelog | V4 | V5 to V10 |
| V12 | Audit events | V2, V10, RFC 0009 P2 and P3 | any |

### V1. Store and model for saved views

- Creates `pkg/models/saved_view.go` and its test, the migration pair in
  `internal/store/sqlite/migrations/` and `internal/store/postgres/migrations/`,
  the store methods in both store packages, and
  `internal/store/storetest/saved_views.go`.
- Edits both `queries.sql` files, `internal/store/store.go` and
  `internal/store/storetest/storetest.go`. Regenerates `internal/store/*/sqlc/`.
- Change: the table, its indexes, `SavedViewDefinition.Validate`,
  `ParseSavedViewDefinition` and `SavedViewStore`.
- Check: `just check` passes. The conformance test runs on both stores. A
  second default in one scope fails. A duplicate name fails in one scope and
  succeeds in another. A stale revision changes no row. Deleting the source,
  the team or the user deletes the right views. The model test has one case
  per row of the definition rule table, and one for an unknown key.

### V2. API, permissions and token scopes

- Creates `internal/core/saved_views.go`, `internal/server/saved_view_handlers.go`
  and a test file for each.
- Edits `internal/server/server.go`, `pkg/models/auth.go`,
  `internal/core/api_tokens.go`, `internal/oauth/scopes.go` and its tests,
  `frontend/src/lib/tokenScopes.ts` and the locale files for the scope labels.
- Change: seven routes, two scopes, the permission function,
  `ValidateSavedViewDefinition`, the cap check, the default swap and the
  error mapping. `saved_views:read` goes at the end of OAuth's `ReadScopes`
  with a consent description (`internal/oauth/scopes.go:13`). The order of
  the existing entries does not change, because token encoding is positional.
  If RFC 0009 P3 has landed, the audit lines go in this unit.
- Check: a handler test with one case per cell of the access table. A member
  cannot update a team view (403). A team editor updates it and cannot set
  the default (403). Another member's personal view, another team's view on
  the same source and a view of another source answer 404. Create with
  `is_default` by a team editor answers 403. Setting a default that would
  clear a managed default answers 403 and changes no row. A stale revision
  answers 409. A token without `saved_views:read` answers 403. An OAuth
  client can request `saved_views:read`, and existing tokens still decode.

### V3. Table state takes a view seed

- Creates `frontend/src/lib/savedViewLayout.ts` and its vitest file.
- Edits `frontend/src/composables/useColumnState.ts`, its test file and
  `frontend/src/views/explore/table/data-table.vue`.
- Change: `useColumnState` takes the storage key and an optional seed with a
  view id and revision. `data-table.vue` takes the seed as a prop, seeds
  `sorting` from it and emits the working layout. With no seed, behavior is
  unchanged.
- Check: vitest for the seed mapping in both directions, a seed with absent
  columns, a sort on an absent column, and a stored working copy with another
  revision. A seed leaves the table's `columnFilters` state alone. The
  existing `useColumnState` tests pass unchanged.

### V4. View picker and the `view` URL parameter

- Creates `frontend/src/api/savedViews.ts`, `frontend/src/stores/savedViews.ts`
  and `frontend/src/views/explore/components/ViewPicker.vue`, with vitest
  files for the store and the picker.
- Edits `frontend/src/lib/exploreLocation.ts`,
  `frontend/src/composables/useExplorerNavigator.ts`,
  `frontend/src/stores/explore.ts`, `ResultsToolbar.vue`, `LogExplorer.vue`
  and the 11 locale files.
- Change: the picker with pick, no view, save as new, update and reset. The
  `view` parameter, the remembered choice, the display mode from the working
  copy, and the toast for a view that the caller cannot read.
- Check: vitest for `parseExploreLocation` and `sameExploreLocation` with
  `view`. UI scenario: set five columns and a width, save as a personal view,
  sign in from a private window, pick the view, and see the same columns and
  width. Open the copied URL as a second user and see the toast and a running
  query.

### V5. Query defaults and default view resolution

- Edits `frontend/src/stores/explore.ts`, `frontend/src/stores/savedViews.ts`,
  `frontend/src/composables/useExplorerNavigator.ts`, `ViewPicker.vue` and
  their tests.
- Change: the fill rules for `filter` and `time_range`, the use of
  `default_view_id`, the two checkboxes in the save dialog and the default
  mark.
- Check: vitest for each row of the fill table, with a URL that has `q`, a
  stored draft and an empty editor. UI scenario: a team admin sets a team
  default with a filter and a 1 hour range. A member with no remembered
  choice sees the view, the filter and the range. The member adds a header
  condition, picks another view, and the condition stays in the editor.
  "Reset to view" restores the saved conditions.

### V6. Manage views dialog and missing columns

- Creates `frontend/src/views/explore/components/ManageViewsDialog.vue` and
  its vitest file. Edits `ViewPicker.vue`, `savedViewLayout.ts` and the
  locale files.
- Change: the list dialog with rename, duplicate, delete, set default and
  remove missing columns. The warning mark and its tooltip.
- Check: vitest that the dialog hides edit actions when `can_edit` is false
  or `managed` is true. UI scenario: save a view, drop one of its columns
  from the dev ClickHouse table, run a query, and see the other columns in
  place and the mark with the column name.

### V7. Layout snapshot in share links

- Edits `pkg/models/query.go`, `internal/server/query_share_handlers.go` and
  its test, `frontend/src/stores/explore.ts`, `frontend/src/api/explore.ts`
  and `ViewPicker.vue`.
- Change: the optional `layout` field, its validation, the in-memory "Shared
  layout" and step 2 of the layout resolution.
- Check: a handler test that a valid `layout` round-trips, that a `layout`
  with `filter` answers 400, and that a share without `layout` still opens.
  A query near the payload limit plus a layout answers 400. Vitest for the
  resolution order with a remembered view, with a default, with `share` and
  `view` together, and with a share that has no layout. UI scenario: a second
  user with a remembered view opens a share with a custom layout, sees it,
  picks "No view" and sees their own layout unchanged.

### V8. CLI `open --view` and `views list`

- Creates `cli/crates/logchef-cli/src/commands/views.rs`.
- Edits `open.rs`, `mod.rs` and `cli.rs` in `cli/crates/logchef-cli/src/`,
  `cli/crates/logchef-core/src/api/mod.rs`,
  `cli/crates/logchef-cli/tests/open.rs` and
  `docs/src/content/docs/integration/cli.mdx`.
- Change: the flag, the name lookup and the list command.
- Check: `just check-cli` passes. `logchef open --view 12 --print` prints a
  URL with `view=12`. A name resolves to its id. A name in two scopes exits
  non-zero and lists both.

### V9. MCP tool `list_saved_views`

- Edits `internal/mcp/logs.go`, `internal/mcp/server.go`,
  `internal/mcp/mcp_test.go` and
  `docs/src/content/docs/integration/mcp-server.mdx`.
- Change: the tool, its result type and its `toolScopes` entry.
- Check: the tool is hidden without `saved_views:read`. It returns the
  caller's personal, team and source views and no other user's personal view.
  An MCP client with an OAuth token that holds the scope lists and calls it.

### V10. Source views in provisioning TOML

- Edits `internal/config/provisioning.go`, `validate.go`, `reconciler.go` and
  `export.go` in `internal/provisioning/` with their tests,
  `internal/core/saved_views.go` and
  `docs/src/content/docs/getting-started/provisioning.md`.
- Change: `ProvisionView`, validation, reconcile, adopt, prune, export, and
  the 403 for a change to a managed view.
- Check: reconciler tests on both stores. A declared view is created with
  `managed` set. A second run keeps the revision. A changed definition
  increases it. Prune deletes an undeclared managed view and keeps an
  unmanaged one. Two defaults for one source fail validation. An invalid
  filter fails validation, and so does a filter on a source type without
  LogchefQL.

### V11. Docs page and changelog

- Creates `docs/src/content/docs/features/saved-views.mdx`. Edits
  `docs/src/content/docs/features/service-tokens.mdx` and `CHANGELOG.md`.
- Change: a how-to for saving, sharing and defaults, a reference table of
  the definition fields, and the note on the two token scopes.
- Check: the docs site builds, and every route, flag and field on the page
  exists in the code at that commit.

### V12. Audit events

- Edits `internal/audit/actions.go`, `internal/audit/views.go`,
  `internal/server/server.go`, `internal/server/saved_view_handlers.go`,
  `internal/provisioning/reconciler.go` and their tests.
- Change: the contract under "Seams". Route names, action constants,
  `SavedViewFields`, one `auditSet` line per handler, and a direct recorder
  call in the reconciler. Only the reconciler call if V2 already did the rest.
- Check: RFC 0009's route test passes. A handler test per event asserts the
  action, the actor and the target, and that no field holds the filter text.

## Open questions for Karan

1. **May a team editor create and edit team views?** Default: yes. Only a
   team admin or a global admin sets the team default.
2. **Do source views earn their place?** Only they reach every team, and only
   they can be provisioned. Default: keep them, managed by global admins.
3. **Should a view also be linkable into a collection?** RFC 0003 decision 7
   names dashboards and saved queries only. Default: no.
4. **Should a share link carry the layout?** Default: yes, as unit V7.
5. **Should provisioning also declare team views?** Default: no. Source views
   only, so the reconciler needs no rule for a view that a team edits.
6. **Should picking a view replace a typed query with the view's filter?**
   Default: no. The filter fills an empty editor. "Reset to view" forces it.
7. **Should a new default move users who already picked a view?** Default:
   no. The browser's remembered choice wins.

## Unverified claims

1. **A partial unique index gives one default per scope on both stores, and
   the `CHECK` constraint with `IS NULL` behaves the same on both.**
   Experiment: the V1 conformance test inserts a second default and one
   invalid row per scope, and expects a failure on SQLite and on Postgres.
2. **Two concurrent default changes on Postgres end with one commit and one
   unique violation, not a deadlock.** Experiment: run two transactions that
   each clear and set a different default for one team and source, commit
   both, and record the error of the second.
3. **Seeding column order, visibility and sizing in one tick does not flash
   the automatic layout first.** Experiment: the V4 UI scenario, recorded,
   with a view of five columns on a source with 30.
4. **The compact list needs no change for `columns` and `sort`.** It reads
   `props.columns` (`frontend/src/views/explore/table/CompactLogListSimple.vue:74`),
   and this RFC did not trace what it does with them. Experiment: apply a
   view in compact mode and compare the rendered fields with no view.
5. **A share payload with the largest valid layout stays under the payload
   limit when the query is small.** The default limit is 1 MiB
   (`internal/config/config.go:492`), and a definition is at most 64 KiB.
   Experiment: the V7 handler tests.
6. **The issue #6 branch lands with header filters that write LogchefQL.**
   Only its query helpers existed when the reviewer read it. Experiment:
   read the merged branch before V5 starts.
