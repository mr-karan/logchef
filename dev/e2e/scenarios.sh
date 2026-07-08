#!/usr/bin/env bash
# scenarios.sh — the logchef e2e scenario library. Each scenario is a function
# `scn_<name>` that runs assertions via the helpers in lib.sh. Add a scenario by
# writing a function here and appending its name to SCENARIOS in run.sh.
#
# Scenarios assume lib.sh is sourced and that login() has run (run.sh handles
# ordering). They re-snapshot on every interaction, so they are order-tolerant
# and safe to run repeatedly against either backend.

# Authenticated and on the explorer.
scn_login() {
  assert_control "logged in (Run control present)" 'button "Run'
  assert_control "team selector present" 'combobox'
  shot login
}

# A team + source can be selected and a source is bound.
scn_sources() {
  select_team_source "Dev Team" "http"
  assert_control "source selected" 'combobox.*(default\.http|http)'
}

# Running the default query returns rows from ClickHouse.
scn_query() {
  set_wide_time_range   # data-age-robust: don't depend on ingest recency
  run_query
  assert_present "query returned log rows" 'HTTP/1\.1|logchef\.dev|GET|POST'
  assert_present "results table rendered" 'timestamp'
  shot query
}

# The filterable-fields sidebar is populated with distinct values + counts
# (exercises the bounded-concurrency field-values fan-out end to end).
scn_field_values() {
  assert_present "field values: host distincts" 'logchef\.dev'
  assert_present "field values: method distincts" '\b(GET|POST|PUT|DELETE)\b'
}

# The histogram toggle renders without error.
scn_histogram() {
  if click_by 'button "Histogram"'; then
    settle 1
    assert_control "histogram control present after toggle" 'Histogram'
    if snap | grep -qiE 'failed|could not|error loading'; then
      fail "histogram: error text present"
    else
      pass "histogram: no error text"
    fi
    shot histogram
  else
    fail "histogram control not found"
  fi
}

# The Collections menu opens.
scn_collections() {
  if click_by 'button "Collections'; then
    settle 1
    assert_control "collections menu opened" 'menuitem|option|New Collection|Save'
    ab press Escape >/dev/null 2>&1
  else
    fail "collections control not found"
  fi
}

# The time-range picker: the popover renders, a quick range applies and drives
# a query, and an absolute range entered as text resolves + applies + queries.
# Exercises the reka-ui + @internationalized/date date stack end to end — a
# duplicated copy of that lib silently breaks its calendar/parse types and can
# regress the picker, so this guards the whole path in the browser.
scn_time_range() {
  # Open the picker (label is either a "Last …" quick range or an absolute
  # "YYYY-MM-DD …" window depending on prior state).
  local tr; tr="$(ref 'button "Last |button "20[0-9]{2}-[0-9]{2}-[0-9]{2}')"
  [ -n "$tr" ] || { fail "time range: picker trigger not found"; return; }
  ab click "$tr" >/dev/null
  # A broken date lib renders an empty popover; assert its contents appear.
  wait_for 'button "Last 1 hour"' 8 || { fail "time range: popover did not render"; return; }
  assert_control "picker: quick ranges rendered" 'button "Last 24 hours"'
  assert_control "picker: absolute inputs rendered" 'textbox "now-1h'
  shot timerange-open

  # Quick range → button label updates and the query returns rows.
  click_by 'button "Last 1 hour"'; settle 2
  assert_control "quick range applied (Last 1h)" 'button "Last 1h"'
  assert_present "quick-range query returned rows" 'logchef\.dev|HTTP/1\.1'

  # Absolute range entered as text (now-2h/now, so it stays date-independent):
  # resolves to a timestamp window, applies, and drives a query.
  tr="$(ref 'button "Last 1h"')"
  [ -n "$tr" ] && ab click "$tr" >/dev/null && wait_for 'textbox "now-1h' 8
  local fr to; fr="$(ref 'textbox "now-1h')"; to="$(ref 'textbox "now or')"
  if [ -n "$fr" ] && [ -n "$to" ]; then
    ab fill "$fr" "now-2h" >/dev/null
    ab fill "$to" "now" >/dev/null
    click_by 'button "Apply time range"'; settle 2
    assert_control "absolute range applied (timestamp label)" 'button "20[0-9]{2}-[0-9]{2}-[0-9]{2}'
    assert_present "absolute-range query returned rows" 'logchef\.dev|HTTP/1\.1'
    shot timerange-absolute
  else
    fail "time range: absolute inputs not found"
  fi
}

