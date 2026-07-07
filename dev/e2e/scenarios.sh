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
  # Keep the window NARROW (15m): the fixtures are ingested moments ago, and VL
  # results are currently unsorted (see tracker: VL explore sort), so a wide
  # window on a data-rich instance may render other rows instead. In a 15m
  # window the fixtures are the only matches on dev/CI stacks.
  set_recent_time_range
  run_query
  assert_present "VictoriaLogs query returned fixture rows" 'payments worker boot completed|retrying gateway request|gateway request failed|billing cycle finished'
  assert_present "VictoriaLogs field discovery (sidebar)" 'button "(level|service)'
  shot victorialogs
  # hand the explorer back to the ClickHouse source for any following scenario
  select_team_source "Dev Team" "http"
  settle 1
}
