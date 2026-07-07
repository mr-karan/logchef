#!/usr/bin/env bash
# run.sh — logchef agent-browser e2e runner.
#
# Drives the running logchef frontend through a set of user scenarios and
# reports pass/fail. Re-runnable against either metadata backend (the app is
# identical; only the store differs), so it doubles as a backend-parity check.
#
# Usage:
#   dev/e2e/run.sh                         # all scenarios, defaults
#   BACKEND=postgres dev/e2e/run.sh        # label the run (for screenshots/report)
#   BASE_URL=http://localhost:5173 EMAIL=admin@logchef.internal PASSWORD=password \
#     dev/e2e/run.sh login query           # run only named scenarios
#
# Requires: agent-browser (npm i -g agent-browser && agent-browser install),
# and a running logchef stack (see .claude/skills/logchef-dev).
#
# Exit code is non-zero if any scenario assertion fails (CI-friendly).

set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$HERE/lib.sh"
source "$HERE/scenarios.sh"

# Ordered scenario list. Add new ones here after defining scn_<name>.
ALL_SCENARIOS=(login sources query field_values time_range histogram collections victorialogs admin_users)

SELECTED=("$@")
[ "${#SELECTED[@]}" -eq 0 ] && SELECTED=("${ALL_SCENARIOS[@]}")

printf '\033[1m▶ logchef e2e  [backend=%s]  %s\033[0m\n' "$BACKEND" "$BASE_URL"

# Preflight: frontend reachable?
if ! curl -sSf -o /dev/null "$BASE_URL" 2>/dev/null; then
  echo "  ✗ frontend not reachable at $BASE_URL — is the stack up?" >&2
  exit 2
fi

login  # authenticate once; scenarios assume an authenticated session

for name in "${SELECTED[@]}"; do
  fn="scn_${name}"
  if ! declare -F "$fn" >/dev/null; then
    echo "  ? unknown scenario: $name (skipping)"; continue
  fi
  printf '\n  \033[1m• %s\033[0m\n' "$name"
  "$fn"
done

report
exit $?