# Live tail (SSE): enable Live on the ClickHouse source, ingest a fresh row via
# clickhouse-client, and assert it streams into the tail view within ~10s; then
# toggle off. Repeats on the VictoriaLogs source when one is linked. Self-seeding
# and re-runnable. The tail cursor starts at connection time, so fixtures are
# ingested AFTER the stream opens (a row inserted earlier would not replay).
#
# Requires the ClickHouse dev container reachable via `docker exec`; skips (with
# a note) if it isn't. clickhouse-client prints a harmless DNS warning about its
# own hostname to stderr on this image — stderr is discarded; the INSERT lands.
CH_CONTAINER="${CH_CONTAINER:-dev-clickhouse-local-1}"
VICTORIALOGS_URL="${VICTORIALOGS_URL:-http://localhost:9428}"

# ch_insert SQL — run an INSERT inside the ClickHouse dev container. Returns
# non-zero if the container isn't reachable.
ch_insert() {
  docker exec "$CH_CONTAINER" clickhouse-client --query "$1" >/dev/null 2>&1
}

# assert_tail NAME REGEX [tries] — like assert_present but with a longer poll
# window (default ~14 tries ≈ 14s) to cover the ClickHouse tail poll interval.
assert_tail() {
  local out i tries="${3:-14}"
  for ((i = 0; i < tries; i++)); do
    out="$(snap)"
    if grep -qiE "$2" <<<"$out"; then pass "$1"; return; fi
    sleep 1
  done
  fail "$1 (not streamed: $2)"
}

# go_live — ensure LogchefQL (Search) mode, then click the Live toggle. Returns
# non-zero if the toggle isn't available/armable.
go_live() {
  click_by 'tab "Search"' >/dev/null 2>&1  # LogchefQL mode arms the toggle
  settle 1
  click_by 'button "Live tail"' || return 1
  settle 1
  return 0
}

