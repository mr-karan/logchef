#!/usr/bin/env bash
# Deploy smoke test for the docs site.
#
# Run after a GitHub Pages deploy to verify the live site actually serves
# what it's supposed to: homepage 200 + expected copy, robots.txt, sitemap,
# canonical/OG tags, llms.txt, and a couple of representative docs pages.
#
# Retries with backoff to tolerate CDN propagation delay right after deploy.
#
# Usage:
#   ./smoke-test.sh                              # checks https://logchef.app
#   BASE_URL=https://example.com ./smoke-test.sh  # checks a different host

set -uo pipefail

BASE_URL="${BASE_URL:-https://logchef.app}"
BASE_URL="${BASE_URL%/}"

CURL_OPTS=(--silent --show-error --location --max-time 15 --retry 6 --retry-delay 5 --retry-all-errors)

fail=0
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

pass() { echo "PASS  $1" >&2; }
bad() { echo "FAIL  $1" >&2; fail=1; }

# fetch_status <path> -> writes body to $tmpdir/<n>, prints http status code
fetch() {
  local path="$1" out="$2"
  curl "${CURL_OPTS[@]}" -o "$out" -w '%{http_code}' "${BASE_URL}${path}"
}

# assert_status <path> <expected-code>
assert_status() {
  local path="$1" expected="$2" out="$tmpdir/body_$RANDOM"
  local code
  code="$(fetch "$path" "$out")"
  if [[ "$code" == "$expected" ]]; then
    pass "$path -> $code"
  else
    bad "$path expected HTTP $expected, got $code"
  fi
  echo "$out"
}

# assert_contains <body-file> <path (for messages)> <pattern...>
assert_contains() {
  local body="$1" path="$2"
  shift 2
  for pattern in "$@"; do
    if grep -qF "$pattern" "$body"; then
      pass "$path contains: $pattern"
    else
      bad "$path missing expected content: $pattern"
    fi
  done
}

echo "Smoke-testing ${BASE_URL}"
echo

# --- Homepage --------------------------------------------------------------
home_body="$(assert_status "/" 200)"
assert_contains "$home_body" "/" \
  "Logchef" \
  '<link rel="canonical"' \
  'property="og:title"' \
  'property="og:image"' \
  'name="description"'

if grep -oE '<link rel="canonical" href="[^"]*"' "$home_body" | grep -qF "${BASE_URL}/"; then
  pass "/ canonical points back at ${BASE_URL}"
else
  bad "/ canonical does not point at ${BASE_URL} (check for host/scheme drift)"
fi

# --- robots.txt --------------------------------------------------------------
robots_body="$(assert_status "/robots.txt" 200)"
assert_contains "$robots_body" "/robots.txt" "Sitemap:"

# --- sitemap-index.xml -------------------------------------------------------
sitemap_body="$(assert_status "/sitemap-index.xml" 200)"
assert_contains "$sitemap_body" "/sitemap-index.xml" "<sitemapindex"

# --- llms.txt (AI-agent discovery file) --------------------------------------
llms_body="$(assert_status "/llms.txt" 200)"
assert_contains "$llms_body" "/llms.txt" "# Logchef"

# --- A couple of representative docs pages -----------------------------------
assert_status "/getting-started/quickstart/" 200 >/dev/null
assert_status "/integration/cli/" 200 >/dev/null

echo
if [[ "$fail" -ne 0 ]]; then
  echo "Smoke test FAILED."
  exit 1
fi
echo "Smoke test passed."
