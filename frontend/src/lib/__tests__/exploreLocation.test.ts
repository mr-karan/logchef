import { describe, expect, it } from "vitest";
import { parseExploreLocation, sameExploreLocation } from "../exploreLocation";

describe("parseExploreLocation", () => {
  it("keeps an empty q or sql, which names an empty query", () => {
    expect(parseExploreLocation({ source: "10", mode: "logchefql", q: "" }).target).toMatchObject({ kind: "query", mode: "logchefql", text: "" });
    expect(parseExploreLocation({ mode: "native", sql: "" }).target).toMatchObject({ kind: "query", mode: "native", text: "" });
  });

  it("reads a URL with only team, source and empty params as the source's draft", () => {
    expect(parseExploreLocation({ team: "", source: "10", limit: "" })).toEqual({ teamId: null, sourceId: 10, viewId: null, target: { kind: "draft" } });
  });

  it("reads a saved query id and a share token before any query params", () => {
    expect(parseExploreLocation({ team: "1", id: "99", q: "x" }).target).toEqual({ kind: "saved", queryId: 99 });
    expect(parseExploreLocation({ share: "tok", id: "99" }).target).toEqual({ kind: "share", token: "tok" });
  });

  it("reads a view beside every target", () => {
    expect(parseExploreLocation({ team: "2", source: "5", view: "12" })).toEqual({ teamId: 2, sourceId: 5, viewId: 12, target: { kind: "draft" } });
    expect(parseExploreLocation({ team: "2", source: "5", view: "12", q: "status>=500", t: "1h" })).toMatchObject({ viewId: 12, target: { kind: "query", text: "status>=500" } });
    expect(parseExploreLocation({ view: "12", id: "99" })).toMatchObject({ viewId: 12, target: { kind: "saved", queryId: 99 } });
    expect(parseExploreLocation({ view: "12", share: "tok" })).toMatchObject({ viewId: 12, target: { kind: "share", token: "tok" } });
    expect(parseExploreLocation({ source: "5", view: "" }).viewId).toBeNull();
  });
});

describe("sameExploreLocation", () => {
  it("ignores empty params and legacy names, not the query", () => {
    expect(sameExploreLocation({ source: "10", time: "1h", limit: "" }, { source: "10", t: "1h" })).toBe(true);
    expect(sameExploreLocation({ source: "10", q: "" }, { source: "10" })).toBe(false);
  });

  it("tells two views of one location apart", () => {
    expect(sameExploreLocation({ source: "10", view: "1" }, { source: "10", view: "2" })).toBe(false);
    expect(sameExploreLocation({ source: "10", view: "1" }, { source: "10" })).toBe(false);
    expect(sameExploreLocation({ source: "10", view: "1" }, { source: "10", view: "1" })).toBe(true);
  });
});
