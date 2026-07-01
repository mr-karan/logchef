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

# The admin users page lists the seeded admin user.
scn_admin_users() {
  ab open "$BASE_URL/admin/users" >/dev/null; settle 1
  assert_present "admin user listed" "$EMAIL"
  shot admin-users
  ab open "$BASE_URL/logs/explore" >/dev/null; settle 1
}
