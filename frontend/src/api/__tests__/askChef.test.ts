import { describe, expect, it } from "vitest";

import { AskChefError, createAskChefStreamParser, type AskChefEvent } from "../askChef";

const evidence = {
  id: "E1",
  tool: "compare_windows",
  team_id: 1,
  source_id: 3,
  mode: "logchefql",
  query: 'level="error"',
  start: 1000,
  end: 2000,
  summary: "23 errors vs 3",
};

function frame(type: string, data: unknown): string {
  return `event: ${type}\ndata: ${JSON.stringify(data)}\n\n`;
}

describe("createAskChefStreamParser", () => {
  it("emits an event only once a frame split across chunks is complete", () => {
    const parser = createAskChefStreamParser();
    const wire = frame("tool_end", { call_id: "c1", ok: true, summary: "23 vs 3", evidence: [evidence] });

    const events: AskChefEvent[] = [];
    for (const chunk of [wire.slice(0, 7), wire.slice(7, 40), wire.slice(40, -1)]) {
      events.push(...parser.push(chunk));
    }
    expect(events).toEqual([]);

    expect(parser.push(wire.slice(-1))).toEqual([
      { type: "tool_end", call_id: "c1", ok: true, summary: "23 vs 3", evidence: [evidence] },
    ]);
  });

  it("emits every frame that arrives in one chunk, in order", () => {
    const parser = createAskChefStreamParser();
    const chunk =
      frame("tool_start", { call_id: "c1", tool: "top_values" }) +
      frame("text", { delta: "Errors rose " }) +
      frame("done", { stop_reason: "end", tool_calls: 1, input_tokens: 10, output_tokens: 5, duration_ms: 6200 });

    expect(parser.push(chunk)).toEqual([
      { type: "tool_start", call_id: "c1", tool: "top_values" },
      { type: "text", delta: "Errors rose " },
      { type: "done", stop_reason: "end", tool_calls: 1, input_tokens: 10, output_tokens: 5, duration_ms: 6200 },
    ]);
  });

  it("ignores unknown event types and comments", () => {
    const parser = createAskChefStreamParser();
    const chunk = ": hb\n\n" + frame("usage", { tokens: 3 }) + frame("error", { message: "model timed out" });

    expect(parser.push(chunk)).toEqual([{ type: "error", message: "model timed out" }]);
  });

  it("accepts a tool_end without evidence", () => {
    const parser = createAskChefStreamParser();

    expect(parser.push(frame("tool_end", { call_id: "c2", ok: false, summary: "query failed", evidence: [] }))).toEqual([
      { type: "tool_end", call_id: "c2", ok: false, summary: "query failed", evidence: [] },
    ]);
  });

  it("accepts a tool_end with one record per window", () => {
    const parser = createAskChefStreamParser();
    const second = { ...evidence, id: "E2", start: 3000, end: 4000 };

    expect(parser.push(frame("tool_end", { call_id: "c1", ok: true, summary: "3 rows vs 5 rows", evidence: [evidence, second] }))).toEqual([
      { type: "tool_end", call_id: "c1", ok: true, summary: "3 rows vs 5 rows", evidence: [evidence, second] },
    ]);
  });

  it("accepts every stop reason the server sends and rejects others", () => {
    const parser = createAskChefStreamParser();
    const done = (stop_reason: string) => frame("done", { stop_reason, tool_calls: 0, input_tokens: 0, output_tokens: 0, duration_ms: 1 });

    for (const reason of ["end", "tool_limit", "deadline", "max_tokens", "budget", "preempted"]) {
      expect(parser.push(done(reason))).toMatchObject([{ type: "done", stop_reason: reason }]);
    }
    expect(() => parser.push(done("bored"))).toThrow(AskChefError);
  });

  it("rejects malformed JSON in a known event", () => {
    const parser = createAskChefStreamParser();

    expect(() => parser.push('event: text\ndata: {"delta": \n\n')).toThrow(AskChefError);
  });

  it("rejects a known event whose payload has the wrong shape", () => {
    const parser = createAskChefStreamParser();

    expect(() => parser.push(frame("tool_end", { call_id: "c1", ok: true, summary: "x", evidence: [{ ...evidence, id: "X1" }] }))).toThrow(
      AskChefError
    );
    expect(() => parser.push(frame("text", { delta: 4 }))).toThrow(AskChefError);
  });
});
