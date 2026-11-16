#!/usr/bin/env bash
# Screenshots of the gallery dashboards for the docs, in PNG, WebP and AVIF.
# Run provision.ts first. SHOTS=api-health,builder limits the run to those shots.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:5173}"
LOGCHEF_URL="${LOGCHEF_URL:-http://localhost:8125}"
LOGCHEF_TOKEN="${LOGCHEF_TOKEN:-logchef_1_devsetuptoken00000000000000}"
EMAIL="${EMAIL:-admin@logchef.internal}"
PASSWORD="${PASSWORD:-password}"
SESSION="${SESSION:-logchef-gallery}"
OUT="${OUT:-$(cd "$(dirname "$0")/../.." && pwd)/docs/public/screenshots}"
SHOTS="${SHOTS:-api-health,api-health-week,business,worker-logs,builder-chart,builder-stat,tabs-edit,stat-thresholds}"
WIDTH=1440
SCALE=2
# The browser inherits the zone; the timezone preference is "local".
export TZ=Asia/Kolkata

for tool in agent-browser curl jq cwebp avifenc; do
  command -v "$tool" >/dev/null || {
    echo "missing $tool. Run: nix shell --inputs-from . nixpkgs#libwebp nixpkgs#libavif --command $0" >&2
    exit 1
  }
done

ab() { agent-browser --session "$SESSION" "$@"; }
wanted() { [[ ",$SHOTS," == *",$1,"* ]]; }
# ref PATTERN: the @ref of the first interactive element whose snapshot line matches.
ref() { ab snapshot -i | grep -m1 -E "$1" | grep -oE 'ref=e[0-9]+' | sed 's/ref=/@/'; }

dashboard_id() {
  curl -fsS -H "Authorization: Bearer $LOGCHEF_TOKEN" -H "X-Logchef-Dashboard-Schema: 2" \
    "$LOGCHEF_URL/api/v1/dashboards" | jq -er --arg name "$1" '[.data[] | select(.name == $name)][0].id' ||
    { echo "dashboard \"$1\" not found: run bun dev/gallery/provision.ts" >&2; exit 1; }
}

login() {
  ab set viewport "$WIDTH" 900 "$SCALE" >/dev/null
  ab open "$BASE_URL/dashboards" >/dev/null
  # The app redirects to Dex, or offers an SSO button first, or is signed in.
  local url i
  for ((i = 0; i < 30; i++)); do
    url=$(ab get url)
    if [[ "$url" == *"/dex/"* ]] && ab snapshot -i | grep -q 'textbox "email address"'; then
      ab fill "$(ref 'textbox "email address"')" "$EMAIL" >/dev/null
      ab fill "$(ref 'textbox "Password"')" "$PASSWORD" >/dev/null
      ab click "$(ref 'button "Login"')" >/dev/null
      ab wait --fn "!location.pathname.startsWith('/dex/')" >/dev/null
      break
    elif [[ "$url" == "$BASE_URL"* ]] && ab snapshot -i | grep -q 'Sign in with SSO'; then
      ab find role button click --name "Sign in with SSO" >/dev/null
    elif [[ "$url" == "$BASE_URL/dashboards"* ]] && ab snapshot -i | grep -q 'New dashboard'; then
      break
    fi
    sleep 1
  done
  # Theme follows the emulated color scheme; times use the browser zone.
  ab open "$BASE_URL/dashboards" >/dev/null
  ab wait --text "New dashboard" >/dev/null
  ab eval 'fetch("/api/v1/me/preferences", {method: "PUT", headers: {"Content-Type": "application/json"}, body: JSON.stringify({theme: "auto", timezone: "local"})}).then(r => { if (!r.ok) throw new Error("preferences " + r.status); localStorage.setItem("vueuse-color-scheme", "auto"); return r.status })' >/dev/null
}

wait_panels() {
  ab wait --fn "document.querySelectorAll('.dash-panel').length > 0 && !document.querySelector('.dash-panel .animate-pulse')" >/dev/null
  sleep 1.5
}

