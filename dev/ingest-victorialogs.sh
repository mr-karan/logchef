#!/usr/bin/env bash
set -euo pipefail

VICTORIALOGS_URL="${VICTORIALOGS_URL:-http://localhost:9428}"

if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to ingest VictoriaLogs sample data" >&2
  exit 1
fi

now="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
five_minutes_ago="$(date -u -v-5M +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -d '5 minutes ago' +"%Y-%m-%dT%H:%M:%SZ")"
ten_minutes_ago="$(date -u -v-10M +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -d '10 minutes ago' +"%Y-%m-%dT%H:%M:%SZ")"

payload="$(
cat <<EOF
{"timestamp":"$ten_minutes_ago","service":"payments-api","env":"dev","level":"info","message":"payments worker boot completed","request_id":"req-001"}
{"timestamp":"$five_minutes_ago","service":"payments-api","env":"dev","level":"warn","message":"retrying gateway request","request_id":"req-002"}
{"timestamp":"$now","service":"payments-api","env":"dev","level":"error","message":"gateway request failed","request_id":"req-003"}
{"timestamp":"$now","service":"billing-worker","env":"dev","level":"info","message":"billing cycle finished","request_id":"req-004"}
EOF
)"

curl --fail --silent --show-error \
  -X POST \
  -H 'Content-Type: application/stream+json' \
  --data-binary "$payload" \
  "${VICTORIALOGS_URL}/insert/jsonline?_stream_fields=service,env&_time_field=timestamp&_msg_field=message" >/dev/null

echo "VictoriaLogs sample data ingested into ${VICTORIALOGS_URL}"
