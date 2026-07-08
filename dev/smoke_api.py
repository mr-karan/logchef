#!/usr/bin/env python3
"""Merge-bar smoke suite: ClickHouse regression + VictoriaLogs e2e via the HTTP API."""
import json, sys, urllib.request, urllib.error, urllib.parse
from datetime import datetime, timedelta, timezone
NOW = datetime.now(timezone.utc)
TR = {"start_time": (NOW - timedelta(hours=2)).strftime("%Y-%m-%d %H:%M:%S"), "end_time": (NOW + timedelta(minutes=5)).strftime("%Y-%m-%d %H:%M:%S")}
TR_RFC = {"start_time": (NOW - timedelta(hours=2)).strftime("%Y-%m-%dT%H:%M:%SZ"), "end_time": (NOW + timedelta(minutes=5)).strftime("%Y-%m-%dT%H:%M:%SZ")}

BASE = "http://localhost:8125/api/v1"
TOKEN = "logchef_1_devsetuptoken00000000000000"
passed, failed = [], []

def call(method, path, body=None, expect=200):
    req = urllib.request.Request(BASE + path, method=method,
        headers={"Authorization": f"Bearer {TOKEN}", "Content-Type": "application/json"},
        data=json.dumps(body).encode() if body is not None else None)
    try:
        resp = urllib.request.urlopen(req)
        code, payload = resp.status, json.loads(resp.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        code, payload = e.code, json.loads(e.read().decode() or "{}")
    return code, payload

def check(name, cond, detail=""):
    (passed if cond else failed).append(name)
    print(("PASS  " if cond else "FAIL  ") + name + (f"  — {detail}" if detail and not cond else ""))

# ── discover team + CH source ─────────────────────────────
code, me_teams = call("GET", "/me/teams")
check("me/teams reachable", code == 200, str(me_teams)[:200])
team_id = me_teams["data"][0]["id"]

code, sources = call("GET", f"/teams/{team_id}/sources")
check("team sources list", code == 200 and len(sources["data"]) >= 1, str(sources)[:200])
ch = next(s for s in sources["data"] if s["source_type"] == "clickhouse")
ch_id = ch["id"]

# #49: no credential leak anywhere in the source payloads
leak = "sekret" in json.dumps(sources) or '"password"' in json.dumps(sources)
check("no password in source list (#49)", not leak, json.dumps(sources)[:300])

# ── ClickHouse regression ─────────────────────────────────
code, r = call("POST", f"/teams/{team_id}/sources/{ch_id}/logs/query",
               {"query_text": "SELECT * FROM default.http ORDER BY timestamp DESC LIMIT 5", "limit": 5})
check("CH native SQL query", code == 200 and len(r["data"]["data"]) == 5, str(r)[:300])

code, r = call("POST", f"/teams/{team_id}/sources/{ch_id}/logchefql/query",
               {"query": 'status=500', "limit": 5, **TR})
check("CH LogchefQL query", code == 200 and "logs" in r.get("data", {}), str(r)[:300])
gen = (r.get("data") or {}).get("generated_query") or (r.get("data") or {}).get("generated_sql") or ""
check("CH LogchefQL generated SQL", "SELECT" in gen and "500" in gen, gen[:200])

code, r = call("POST", f"/teams/{team_id}/sources/{ch_id}/logs/histogram",
               {"query_text": "SELECT * FROM default.http WHERE status = 500", "window": "5m", **TR_RFC})
check("CH histogram", code == 200 and "data" in r["data"], str(r)[:300])

code, r = call("GET", f"/teams/{team_id}/sources/{ch_id}/schema")
check("CH schema", code == 200, str(r)[:200])

# log context (restored capability)
code, r = call("POST", f"/teams/{team_id}/sources/{ch_id}/logs/query",
               {"query_text": "SELECT toUnixTimestamp64Milli(timestamp) AS ts_ms FROM default.http ORDER BY timestamp DESC LIMIT 1", "limit": 1})
ts_ms = int(r["data"]["data"][0]["ts_ms"])
code, r = call("POST", f"/teams/{team_id}/sources/{ch_id}/logs/context",
               {"timestamp": ts_ms, "before_limit": 3, "after_limit": 3})
check("CH log context (restored #45)", code == 200 and "before_logs" in r["data"], str(r)[:300])

# saved query round-trip
code, r = call("POST", "/saved-queries", {
    "name": "smoke-ch", "source_id": ch_id, "query_language": "clickhefql" if False else "logchefql",
    "editor_mode": "builder",
    "query_content": json.dumps({"version": 1, "sourceId": ch_id, "timeRange": {"relative": "1h", "absolute": {"start": 0, "end": 0}}, "limit": 100, "content": 'status=500', "variables": []}),
})
check("CH saved query create", code in (200, 201), str(r)[:300])
if code in (200, 201):
    qid = r["data"]["id"]
    code, r = call("GET", f"/saved-queries/{qid}")
    ok = code == 200 and r["data"]["query_language"] == "logchefql" and r["data"]["editor_mode"] == "builder"
    check("CH saved query round-trip (language/mode)", ok, str(r)[:300])
    call("DELETE", f"/saved-queries/{qid}")

# CH alert test endpoint
code, r = call("POST", "/alerts/test", {
    "source_id": ch_id,
    "query_language": "clickhouse-sql", "editor_mode": "native",
    "query": "SELECT count(*) as value FROM default.http WHERE status = 500 AND `timestamp` >= now() - toIntervalSecond(300)",
    "lookback_seconds": 300, "threshold_operator": "gt", "threshold_value": 0,
})
check("CH alert test", code == 200 and "value" in r.get("data", {}), str(r)[:300])

# ── VictoriaLogs e2e ──────────────────────────────────────
# idempotency: drop any leftover smoke source
_, existing = call("GET", "/admin/sources")
for src in (existing.get("data") or []):
    if src.get("name") == "VL Smoke":
        call("DELETE", f"/admin/sources/{src['id']}")
code, r = call("POST", "/admin/sources", {
    "name": "VL Smoke", "source_type": "victorialogs",
    "meta_ts_field": "_time", "meta_severity_field": "level",
    "connection": {"base_url": "http://localhost:9428", "auth": {"mode": "none"}, "tenant": {"account_id": "0", "project_id": "0"}},
})
check("VL source create", code in (200, 201), str(r)[:400])
vl_id = r["data"]["id"] if code in (200, 201) else None

if vl_id:
    code, r = call("POST", f"/teams/{team_id}/sources", {"source_id": vl_id})
    check("VL source linked to team", code in (200, 201), str(r)[:200])

    code, r = call("GET", f"/teams/{team_id}/sources/{vl_id}")
    d = r.get("data", {})
    check("VL capabilities exposed", "logsql" in json.dumps(d.get("query_languages", [])), str(d)[:300])

    code, r = call("POST", f"/teams/{team_id}/sources/{vl_id}/logs/query",
                   {"query_text": 'level:="error"', "limit": 5})
    check("VL native LogsQL query", code == 200 and len(r["data"].get("data", [])) > 0, str(r)[:300])

    code, r = call("POST", f"/teams/{team_id}/sources/{vl_id}/logchefql/query",
                   {"query": 'level="error"', "limit": 5, **TR})
    check("VL LogchefQL→LogsQL query", code == 200 and "logs" in r.get("data", {}), str(r)[:300])
    gen = (r.get("data") or {}).get("generated_query", "")
    check("VL generated LogsQL", "level" in gen and "error" in gen, gen[:200])

    code, r = call("POST", f"/teams/{team_id}/sources/{vl_id}/logs/histogram",
                   {"query_text": 'level:="error"', "window": "5m", **TR_RFC})
    check("VL histogram", code == 200 and "data" in r.get("data", {}), str(r)[:300])

    code, r = call("GET", f"/teams/{team_id}/sources/{vl_id}/schema")
    check("VL schema/field discovery", code == 200, str(r)[:200])

    qs = urllib.parse.urlencode({"type": "string", "limit": 5, **TR_RFC})
    code, r = call("GET", f"/teams/{team_id}/sources/{vl_id}/fields/level/values?" + qs)
    check("VL field values", code == 200, str(r)[:250])

    # context must be a clean unsupported error, not a 500
    code, r = call("POST", f"/teams/{team_id}/sources/{vl_id}/logs/context",
                   {"timestamp": 1, "before_limit": 1, "after_limit": 1})
    check("VL log context rejected cleanly", code == 400, f"code={code} {str(r)[:200]}")

    # saved LogsQL query round-trip
    code, r = call("POST", "/saved-queries", {
        "name": "smoke-vl", "source_id": vl_id, "query_language": "logsql", "editor_mode": "native",
        "query_content": json.dumps({"version": 1, "sourceId": vl_id, "timeRange": {"relative": "1h", "absolute": {"start": 0, "end": 0}}, "limit": 100, "content": 'level:="error"', "variables": []}),
    })
    check("VL saved query create", code in (200, 201), str(r)[:300])
    if code in (200, 201):
        qid = r["data"]["id"]
        code, r = call("GET", f"/saved-queries/{qid}")
        ok = code == 200 and r["data"]["query_language"] == "logsql"
        check("VL saved query round-trip", ok, str(r)[:200])
        call("DELETE", f"/saved-queries/{qid}")

    # builder-mode saved query must be REJECTED for logsql (contract validation)
    code, r = call("POST", "/saved-queries", {
        "name": "smoke-vl-bad", "source_id": vl_id, "query_language": "logsql", "editor_mode": "builder",
        "query_content": "{}",
    })
    check("VL invalid language/mode combo rejected", code == 400, f"code={code} {str(r)[:200]}")

    # VL alert test (native LogsQL stats)
    code, r = call("POST", "/alerts/test", {
        "source_id": vl_id,
        "query_language": "logsql", "editor_mode": "native",
        "query": 'level:="error" | stats count() as value',
        "lookback_seconds": 300, "threshold_operator": "gt", "threshold_value": 0,
    })
    check("VL alert test (stats_query)", code == 200 and "value" in r.get("data", {}), str(r)[:300])

    # VL source update with blank credentials keeps working (inherit path)
    code, r = call("PUT", f"/admin/sources/{vl_id}", {
        "description": "updated by smoke",
        "connection": {"base_url": "http://localhost:9428", "auth": {"mode": "none"}, "tenant": {"account_id": "0", "project_id": "0"}},
    })
    check("VL source update", code == 200, str(r)[:300])

    call("DELETE", f"/admin/sources/{vl_id}")

# ── Dashboards CRUD (#56/#73) ─────────────────────────────
# Dashboards are a top-level resource (not team-scoped in the path); the panel
# blob carries the team/source per panel. Idempotent: drop leftovers by name.
DASH_NAME = "smoke-dashboard"
_, existing = call("GET", "/dashboards")
for d in (existing.get("data") or []):
    if d.get("name") == DASH_NAME:
        call("DELETE", f"/dashboards/{d['id']}")

def panel(pid, ptype="timeseries", title="p"):
    return {"id": pid, "title": title, "type": ptype, "team_id": team_id,
            "source_id": ch_id, "query": "status>=500", "query_language": "logchefql", "options": {}}

def layout(pid, y=0, w=6, h=2):
    return {"id": pid, "x": 0, "y": y, "w": w, "h": h}

# create (valid)
code, r = call("POST", "/dashboards", {
    "name": DASH_NAME, "description": "created by smoke",
    "panels": {"version": 1, "layout": [layout("p1")], "panels": [panel("p1", "timeseries", "5xx over time")]},
}, expect=201)
check("dashboard create", code in (200, 201) and "id" in r.get("data", {}), str(r)[:300])
dash_id = r["data"]["id"] if code in (200, 201) else None

if dash_id:
    # list includes it, with can_edit hint
    code, r = call("GET", "/dashboards")
    names = [d["name"] for d in (r.get("data") or [])]
    check("dashboard list includes created", code == 200 and DASH_NAME in names, str(names)[:200])

    # get round-trips the panel blob
    code, r = call("GET", f"/dashboards/{dash_id}")
    d = r.get("data", {})
    ok = code == 200 and d.get("panels", {}).get("version") == 1 and len(d["panels"]["panels"]) == 1
    check("dashboard get round-trip", ok, str(r)[:300])

    # update: rename + add a second (stat) panel
    code, r = call("PUT", f"/dashboards/{dash_id}", {
        "name": DASH_NAME, "description": "updated by smoke",
        "panels": {"version": 1,
                   "layout": [layout("p1"), layout("p2", y=2, w=3, h=1)],
                   "panels": [panel("p1", "timeseries", "5xx over time"), panel("p2", "stat", "5xx count")]},
    })
    ok = code == 200 and len(r.get("data", {}).get("panels", {}).get("panels", [])) == 2
    check("dashboard update (rename + add panel)", ok, str(r)[:300])

    # invalid: 25 panels exceeds the max of 24 → 400
    many_panels = [panel(f"p{i}") for i in range(25)]
    many_layout = [layout(f"p{i}", y=i, w=3, h=1) for i in range(25)]
    code, r = call("PUT", f"/dashboards/{dash_id}", {
        "name": DASH_NAME, "description": "too many",
        "panels": {"version": 1, "layout": many_layout, "panels": many_panels},
    })
    check("dashboard 25-panel blob rejected (400)", code == 400, f"code={code} {str(r)[:200]}")

    # invalid: bad panel type → 400
    code, r = call("POST", "/dashboards", {
        "name": "smoke-dashboard-bad", "description": "bad type",
        "panels": {"version": 1, "layout": [layout("p1")],
                   "panels": [{**panel("p1"), "type": "piechart"}]},
    })
    check("dashboard invalid panel type rejected (400)", code == 400, f"code={code} {str(r)[:200]}")

    # delete, then confirm it's gone (404)
    code, r = call("DELETE", f"/dashboards/{dash_id}")
    check("dashboard delete", code == 200, str(r)[:200])
    code, r = call("GET", f"/dashboards/{dash_id}")
    check("dashboard get after delete (404)", code == 404, f"code={code} {str(r)[:200]}")

# 404 for a never-existent id
code, r = call("GET", "/dashboards/99999999")
check("dashboard get missing id (404)", code == 404, f"code={code} {str(r)[:200]}")

print(f"\n══ {len(passed)} passed, {len(failed)} failed")
if failed:
    print("failed:", failed)
    sys.exit(1)
