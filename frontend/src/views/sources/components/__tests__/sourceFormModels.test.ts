import { describe, expect, it } from "vitest";
import type { Source } from "@/api/sources";
import {
  buildVictoriaLogsConnection,
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
