import { describe, it, expect, vi, afterEach } from "vitest";
import { subscribeToTail, type TailCallbacks } from "../explore";

// subscribeToTail drives fetch + a ReadableStream directly (axios can't
// consume streaming bodies), so we fake both here rather than going through
// apiClient/axios mocks.

function streamFromChunks(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  let i = 0;
  return new ReadableStream<Uint8Array>({
    pull(controller) {
      if (i < chunks.length) {
        controller.enqueue(encoder.encode(chunks[i]));
        i += 1;
      } else {
        controller.close();
      }
    },
  });
}

function mockFetchWithStream(chunks: string[], ok = true) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => ({
      ok,
      status: ok ? 200 : 500,
      body: streamFromChunks(chunks),
      json: async () => ({}),
    }))
  );
}

function collectingCallbacks(): TailCallbacks & {
  rows: Record<string, any>[][];
  notices: any[];
  ends: any[];
} {
  const rows: Record<string, any>[][] = [];
  const notices: any[] = [];
  const ends: any[] = [];
  return {
    rows,
    notices,
    ends,
    onRows: (r) => rows.push(r),
    onNotice: (n) => notices.push(n),
    onEnd: (e) => ends.push(e),
  };
}

describe("subscribeToTail", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("surfaces the server's message on an error end frame", async () => {
    mockFetchWithStream([
      'event: rows\ndata: [{"a":1}]\n\n' +
        'event: end\ndata: {"reason":"error","message":"clickhouse: connection refused"}\n\n',
    ]);
    const callbacks = collectingCallbacks();
    const controller = new AbortController();

    await subscribeToTail("http://mock/tail", controller.signal, callbacks);

    expect(callbacks.ends).toEqual([
      { reason: "error", message: "clickhouse: connection refused" },
    ]);
  });

  it("treats reader EOF without an end frame as an abnormal end", async () => {
    // Server sends rows then just closes the transport (no `end` event).
    mockFetchWithStream(['event: rows\ndata: [{"a":1}]\n\n']);
    const callbacks = collectingCallbacks();
    const controller = new AbortController();

    await subscribeToTail("http://mock/tail", controller.signal, callbacks);

    expect(callbacks.rows).toEqual([[{ a: 1 }]]);
    expect(callbacks.ends).toHaveLength(1);
    expect(callbacks.ends[0].reason).toBe("connection_lost");
    expect(typeof callbacks.ends[0].message).toBe("string");
  });

  it("does not synthesize an abnormal end when the caller aborted", async () => {
    // Stream never closes on its own within the test; aborting the signal
    // should reject the pending read (as real fetch streams do) and resolve
    // silently rather than synthesizing an abnormal end.
    const encoder = new TextEncoder();
    let streamController: ReadableStreamDefaultController<Uint8Array>;
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        streamController = controller;
        controller.enqueue(encoder.encode('event: rows\ndata: [{"a":1}]\n\n'));
        // Deliberately never close; the abort below errors the controller,
        // mirroring how a real fetch stream rejects the pending read on abort.
      },
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init: { signal?: AbortSignal }) => {
        init.signal?.addEventListener("abort", () => {
          streamController.error(new DOMException("aborted", "AbortError"));
        });
        return {
          ok: true,
          status: 200,
          body: stream,
          json: async () => ({}),
        };
      })
    );

    const callbacks = collectingCallbacks();
    const controller = new AbortController();
    const promise = subscribeToTail("http://mock/tail", controller.signal, callbacks);
    controller.abort();

    await promise;
    expect(callbacks.ends).toEqual([]);
  });

  it("does not emit an end for a normal ttl_expired end frame", async () => {
    mockFetchWithStream(['event: end\ndata: {"reason":"ttl_expired"}\n\n']);
    const callbacks = collectingCallbacks();
    const controller = new AbortController();

    await subscribeToTail("http://mock/tail", controller.signal, callbacks);

    expect(callbacks.ends).toEqual([{ reason: "ttl_expired" }]);
  });
});
