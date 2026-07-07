#!/usr/bin/env bash
# lib.sh — shared helpers for the logchef agent-browser e2e suite.
#
# These wrap the `agent-browser` CLI into a small, resilient testing library:
# snapshot-driven element lookup (refs are re-resolved on every call, so they
# never go stale), a Dex login flow, and pass/fail assertions with a tally.
#
# Config via env (see run.sh for defaults):
#   BASE_URL   frontend URL           (default http://localhost:5173)
#   EMAIL      OIDC login email       (default admin@logchef.internal)
#   PASSWORD   OIDC password          (default password)
#   BACKEND    label for the report   (e.g. sqlite / postgres)
#   ART_DIR    screenshot output dir

# NB: no `pipefail` — assertions grep -q the (large) snapshot, which closes the
# pipe early and SIGPIPEs agent-browser; under pipefail that would read as a
# failed match. The explicit assertions below are our error detection.
set -u

: "${BASE_URL:=http://localhost:5173}"
: "${EMAIL:=admin@logchef.internal}"
: "${PASSWORD:=password}"
: "${BACKEND:=unknown}"
: "${ART_DIR:=/tmp/logchef-e2e}"
mkdir -p "$ART_DIR"

PASSED=0
FAILED=0
FAILURES=()

# --- agent-browser wrappers --------------------------------------------------

ab() { agent-browser "$@" 2>&1; }

# Full accessibility snapshot (for content assertions).
snap() { agent-browser snapshot 2>/dev/null; }
# Interactive-only snapshot (for finding controls).
snapi() { agent-browser snapshot -i 2>/dev/null; }

settle() { agent-browser wait --load networkidle >/dev/null 2>&1; sleep "${1:-1}"; }

# wait_for PATTERN [tries] — poll the interactive snapshot until a control
# matching PATTERN appears (1s between tries). Handles async render after a
# fresh login / SPA route change, which `networkidle` alone doesn't cover.
wait_for() {
  local pat="$1" tries="${2:-20}" i
  for ((i = 0; i < tries; i++)); do
    [ -n "$(ref "$pat")" ] && return 0
    sleep 1
  done
  return 1
}

# ref PATTERN — print the first @eN whose interactive-snapshot line matches
# PATTERN (a grep -E regex, usually an accessible name). Empty if not found.
ref() {
  snapi | grep -m1 -iE "$1" | grep -oE 'ref=e[0-9]+' | head -1 | sed 's/ref=/@/'
}

# click_by PATTERN — resolve a ref for PATTERN and click it. Returns non-zero
# (and records nothing) if no element matched.
click_by() {
  local r; r="$(ref "$1")"
  [ -n "$r" ] || { return 1; }
  ab click "$r" >/dev/null
}

# --- assertions --------------------------------------------------------------

pass() { PASSED=$((PASSED + 1)); printf '    \033[32m✓\033[0m %s\n' "$1"; }
fail() { FAILED=$((FAILED + 1)); FAILURES+=("$1"); printf '    \033[31m✗\033[0m %s\n' "$1"; }

# assert_present NAME REGEX — pass if REGEX appears in the full snapshot. Polls
# for a few seconds so async SPA renders (query results, field values loading in
# after the XHR) aren't raced. Snapshot is captured to a var before grepping so
# grep -q closing the pipe can't SIGPIPE the producer.
assert_present() {
  local out i
  for ((i = 0; i < 8; i++)); do
    out="$(snap)"
    if grep -qiE "$2" <<<"$out"; then pass "$1"; return; fi
    sleep 1
  done
  fail "$1 (not found: $2)"
}

# assert_control NAME REGEX — pass if REGEX matches an interactive control (polls).
assert_control() {
  local out i
  for ((i = 0; i < 5; i++)); do
    out="$(snapi)"
    if grep -qiE "$2" <<<"$out"; then pass "$1"; return; fi
    sleep 1
  done
  fail "$1 (no control: $2)"
}

shot() { agent-browser screenshot "$ART_DIR/${BACKEND}-$1.png" >/dev/null 2>&1; }

# --- flows -------------------------------------------------------------------

