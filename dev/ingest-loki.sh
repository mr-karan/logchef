#!/usr/bin/env bash
set -euo pipefail

LOKI_URL="${LOKI_URL:-http://localhost:3100}"
LOKI_TENANT="${LOKI_TENANT:-dev}"

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to ingest Loki sample data" >&2
  exit 1
fi

# Loki takes nanosecond timestamps. date +%N is not portable, so whole seconds
# are padded with zeros.
now_s="$(date -u +%s)"
ts() { echo "$((now_s - $1))000000000"; }

# Four streams (service_name, env, level), JSON lines, and trace_id as
# structured metadata.
payload="$(
cat <<JSON
{"streams": [
  {"stream": {"service_name": "payments-api", "env": "dev", "level": "info"},
   "values": [["$(ts 600)", "{\"message\":\"payments worker boot completed\",\"request_id\":\"req-001\",\"status\":200,\"duration_ms\":12}", {"trace_id": "trace-001"}]]},
  {"stream": {"service_name": "payments-api", "env": "dev", "level": "warn"},
   "values": [["$(ts 300)", "{\"message\":\"retrying gateway request\",\"request_id\":\"req-002\",\"status\":429,\"duration_ms\":250}", {"trace_id": "trace-002"}]]},
  {"stream": {"service_name": "payments-api", "env": "dev", "level": "error"},
   "values": [["$(ts 1)", "{\"message\":\"gateway request failed\",\"request_id\":\"req-003\",\"status\":502,\"duration_ms\":3000}", {"trace_id": "trace-003"}]]},
  {"stream": {"service_name": "billing-worker", "env": "dev", "level": "info"},
   "values": [["$(ts 1)", "{\"message\":\"billing cycle finished\",\"request_id\":\"req-004\",\"status\":200,\"duration_ms\":80}", {"trace_id": "trace-004"}]]}
]}
JSON
)"

curl --fail --silent --show-error \
  -X POST \
  -H 'Content-Type: application/json' \
  -H "X-Scope-OrgID: ${LOKI_TENANT}" \
  --data-binary "$payload" \
  "${LOKI_URL}/loki/api/v1/push" >/dev/null

echo "Loki sample data ingested into ${LOKI_URL} for tenant ${LOKI_TENANT}"