scn_livetail() {
  if ! docker ps >/dev/null 2>&1 || ! docker inspect "$CH_CONTAINER" >/dev/null 2>&1; then
    pass "livetail: ClickHouse dev container not reachable — scenario skipped"
    return
  fi

  # --- ClickHouse source ---
  select_team_source "Dev Team" "http"
  assert_control "livetail: ClickHouse source selected" 'combobox.*(default\.http|http)'

  if ! go_live; then
    fail "livetail: Live toggle not available on ClickHouse source"
    return
  fi
  assert_control "livetail: Stop control present (live armed)" 'button "Stop"'

  # Ingest a fresh row AFTER the stream is open, with a unique marker.
  local marker="/livetail-e2e-$$"
  if ! ch_insert "INSERT INTO default.http VALUES (now(),'api.logchef.dev','GET','HTTP/1.1','-','${marker}',200,'admin',7)"; then
    fail "livetail: ClickHouse INSERT failed"
    click_by 'button "Stop"' >/dev/null 2>&1
    return
  fi
  assert_tail "livetail: ingested row streamed into tail view" "$marker"
  shot livetail-clickhouse

  # Toggle off — the Run button returns and the tail view is gone.
  click_by 'button "Stop"' >/dev/null 2>&1
  settle 1
  assert_control "livetail: Run control restored after stop" 'button "Run'

  # --- VictoriaLogs source (only if one is linked) ---
  select_team_source "Dev Team" "VictoriaLogs"
  if ! snapi | grep -qiE 'combobox.*VictoriaLogs'; then
    pass "livetail: no VictoriaLogs source linked — VL leg skipped"
    select_team_source "Dev Team" "http"
    settle 1
    return
  fi
  assert_control "livetail: VictoriaLogs source selected" 'combobox.*VictoriaLogs'

  if ! go_live; then
    fail "livetail: Live toggle not available on VictoriaLogs source"
    select_team_source "Dev Team" "http"
    return
  fi

  # Ingest a fresh jsonline row after the VL tail stream opens.
  local vl_marker="livetail-e2e-vl-$$"
  local now_ts; now_ts="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  if curl --fail --silent --show-error -X POST \
      -H 'Content-Type: application/stream+json' \
      --data-binary "{\"timestamp\":\"${now_ts}\",\"service\":\"livetail-e2e\",\"level\":\"info\",\"message\":\"${vl_marker}\"}" \
      "${VICTORIALOGS_URL}/insert/jsonline?_stream_fields=service&_time_field=timestamp&_msg_field=message" >/dev/null 2>&1; then
    assert_tail "livetail: VictoriaLogs row streamed into tail view" "$vl_marker"
    shot livetail-victorialogs
  else
    fail "livetail: VictoriaLogs ingest failed (is VictoriaLogs on :9428?)"
  fi

  click_by 'button "Stop"' >/dev/null 2>&1
  settle 1
  # hand the explorer back to the ClickHouse source for any following scenario
  select_team_source "Dev Team" "http"
  settle 1
}

# Dashboards (#56 / #73). Route taken: API-created dashboard + browser-verified
# render and edit-mode persistence.
#
# Panels are built through the editor sheet's reka-ui Select pickers + Monaco
# query editor, which are fiddly to drive reliably by a11y ref, so this scenario
# creates the dashboard via the HTTP API (the sanctioned fallback in #73) and
# then exercises the real UI: navigate via the sidebar Dashboards entry, assert
# the dashboard lists + both panels render data (not an empty/locked/error
# state), then rename a panel through the browser editor sheet, save, reload,
# and assert the rename persisted. Self-seeding (fresh CH + VL fixtures in a 15m
# window, matching the dashboard's default range) and re-runnable (leftover
# dashboards with the same name are deleted first). Cleans up at the end.
DASH_API="${DASH_API:-http://localhost:8125/api/v1}"
DASH_TOKEN="${DASH_TOKEN:-logchef_1_devsetuptoken00000000000000}"
DASH_NAME="e2e-dashboards"
DASH_CH_TITLE="e2e 5xx timeseries"
DASH_VL_TITLE="e2e error count"
DASH_RENAMED="e2e 5xx renamed"

dash_api() { # METHOD PATH [BODY]
  local method="$1" path="$2" body="${3:-}"
  if [ -n "$body" ]; then
    curl -sS -X "$method" -H "Authorization: Bearer $DASH_TOKEN" \
      -H "Content-Type: application/json" -d "$body" "$DASH_API$path"
  else
    curl -sS -X "$method" -H "Authorization: Bearer $DASH_TOKEN" "$DASH_API$path"
  fi
}

scn_dashboards() {
  # --- Seed fresh fixtures in the default 15m window ---
  if ! docker ps >/dev/null 2>&1 || ! docker inspect "$CH_CONTAINER" >/dev/null 2>&1; then
    pass "dashboards: ClickHouse dev container not reachable — scenario skipped"
    return
  fi
  # A handful of 5xx rows at now() so the timeseries panel has buckets.
  if ! ch_insert "INSERT INTO default.http (timestamp,host,method,protocol,referer,request,status,\`user-identifier\`,bytes) SELECT now(),'api.logchef.dev','GET','HTTP/1.1','-','/dash-e2e',500,'admin',toUInt32(number) FROM numbers(8)"; then
    fail "dashboards: ClickHouse fixture INSERT failed"
    return
  fi

  # Discover a VictoriaLogs source linked to team 1 (source 8 is usual on dev).
  local vl_id
  vl_id="$(dash_api GET /teams/1/sources | python3 -c 'import sys,json;d=json.load(sys.stdin)["data"];print(next((s["id"] for s in d if s["source_type"]=="victorialogs"),""))' 2>/dev/null)"
  if [ -n "$vl_id" ]; then
    # Fresh error rows so the VL stat panel counts > 0.
    local now_ts; now_ts="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
    local i
    for i in 1 2 3; do
      curl -sS -X POST -H 'Content-Type: application/stream+json' \
        --data-binary "{\"timestamp\":\"${now_ts}\",\"service\":\"dash-e2e\",\"level\":\"error\",\"message\":\"dash-e2e error ${i}\"}" \
        "${VICTORIALOGS_URL}/insert/jsonline?_stream_fields=service&_time_field=timestamp&_msg_field=message" >/dev/null 2>&1
    done
  fi

  # --- Idempotency: delete any leftover dashboard by name ---
  local leftover
  leftover="$(dash_api GET /dashboards | python3 -c "import sys,json;d=json.load(sys.stdin)['data'];print(' '.join(str(x['id']) for x in d if x['name']=='$DASH_NAME'))" 2>/dev/null)"
  for id in $leftover; do dash_api DELETE "/dashboards/$id" >/dev/null; done

  # --- Create the dashboard via API: CH timeseries + (optional) VL stat ---
  local panels layout body
  panels="{\"id\":\"p1\",\"title\":\"$DASH_CH_TITLE\",\"type\":\"timeseries\",\"team_id\":1,\"source_id\":1,\"query\":\"status>=500\",\"query_language\":\"logchefql\",\"options\":{}}"
  layout="{\"id\":\"p1\",\"x\":0,\"y\":0,\"w\":6,\"h\":3}"
  if [ -n "$vl_id" ]; then
    panels="$panels,{\"id\":\"p2\",\"title\":\"$DASH_VL_TITLE\",\"type\":\"stat\",\"team_id\":1,\"source_id\":$vl_id,\"query\":\"level=\\\"error\\\"\",\"query_language\":\"logchefql\",\"options\":{}}"
    layout="$layout,{\"id\":\"p2\",\"x\":6,\"y\":0,\"w\":3,\"h\":3}"
  fi
  body="{\"name\":\"$DASH_NAME\",\"description\":\"e2e dashboards scenario\",\"panels\":{\"version\":1,\"layout\":[$layout],\"panels\":[$panels]}}"

  local dash_id
  dash_id="$(dash_api POST /dashboards "$body" | python3 -c 'import sys,json;print(json.load(sys.stdin)["data"]["id"])' 2>/dev/null)"
  if [ -z "$dash_id" ]; then
    fail "dashboards: API create failed"
    return
  fi
  pass "dashboards: created via API (id=$dash_id${vl_id:+, VL stat on source $vl_id})"

  # --- Navigate via the sidebar Dashboards entry, assert it lists ---
  # The sidebar collapses to icons-only (nav links lose their text label); expand
  # it so the "Dashboards" link is addressable by its accessible name.
  if [ -z "$(ref 'link "Dashboards"')" ]; then
    click_by 'button "Toggle Sidebar"'; settle 1
  fi
  if click_by 'link "Dashboards"'; then
    settle 1
  else
    ab open "$BASE_URL/dashboards" >/dev/null; settle 1
    fail "dashboards: sidebar Dashboards entry not found (opened by URL instead)"
  fi
  wait_for "New dashboard" 10 || true
  assert_present "dashboards: created dashboard appears in list" "$DASH_NAME"
  shot dashboards-list

  # --- Open the dashboard view (cards are non-interactive divs; open by URL) ---
  ab open "$BASE_URL/dashboards/$dash_id" >/dev/null
  settle 1
  wait_for 'button "Edit"' 15 || true

  # Panel chrome renders (titles from the panel headers).
  assert_present "dashboards: CH timeseries panel rendered" "$DASH_CH_TITLE"
  if [ -n "$vl_id" ]; then
    assert_present "dashboards: VL stat panel rendered" "$DASH_VL_TITLE"
  else
    pass "dashboards: no VictoriaLogs source linked — VL stat panel skipped"
  fi

  # Panels rendered data (not an empty/locked/error state). Poll a few seconds
  # for the async panel XHRs to resolve, then confirm no failure text is present.
  local out i ok=0
  for ((i = 0; i < 10; i++)); do
    out="$(snap)"
    if ! grep -qiE 'No data for this time range|No access to this source|Failed to load' <<<"$out"; then
      ok=1; break
    fi
    sleep 1
  done
  if [ "$ok" -eq 1 ]; then
    pass "dashboards: panels rendered data (no empty/locked/error state)"
  else
    fail "dashboards: a panel is in an empty/locked/error state"
  fi
  shot dashboards-view

  # --- Edit-mode persistence: rename a panel through the browser editor sheet ---
  if ! click_by 'button "Edit"'; then
    fail "dashboards: Edit button not available (creator/admin edit)"
    dash_api DELETE "/dashboards/$dash_id" >/dev/null
    return
  fi
  settle 1
  assert_control "dashboards: edit mode entered (Add panel present)" 'button "Add panel"'

  # The per-panel controls are hover-revealed but present in the DOM; open the
  # editor sheet for the first panel (the CH timeseries) via its "Edit panel"
  # control. "Update panel" is unique to the edit-existing sheet, so wait on it
  # (not "Add panel", which is always present in the edit-mode header).
  local renamed=0
  if click_by 'button "Edit panel"'; then
    if wait_for 'button "Update panel"' 12; then
      settle 1   # let the side sheet finish its slide-in before typing
      local tbox j
      # The Title <input> is filled through Playwright, but the sheet animation +
      # Monaco init can race a single fill; retry until the value sticks.
      for ((j = 0; j < 4; j++)); do
        tbox="$(ref 'textbox "Title"')"
        [ -n "$tbox" ] || { sleep 1; continue; }
        ab fill "$tbox" "$DASH_RENAMED" >/dev/null
        sleep 1
        snapi | grep -qiE "textbox \"Title\".*${DASH_RENAMED}" && break
      done
      if [ -n "$tbox" ] && snapi | grep -qiE "textbox \"Title\".*${DASH_RENAMED}"; then
        click_by 'button "Update panel"'; settle 1
        assert_present "dashboards: renamed panel shown in grid (edit mode)" "$DASH_RENAMED"
        renamed=1
      else
        fail "dashboards: panel Title input not editable in editor sheet"
      fi
    else
      fail "dashboards: panel editor sheet did not open"
    fi
  else
    fail "dashboards: 'Edit panel' control not found in edit mode"
  fi

  # Save (wait until the Save button is enabled — it stays disabled until the
  # draft is dirty), reload from the server, and assert the rename persisted.
  if [ "$renamed" -eq 1 ]; then
    if wait_for 'button "Save" \[ref' 8 && click_by 'button "Save" \[ref'; then
      settle 2
      ab open "$BASE_URL/dashboards/$dash_id" >/dev/null
      settle 1
      wait_for 'button "Edit"' 15 || true
      assert_present "dashboards: renamed panel persisted after save + reload" "$DASH_RENAMED"
      shot dashboards-renamed
    else
      fail "dashboards: Save button never enabled / not clickable"
    fi
  fi

  # --- Cleanup ---
  dash_api DELETE "/dashboards/$dash_id" >/dev/null
  ab open "$BASE_URL/logs/explore" >/dev/null; settle 1
}

# The admin users page lists the seeded admin user.
scn_admin_users() {
  ab open "$BASE_URL/admin/users" >/dev/null; settle 1
  assert_present "admin user listed" "$EMAIL"
  shot admin-users
  ab open "$BASE_URL/logs/explore" >/dev/null; settle 1
}

# VictoriaLogs source: select it, run a LogsQL-backed query, and verify field
# discovery — the multi-datasource path end to end. Self-seeding: ingests the
# stable fixture set via dev/ingest-victorialogs.sh (idempotent, ~4 rows) so
# the assertions don't depend on ad-hoc data. Skips (passes with a note) when
# no VictoriaLogs source is linked to the team.
scn_victorialogs() {
  if ! "$(dirname "${BASH_SOURCE[0]}")/../ingest-victorialogs.sh" >/dev/null 2>&1; then
    fail "victorialogs: fixture ingest failed (is VictoriaLogs running on :9428?)"
    return
  fi
  # open the source picker to see whether a VL source exists at all
  select_team_source "Dev Team" "VictoriaLogs"
  if ! snapi | grep -qiE 'combobox.*VictoriaLogs'; then
    pass "victorialogs: no VictoriaLogs source linked — scenario skipped"
    return
  fi
  assert_control "VictoriaLogs source selected" 'combobox.*VictoriaLogs'
  # Keep the window NARROW (15m): the fixtures are ingested moments ago. VL
  # explore results now sort newest-first for LogchefQL-translated and pipe-free
  # raw queries (the provider appends `| sort by (_time desc)`), but a wide
  # window on a data-rich instance could still render unrelated recent rows
  # ahead of the fixtures. In a 15m window the fixtures are the only matches on
  # dev/CI stacks, so this stays the right isolation.
  set_recent_time_range
  run_query
  assert_present "VictoriaLogs query returned fixture rows" 'payments worker boot completed|retrying gateway request|gateway request failed|billing cycle finished'
  assert_present "VictoriaLogs field discovery (sidebar)" 'button "(level|service)'
  shot victorialogs
  # hand the explorer back to the ClickHouse source for any following scenario
  select_team_source "Dev Team" "http"
  settle 1
}