# login — land on the explorer, authenticating through Dex if needed. Idempotent:
# a no-op when already authenticated (existing session/SSO cookie).
login() {
  ab open "$BASE_URL/logs/explore" >/dev/null
  settle 1
  if [ -n "$(ref 'Sign in with SSO')" ]; then
    click_by 'Sign in with SSO'; settle 1
  fi
  if [ -n "$(ref 'textbox "email address"')" ]; then
    ab fill "$(ref 'textbox "email address"')" "$EMAIL" >/dev/null
    ab fill "$(ref 'textbox "Password"')" "$PASSWORD" >/dev/null
    click_by 'button "Login"'; settle 2
  fi
  # Ensure we end up on a fully-rendered explorer (fresh logins render async):
  # wait for Run AND for a source to be bound (teams/sources finished loading),
  # then a beat to let the initial results settle before scenarios assert.
  ab open "$BASE_URL/logs/explore" >/dev/null
  settle 1
  wait_for 'button "Run' 20 || echo "  (warning: explorer 'Run' control did not appear)"
  wait_for 'combobox.*: *[A-Za-z0-9_]+\.[A-Za-z0-9_]+' 20 || echo "  (warning: no source bound)"
  sleep 2
}

# cbox_ref VALUE_REGEX — ref of the combobox whose displayed value matches
# VALUE_REGEX. The explorer has several bare "combobox" controls (team, source,
# refresh interval, grouping, page size) with no accessible name, so we key off
# the value shown after the colon.
cbox_ref() {
  snapi | grep -iE "combobox.*: *${1}" | grep -m1 -oE 'ref=e[0-9]+' | head -1 | sed 's/ref=/@/'
}

# select_team_source TEAM SOURCE_SUBSTR — pick a team then a source. The source
# combobox is the one whose value looks like db.table; the team combobox is the
# one whose value mentions a team (or "Select team"). No-ops when already set.
select_team_source() {
  local team="$1" src="$2"
  if ! snapi | grep -qiE "combobox.*: *${team}\b"; then
    local tr; tr="$(cbox_ref '([A-Za-z ]*Team|Select team)')"
    [ -n "$tr" ] && ab click "$tr" >/dev/null && sleep 1 && click_by "option \"${team}\"" && settle 1
  fi
  if ! snapi | grep -qiE "combobox.*: *[A-Za-z0-9_]*${src}"; then
    # Prefer the db.table-shaped value; fall back to any non-team combobox so
    # this still works when a non-ClickHouse source (no dot) is selected.
    local sr; sr="$(cbox_ref '[A-Za-z0-9_]+\.[A-Za-z0-9_]+')"  # db.table value
    if [ -z "$sr" ]; then
      sr="$(snapi | grep -iE 'combobox' | grep -viE ': *([A-Za-z ]*Team|Select team)' | grep -m1 -oE 'ref=e[0-9]+' | sed 's/ref=/@/')"
    fi
    [ -n "$sr" ] && ab click "$sr" >/dev/null && sleep 1 && click_by "option \"[^\"]*${src}" && settle 2
  fi
}

# set_wide_time_range — widen the explorer's time window so scenarios don't
# depend on how recently the sample data was ingested. No-op if already wide.
set_wide_time_range() {
  snapi | grep -qiE 'button "Last (24h|2d|7d|12h|6h|3h)"' && return 0
  local tr; tr="$(ref 'button "Last ')"
  [ -n "$tr" ] || return 1
  ab click "$tr" >/dev/null
  wait_for 'button "Last 24 hours"' 8 || return 1   # let the picker render
  click_by 'button "Last 24 hours"'
  settle 2
}

# run_query — run the query and wait for the results grid to actually populate
# (the query is an async XHR; networkidle alone can fire before React renders).
run_query() {
  click_by 'button "Run' || return 1
  settle 1
  wait_for 'button "Run' 15   # controls re-enable once the response is in
  sleep 2
}

report() {
  echo
  printf '  \033[1m[%s] %d passed, %d failed\033[0m\n' "$BACKEND" "$PASSED" "$FAILED"
  if [ "$FAILED" -gt 0 ]; then
    printf '  failures:\n'; printf '    - %s\n' "${FAILURES[@]}"
    return 1
  fi
  return 0
}
