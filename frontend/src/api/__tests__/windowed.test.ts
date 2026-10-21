import { afterEach, describe, expect, it, vi } from "vitest";
import { streamWindowedQuery, WindowedHTTPError, type WindowedEvent, type WindowedRequest } from "../windowed";

const request: WindowedRequest = { query_text: "", query_language: "logchefql" };
const encoder = new TextEncoder();

function serve(chunks: string[], queryId?: string) {
  const fetch = vi.fn(async () => new Response(new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  }), { headers: queryId ? { "X-LogChef-Query-ID": queryId } : {} }));
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

async function query(chunks: string[]): Promise<WindowedEvent[]> {
  serve(chunks);
  const events: WindowedEvent[] = [];
  await streamWindowedQuery(1, 2, request, new AbortController().signal, (event) => events.push(event));
  return events;
}

describe("streamWindowedQuery", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("dispatches split JSON frames and ignores comments and an incomplete final line", async () => {
    expect(await query([
      ': hb\n\ndata: {"type":"ro',
      'ws","logs":[{"message":"hello"}],"complete":false}\n\n',
      'data: {"type":"end","complete":true}\n\ndata: partial',
    ])).toEqual([
      { type: "rows", logs: [{ message: "hello" }], complete: false },
      { type: "end", complete: true },
    ]);
  });

  it("continues dispatching until EOF after an end event", async () => {
    expect(await query(['data: {"type":"end","complete":true}\n\ndata: {"type":"count","complete":true}\n\n'])).toEqual([
      { type: "end", complete: true }, { type: "count", complete: true },
    ]);
  });

  it("keeps HTTP failures identifiable to the fallback caller", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ message: "unsupported query", error_type: "ValidationError" }), { status: 422 })));

    const error = await streamWindowedQuery(1, 2, request, new AbortController().signal, () => {}).catch((error: unknown) => error);

    expect(error).toBeInstanceOf(WindowedHTTPError);
    expect(error).toMatchObject({ status: 422, message: "unsupported query" });
  });

  it("rejects invalid event types", async () => {
    await expect(query(['data: {"type":"unknown"}\n\n'])).rejects.toThrow("Invalid windowed query event");
  });

  it("cancels the server query once on premature EOF", async () => {
    const fetch = serve(['data: {"type":"rows","complete":false}\n\n'], "q /1");

    await expect(streamWindowedQuery(1, 2, request, new AbortController().signal, () => {})).rejects.toThrow("Connection closed before the windowed query finished");

    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch).toHaveBeenLastCalledWith(expect.stringContaining("/query/q%20%2F1/cancel"), expect.objectContaining({ method: "POST" }));
  });

  it("does not cancel a query after an end event", async () => {
    const fetch = serve(['data: {"type":"end","complete":true}\n\n'], "q-1");

    await streamWindowedQuery(1, 2, request, new AbortController().signal, () => {});

    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("cancels once when aborted while awaiting response headers", async () => {
    const controller = new AbortController();
    const fetch = serve([], "q-1");
    const promise = streamWindowedQuery(1, 2, request, controller.signal, () => {});
    controller.abort();

    await expect(promise).resolves.toBeUndefined();
    expect(fetch).toHaveBeenCalledTimes(2);
    expect(fetch).toHaveBeenLastCalledWith(expect.stringContaining("/query/q-1/cancel"), expect.objectContaining({ method: "POST" }));
  });
});
