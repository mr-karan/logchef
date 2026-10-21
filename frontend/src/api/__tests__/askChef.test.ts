import { afterEach, describe, expect, it, vi } from "vitest";

import { AskChefError, streamTurn, type AskChefEvent, type TurnRequest } from "../askChef";

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

const request: TurnRequest = {
  team_id: 1,
  source_id: 3,
  timezone: "UTC",
  context: { kind: "explorer", start: 1000, end: 2000 },
  history: [],
  evidence: [],
  message: "why did errors rise?",
};

const done = frame("done", { stop_reason: "end", tool_calls: 0, input_tokens: 0, output_tokens: 0, duration_ms: 1 });
const doneEvent = { type: "done", stop_reason: "end", tool_calls: 0, input_tokens: 0, output_tokens: 0, duration_ms: 1 };

function serve(chunks: string[], status = 200, body: unknown = {}) {
  const encoder = new TextEncoder();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      const stream = new ReadableStream<Uint8Array>({
        start(controller) {
          for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
          controller.close();
        },
      });
      return status === 200 ? new Response(stream, { status }) : new Response(JSON.stringify(body), { status });
    })
  );
}

async function runTurn(chunks: string[]): Promise<AskChefEvent[]> {
  serve(chunks);
  const events: AskChefEvent[] = [];
  await streamTurn(request, (event) => events.push(event), new AbortController().signal);
  return events;
}

describe("streamTurn", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("emits an event only once a frame split across chunks is complete", async () => {
    const wire = frame("tool_end", { call_id: "c1", ok: true, summary: "23 vs 3", evidence: [evidence] }) + done;

    expect(await runTurn([wire.slice(0, 7), wire.slice(7, 40), wire.slice(40, -1), wire.slice(-1)])).toEqual([
      { type: "tool_end", call_id: "c1", ok: true, summary: "23 vs 3", evidence: [evidence] },
      doneEvent,
    ]);
  });

  it("emits every frame that arrives in one chunk, in order", async () => {
    const chunk =
      frame("tool_start", { call_id: "c1", tool: "top_values" }) +
      frame("text", { delta: "Errors rose " }) +
      frame("done", { stop_reason: "end", tool_calls: 1, input_tokens: 10, output_tokens: 5, duration_ms: 6200 });

    expect(await runTurn([chunk])).toEqual([
      { type: "tool_start", call_id: "c1", tool: "top_values" },
      { type: "text", delta: "Errors rose " },
      { type: "done", stop_reason: "end", tool_calls: 1, input_tokens: 10, output_tokens: 5, duration_ms: 6200 },
    ]);
  });

  it("ignores unknown event types and comments", async () => {
    const chunk = ": hb\n\n" + frame("usage", { tokens: 3 }) + frame("error", { message: "model timed out" });

    expect(await runTurn([chunk])).toEqual([{ type: "error", message: "model timed out" }]);
  });

  it("stops at the first done event", async () => {
    expect(await runTurn([done + frame("text", { delta: "late" })])).toEqual([doneEvent]);
  });

  it("accepts a tool_end without evidence", async () => {
    expect(await runTurn([frame("tool_end", { call_id: "c2", ok: false, summary: "query failed", evidence: [] }) + done])).toEqual([
      { type: "tool_end", call_id: "c2", ok: false, summary: "query failed", evidence: [] },
      doneEvent,
    ]);
  });

  it("accepts a tool_end with one record per window", async () => {
    const second = { ...evidence, id: "E2", start: 3000, end: 4000 };

    expect(await runTurn([frame("tool_end", { call_id: "c1", ok: true, summary: "3 rows vs 5 rows", evidence: [evidence, second] }) + done])).toEqual([
      { type: "tool_end", call_id: "c1", ok: true, summary: "3 rows vs 5 rows", evidence: [evidence, second] },
      doneEvent,
    ]);
  });

  it("accepts every stop reason the server sends and rejects others", async () => {
    const doneWith = (stop_reason: string) => frame("done", { stop_reason, tool_calls: 0, input_tokens: 0, output_tokens: 0, duration_ms: 1 });

    for (const reason of ["end", "tool_limit", "deadline", "max_tokens", "budget", "preempted"]) {
      expect(await runTurn([doneWith(reason)])).toMatchObject([{ type: "done", stop_reason: reason }]);
    }
    await expect(runTurn([doneWith("bored")])).rejects.toThrow(AskChefError);
  });

  it("rejects malformed JSON in a known event", async () => {
    await expect(runTurn(['event: text\ndata: {"delta": \n\n'])).rejects.toThrow(AskChefError);
  });

  it("rejects a known event whose payload has the wrong shape", async () => {
    await expect(runTurn([frame("tool_end", { call_id: "c1", ok: true, summary: "x", evidence: [{ ...evidence, id: "X1" }] })])).rejects.toThrow(
      AskChefError
    );
    await expect(runTurn([frame("text", { delta: 4 })])).rejects.toThrow(AskChefError);
  });

  it("rejects with a protocol error when the stream closes before done", async () => {
    await expect(runTurn([frame("text", { delta: "Errors rose " })])).rejects.toMatchObject({ reason: "protocol" });
  });

  it("maps a 404 to disabled and other failures to failed, keeping the server message", async () => {
    serve([], 404, { status: "error", message: "Ask Chef is not enabled", error_type: "NotFoundError" });
    await expect(streamTurn(request, () => {}, new AbortController().signal)).rejects.toMatchObject({
      reason: "disabled",
      message: "Ask Chef is not enabled",
    });

    serve([], 500, { status: "error", message: "model unavailable", error_type: "GeneralError" });
    await expect(streamTurn(request, () => {}, new AbortController().signal)).rejects.toMatchObject({
      reason: "failed",
      message: "model unavailable",
    });
  });
});
