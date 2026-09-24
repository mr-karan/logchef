#!/usr/bin/env python3
"""Ingest wide-schema fixtures into the dev VictoriaLogs for issue #106 benchmarks.

env="perf":    6000 lines, 4 services, 20 shared + 60 service-specific fields
               (267 field names including _time, _msg and _stream fields).
env="perf-xl": 3000 lines, 10 services, 40 fields per line drawn from a
               300-field slice per service (3000 distinct attribute names).

VictoriaLogs drops blocks with more than 2000 unique field names. A block holds
one stream, so each perf-xl service draws from its own slice of the pool to
keep every block far below that limit while the source schema stays wide.

Usage: python3 dev/ingest-wide-fixture.py [victorialogs-url]
"""
import json
import random
import sys
import urllib.request
from datetime import datetime, timedelta, timezone

URL = (sys.argv[1] if len(sys.argv) > 1 else "http://localhost:9428").rstrip("/")
INSERT = f"{URL}/insert/jsonline?_stream_fields=service,env&_time_field=timestamp&_msg_field=message"
LEVELS = ["info", "warn", "error", "debug"]
NOW = datetime.now(timezone.utc)


def perf_lines():
    services = ["wide-a", "wide-b", "wide-c", "wide-d"]
    for i in range(6000):
        service = services[i % len(services)]
        record = {
            "timestamp": (NOW - timedelta(seconds=i * 0.5)).isoformat(),
            "service": service,
            "env": "perf",
            "level": random.choice(LEVELS),
            "message": f"request {i} handled by {service}",
        }
        for field in range(20):
            record[f"shared_{field:02d}"] = f"s{random.randint(0, 50)}"
        prefix = service.replace("-", "_")
        for field in range(60):
            record[f"{prefix}_f{field:02d}"] = f"v{random.randint(0, 500)}"
        yield record


def perf_xl_lines():
    services, fields_per_service = 10, 300
    for i in range(3000):
        service = i % services
        pool = range(service * fields_per_service, (service + 1) * fields_per_service)
        record = {
            "timestamp": (NOW - timedelta(seconds=i)).isoformat(),
            "service": f"xl-{service}",
            "env": "perf-xl",
            "level": random.choice(LEVELS),
            "message": f"xl event {i}",
        }
        for field in random.sample(pool, 40):
            record[f"attr_{field:04d}"] = f"v{random.randint(0, 200)}"
        yield record


def ingest(name, lines):
    encoded = [json.dumps(line) for line in lines]
    body = "\n".join(encoded).encode()
    # A form content type makes VictoriaLogs parse the body as a form and drop lines.
    request = urllib.request.Request(INSERT, data=body, method="POST",
                                     headers={"Content-Type": "application/stream+json"})
    with urllib.request.urlopen(request) as response:
        print(f"{name}: HTTP {response.status}, {len(encoded)} lines")


ingest('env="perf"', perf_lines())
ingest('env="perf-xl"', perf_xl_lines())
print('Add VictoriaLogs sources scoped with {env="perf"} and {env="perf-xl"} to explore them.')
