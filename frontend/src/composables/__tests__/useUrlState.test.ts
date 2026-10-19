import { describe, expect, it } from "vitest";

import { normalizeQueryParams } from "../useUrlState";

describe("normalizeQueryParams", () => {
  it("keeps an empty q or sql, which names an empty query", () => {
    expect(normalizeQueryParams({ source: "10", mode: "logchefql", q: "" })).toEqual({ source: "10", mode: "logchefql", q: "" });
    expect(normalizeQueryParams({ mode: "native", sql: "" })).toEqual({ mode: "native", sql: "" });
  });

  it("drops other empty params and leaves q and sql out when absent", () => {
    expect(normalizeQueryParams({ team: "", source: "10", limit: "" })).toEqual({ source: "10" });
  });
});
