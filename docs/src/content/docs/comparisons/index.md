---
title: Comparisons
description: Honest comparisons between Logchef and other log analytics and observability tools, covering architecture, features, and when to choose each.
---

Logchef is a query and control-plane UI over ClickHouse or VictoriaLogs data
you already have — it doesn't collect logs or own a schema. That makes it a
good fit for some teams and the wrong tool for others. These pages compare
Logchef against adjacent projects on architecture and features, and try to
be direct about where another tool is the better choice.

- [Logchef vs ClickStack](/comparisons/logchef-vs-clickstack/) — how Logchef's
  schema-agnostic log UI compares to ClickStack (HyperDX), ClickHouse's
  end-to-end observability stack covering logs, traces, metrics, and session
  replay.
- [Logchef vs Grafana Loki](/comparisons/logchef-vs-grafana-loki/) — how
  Logchef's ClickHouse/VictoriaLogs-backed search compares to Loki's
  label-indexed, object-storage logging, why high-cardinality fields behave
  differently, and how Logchef + VictoriaLogs is itself a Loki alternative.
