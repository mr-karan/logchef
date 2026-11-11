import { describe, expect, it } from "vitest";
import type { Source } from "@/api/sources";
import {
  buildClickHouseConnection,
  buildVictoriaLogsConnection,
  clickHouseFormStateFromSource,
  createDefaultClickHouseFormState,
  createDefaultVictoriaLogsFormState,
  victoriaLogsFormStateFromSource,
} from "@/views/sources/components/sourceFormModels";

function victoriaLogsSource(connection: Source["connection"]): Source {
  return {
    id: 1,
    name: "payments",
    _meta_is_auto_created: false,
    source_type: "victorialogs",
    _meta_ts_field: "_time",
    connection,
    ttl_days: 0,
    created_at: "",
    updated_at: "",
    is_connected: true,
  };
}

describe("VictoriaLogs windowed search settings", () => {
  it("defaults the schema lookback to five minutes", () => {
    const state = { ...createDefaultVictoriaLogsFormState(), baseURL: "https://logs.example.com", windowedEnabled: true };
    expect(buildVictoriaLogsConnection(state).optimizer?.schema_lookback_seconds).toBe(300);
  });

  it("round-trips the schema lookback through the form", () => {
    const state = { ...createDefaultVictoriaLogsFormState(), baseURL: "https://logs.example.com", windowedEnabled: true, schemaLookbackSeconds: "900" };
    const connection = buildVictoriaLogsConnection(state);
    expect(connection.optimizer?.schema_lookback_seconds).toBe(900);
    expect(victoriaLogsFormStateFromSource(victoriaLogsSource(connection)).schemaLookbackSeconds).toBe("900");
  });
});

function clickHouseSource(connection: Source["connection"]): Source {
  return {
    id: 2,
    name: "logs_view",
    _meta_is_auto_created: false,
    source_type: "clickhouse",
    _meta_ts_field: "timestamp",
    connection,
    ttl_days: 30,
    created_at: "",
    updated_at: "",
    is_connected: true,
  };
}

describe("ClickHouse view activity settings", () => {
  it("omits activity from the connection when view activity is disabled", () => {
    const state = {
      ...createDefaultClickHouseFormState(),
      host: "localhost:9000",
      database: "default",
      tableName: "logs_view",
      activity: { allowView: false, maxRowsToRead: "5000000", maxBytesToRead: "" },
    };
    expect(buildClickHouseConnection(state).activity).toBeUndefined();
  });

  it("round-trips view activity opt-in and scan limits through the form", () => {
    const state = {
      ...createDefaultClickHouseFormState(),
      host: "clickhouse.example.com:9440",
      tlsEnable: true,
      database: "default",
      tableName: "logs_view",
      activity: {
        allowView: true,
        maxRowsToRead: "10000000",
        maxBytesToRead: "536870912",
      },
    };
    const connection = buildClickHouseConnection(state);
    expect(connection.tls_enable).toBe(true);
    expect(connection.activity).toEqual({
      allow_view: true,
      max_rows_to_read: 10000000,
      max_bytes_to_read: 536870912,
    });

    const hydrated = clickHouseFormStateFromSource(clickHouseSource(connection));
    expect(hydrated.tlsEnable).toBe(true);
    expect(hydrated.activity).toEqual({
      allowView: true,
      maxRowsToRead: "10000000",
      maxBytesToRead: "536870912",
    });
  });
});
