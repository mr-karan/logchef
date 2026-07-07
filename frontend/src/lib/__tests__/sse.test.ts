import { describe, it, expect } from "vitest";
import { createSSEParser, type SSEEvent } from "../sse";

// Convenience: drive a parser with a list of chunks and collect all events.
function feed(chunks: string[]): SSEEvent[] {
  const parser = createSSEParser();
  const events: SSEEvent[] = [];
  for (const c of chunks) {
    events.push(...parser.push(c));
  }
  return events;
}

describe("createSSEParser", () => {
  it("emits the initial comment then a rows message (logchef contract)", () => {
    const events = feed([
      ': ok\n\nevent: rows\ndata: [{"a":1}]\n\n',
    ]);
    expect(events).toEqual([
      { type: "comment", text: "ok" },
      { type: "message", event: "rows", data: '[{"a":1}]' },
    ]);
  });

  it("does not emit a message for a comment-only block", () => {
    const events = feed([": ok\n\n", ": hb\n\n"]);
    expect(events).toEqual([
      { type: "comment", text: "ok" },
      { type: "comment", text: "hb" },
    ]);
  });

  it("strips exactly one leading space from field values and comments", () => {
    const events = feed(["event: notice\ndata:  two-spaces\n\n"]);
    expect(events).toEqual([
      // "data:  two-spaces" -> one space stripped -> " two-spaces"
      { type: "message", event: "notice", data: " two-spaces" },
    ]);
  });

  it("concatenates multi-line data fields with newlines", () => {
    const events = feed(["data: line1\ndata: line2\ndata: line3\n\n"]);
    expect(events).toEqual([
      { type: "message", event: "message", data: "line1\nline2\nline3" },
    ]);
  });

  it("reassembles events split across arbitrary chunk boundaries", () => {
    const events = feed([
      "eve",
      "nt: ro",
      "ws\nda",
      "ta: [{\"x\"",
      ":1}]\n",
      "\n",
    ]);
    expect(events).toEqual([
      { type: "message", event: "rows", data: '[{"x":1}]' },
    ]);
  });

  it("handles multiple messages arriving in a single chunk", () => {
    const events = feed([
      'event: rows\ndata: [1]\n\nevent: notice\ndata: {"code":"rate_limited"}\n\nevent: end\ndata: {"reason":"ttl_expired"}\n\n',
    ]);
    expect(events).toEqual([
      { type: "message", event: "rows", data: "[1]" },
      { type: "message", event: "notice", data: '{"code":"rate_limited"}' },
      { type: "message", event: "end", data: '{"reason":"ttl_expired"}' },
    ]);
  });

  it("tolerates CRLF line endings", () => {
    const events = feed(["event: rows\r\ndata: [1]\r\n\r\n"]);
    expect(events).toEqual([
      { type: "message", event: "rows", data: "[1]" },
    ]);
  });

  it("does not dispatch a partial event until its terminating blank line arrives", () => {
    const parser = createSSEParser();
    expect(parser.push("event: rows\ndata: [1]\n")).toEqual([]);
    // No trailing blank line yet -> nothing dispatched.
    expect(parser.push("\n")).toEqual([
      { type: "message", event: "rows", data: "[1]" },
    ]);
  });

  it("treats a field with no colon as an empty-valued field", () => {
    // "data" alone means an empty data line per the SSE spec.
    const events = feed(["data\n\n"]);
    expect(events).toEqual([
      { type: "message", event: "message", data: "" },
    ]);
  });

  it("keeps heartbeat comments interleaved with row messages", () => {
    const events = feed([
      ': ok\n\nevent: rows\ndata: [1]\n\n: hb\n\nevent: rows\ndata: [2]\n\n',
    ]);
    expect(events).toEqual([
      { type: "comment", text: "ok" },
      { type: "message", event: "rows", data: "[1]" },
      { type: "comment", text: "hb" },
      { type: "message", event: "rows", data: "[2]" },
    ]);
  });
});