# open_dashboard ID [QUERY]: load a dashboard with the viewport as tall as its content.
open_dashboard() {
  ab set viewport "$WIDTH" 900 "$SCALE" >/dev/null
  ab open "$BASE_URL/dashboards/$1${2:+?$2}" >/dev/null
  wait_panels
  local height
  height=$(ab eval "Math.ceil(document.querySelector('.overflow-y-auto').scrollHeight)")
  ab set viewport "$WIDTH" "$height" "$SCALE" >/dev/null
  sleep 1
}

encode() {
  local png="$1" base="${1%.png}"
  cwebp -quiet -q 82 -m 6 "$png" -o "$base.webp"
  avifenc --jobs all --speed 4 -q 60 "$png" "$base.avif" >/dev/null
  echo "wrote $base.{png,webp,avif}"
}

# shoot NAME THEME [SELECTOR]: the viewport, or one element, to OUT/dashboard-NAME-THEME.png.
shoot() {
  local png="$OUT/dashboard-$1-$2.png"
  ab screenshot ${3:+"$3"} "$png" >/dev/null
  encode "$png"
}

# both NAME ID [QUERY]: the whole dashboard in light and dark.
both() {
  for theme in light dark; do
    ab set media "$theme" >/dev/null
    open_dashboard "$2" "${3:-}"
    shoot "$1" "$theme"
  done
}

mkdir -p "$OUT"
login
api=$(dashboard_id "API health")
business=$(dashboard_id "Business KPIs")
workers=$(dashboard_id "Worker logs")

wanted api-health && both api-health "$api" "tab=day"
wanted api-health-week && both api-health-week "$api" "tab=week"
wanted business && both business "$business"
wanted worker-logs && both worker-logs "$workers"

# builder NAME PANEL HEIGHT [LABEL]: the panel builder for PANEL, its options
# scrolled so LABEL is at the top.
builder() {
  ab set media light >/dev/null
  ab set viewport "$WIDTH" "$3" "$SCALE" >/dev/null
  ab open "$BASE_URL/dashboards/$api?tab=day" >/dev/null
  wait_panels
  ab find role button click --name "Edit" >/dev/null
  ab wait --fn "document.querySelector('.dash-item .dash-panel') && !document.querySelector('.dash-item .animate-pulse')" >/dev/null
  ab eval "[...document.querySelectorAll('.dash-item')].find(i => i.querySelector('.dash-item__title')?.textContent === '$2').querySelector('.dash-item__btn').click(); 1" >/dev/null
  ab wait --fn "document.querySelector('.monaco-editor .view-lines')?.textContent.includes('quantile')" >/dev/null
  ab eval 'document.activeElement.blur(); 1' >/dev/null
  if [[ -n "${4:-}" ]]; then
    ab eval "const box = document.querySelector('[role=dialog] .overflow-y-auto[class~=\"pl-0.5\"]'); const label = [...box.querySelectorAll('*')].find(e => e.children.length === 0 && e.textContent.trim() === '$4'); box.scrollTop += label.getBoundingClientRect().top - box.getBoundingClientRect().top - 8; 1" >/dev/null
  fi
  sleep 2
  shoot "$1" light
}

wanted builder-chart && builder builder-chart "Latency" 1300
wanted builder-stat && builder builder-stat "p95 latency, last hour" 1000 "Value column"

if wanted tabs-edit; then
  ab set media light >/dev/null
  ab set viewport "$WIDTH" 900 "$SCALE" >/dev/null
  ab open "$BASE_URL/dashboards/$api?tab=week" >/dev/null
  wait_panels
  ab find role button click --name "Edit" >/dev/null
  ab wait --fn "document.querySelector('.dash-item .dash-panel') && !document.querySelector('.dash-item .animate-pulse')" >/dev/null
  sleep 1
  ab set viewport "$WIDTH" 560 "$SCALE" >/dev/null
  sleep 1
  shoot tabs-edit light
fi

if wanted stat-thresholds; then
  ab set media dark >/dev/null
  open_dashboard "$business"
  shoot stat-thresholds dark '.dash-cell:nth-child(4)'
fi

[[ -n "${KEEP_OPEN:-}" ]] || ab close >/dev/null
