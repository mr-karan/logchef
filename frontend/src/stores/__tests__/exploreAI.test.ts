import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { exploreApi } from "@/api/explore";
import { sourcesApi } from "@/api/sources";
import { useContextStore } from "../context";
import { useExploreAIStore } from "../exploreAI";
import { useSourcesStore } from "../sources";

// Real AI, context and sources stores. Only the source and generation
// endpoints are stubbed.
const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });

describe("useExploreAIStore", () => {
  beforeEach(async () => {
    setActivePinia(createPinia());
    vi.spyOn(sourcesApi, "getTeamSource").mockReturnValue(ok({ id: 3, name: "app-logs" }) as never);
    useContextStore().selectTeam(1);
    useContextStore().selectSource(3);
    await useSourcesStore().loadSourceDetails(3);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  function generate(response: { sql_query: string; language?: string }, mode: "logchefql" | "native") {
    vi.spyOn(exploreApi, "generateAISQL").mockReturnValue(ok(response) as never);
    return useExploreAIStore().generateAiSql("errors in the last hour", "", mode);
  }

  it("keeps a LogchefQL query in LogchefQL mode", async () => {
    await generate({ sql_query: 'level="error"', language: "logchefql" }, "logchefql");
    expect(useExploreAIStore().generatedAiQuery).toEqual({ query: 'level="error"', mode: "logchefql" });
  });

  it("keeps a native query in native mode", async () => {
    await generate({ sql_query: "level:error", language: "logsql" }, "native");
    expect(useExploreAIStore().generatedAiQuery).toEqual({ query: "level:error", mode: "native" });
  });

  it("trusts the language the server wrote over the mode asked for", async () => {
    await generate({ sql_query: "SELECT 1", language: "clickhouse-sql" }, "logchefql");
    expect(useExploreAIStore().generatedAiQuery).toMatchObject({ mode: "native" });
  });

  it("uses the mode asked for when the server names no language", async () => {
    await generate({ sql_query: 'level="error"' }, "logchefql");
    expect(useExploreAIStore().generatedAiQuery).toMatchObject({ mode: "logchefql" });
  });
});
