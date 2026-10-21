import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";

import router from "@/router";
import { useAuthStore } from "@/stores/auth";
import * as errorHandler from "@/api/error-handler";
import type { SSEEvent } from "@/lib/sse";
import { expireSession } from "../session";
import { streamTurn } from "../askChef";
import { subscribeToTail } from "../explore";
import { streamWindowedQuery } from "../windowed";
import { streamSSE, StreamHTTPError, type StreamRequest } from "../eventStream";

const encoder = new TextEncoder();

interface FakeServer {
  fetch: ReturnType<typeof vi.fn>;
  push(chunk: string | Uint8Array): void;
  close(): void;
  cancelled: () => boolean;
}

function fakeServer(): FakeServer {
  let body: ReadableStreamDefaultController<Uint8Array> | undefined;
  let cancelled = false;
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      body = controller;
    },
    cancel() {
      cancelled = true;
    },
  });
  const fetch = vi.fn(async (_url: string, init: RequestInit) => {
    init.signal?.addEventListener("abort", () => body?.error(new DOMException("aborted", "AbortError")));
    return new Response(stream, { status: 200, headers: { "X-LogChef-Query-ID": "q-1" } });
  });
  vi.stubGlobal("fetch", fetch);
  return {
    fetch,
    push: (chunk) => body?.enqueue(typeof chunk === "string" ? encoder.encode(chunk) : chunk),
    close: () => body?.close(),
    cancelled: () => cancelled,
  };
}

function failWith(status: number, body: string) {
  vi.stubGlobal("fetch", vi.fn(async () => new Response(body, { status })));
}

function request(overrides: Partial<StreamRequest> = {}): StreamRequest {
  return { method: "GET", signal: new AbortController().signal, label: "Test stream", ...overrides };
}

function collect(): { frames: SSEEvent[]; onFrame: (frame: SSEEvent) => void } {
  const frames: SSEEvent[] = [];
  return { frames, onFrame: (frame) => void frames.push(frame) };
}

describe("streamSSE", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("rejects a missing response body with the request label", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));

    await expect(streamSSE("/s", request(), () => {})).rejects.toThrow("Test stream returned no stream");
  });

  it("resolves aborted when a bodyless response arrives after abort", async () => {
    const controller = new AbortController();
    vi.stubGlobal("fetch", vi.fn(async () => {
      controller.abort();
      return new Response(null, { status: 200 });
    }));

    await expect(streamSSE("/s", request({ signal: controller.signal }), () => {})).resolves.toBe("aborted");
  });

  it("sends a JSON body with SSE headers and exposes response headers on open", async () => {
    const server = fakeServer();
    const onOpen = vi.fn();
    const promise = streamSSE("/api/v1/stream", request({ method: "POST", body: { query: "a" }, onOpen }), () => {});
    server.close();

    expect(await promise).toBe("eof");
    const [url, init] = server.fetch.mock.calls[0];
    expect(url).toBe("/api/v1/stream");
    expect(init).toMatchObject({
      method: "POST",
      credentials: "same-origin",
      cache: "no-store",
      headers: { Accept: "text/event-stream", "Content-Type": "application/json" },
      body: '{"query":"a"}',
    });
    expect(onOpen.mock.calls[0][0].get("X-LogChef-Query-ID")).toBe("q-1");
  });

  it("assembles frames split across chunk boundaries, including a split UTF-8 character", async () => {
    const server = fakeServer();
    const { frames, onFrame } = collect();
    const promise = streamSSE("/s", request(), onFrame);
    const wire = encoder.encode(': ok\n\nevent: rows\ndata: [{"msg":"héllo"}]\n\ndata: second\n\n');
    const accent = wire.indexOf(0xc3);
    for (const cut of [[0, 3], [3, 15], [15, accent + 1], [accent + 1, wire.length]]) {
      server.push(wire.slice(cut[0], cut[1]));
    }
    server.close();

    expect(await promise).toBe("eof");
    expect(frames).toEqual([
      { type: "comment", text: "ok" },
      { type: "message", event: "rows", data: '[{"msg":"héllo"}]' },
      { type: "message", event: "message", data: "second" },
    ]);
  });

  it("stops when onFrame asks and cancels the response body", async () => {
    const server = fakeServer();
    const { frames, onFrame } = collect();
    server.push("event: end\ndata: {}\n\nevent: rows\ndata: []\n\n");

    const outcome = await streamSSE("/s", request(), (frame) => {
      onFrame(frame);
      return "stop";
    });

    expect(outcome).toBe("stop");
    expect(frames).toEqual([{ type: "message", event: "end", data: "{}" }]);
    expect(server.cancelled()).toBe(true);
  });

  it("resolves aborted when the signal aborts mid-stream and drops the partial frame", async () => {
    const server = fakeServer();
    const controller = new AbortController();
    const { frames, onFrame } = collect();
    const promise = streamSSE("/s", request({ signal: controller.signal }), onFrame);
    server.push("data: one\n\n");
    await vi.waitFor(() => expect(frames).toHaveLength(1));

    server.push("data: tw");
    controller.abort();

    expect(await promise).toBe("aborted");
    expect(frames).toEqual([{ type: "message", event: "message", data: "one" }]);
  });

  it("resolves aborted when the request is aborted before the response", async () => {
    const controller = new AbortController();
    vi.stubGlobal("fetch", vi.fn(async () => Promise.reject(new DOMException("aborted", "AbortError"))));
    controller.abort();

    expect(await streamSSE("/s", request({ signal: controller.signal }), () => {})).toBe("aborted");
  });

  it("drops later frames in the same chunk when a callback aborts", async () => {
    const server = fakeServer();
    const controller = new AbortController();
    const frames: SSEEvent[] = [];
    server.push("data: first\n\ndata: second\n\n");

    const outcome = await streamSSE("/s", request({ signal: controller.signal }), (frame) => {
      frames.push(frame);
      controller.abort();
    });

    expect(outcome).toBe("aborted");
    expect(frames).toEqual([{ type: "message", event: "message", data: "first" }]);
  });

  it("resolves aborted while reading an HTTP error body without expiring the session", async () => {
    const controller = new AbortController();
    const push = vi.spyOn(router, "push").mockResolvedValue(undefined);
    const body = new ReadableStream<Uint8Array>({
      start(stream) {
        controller.signal.addEventListener("abort", () => stream.error(new DOMException("aborted", "AbortError")));
      },
    });
    vi.stubGlobal("fetch", vi.fn(async () => new Response(body, { status: 401 })));
    const promise = streamSSE("/s", request({ signal: controller.signal }), () => {});
    await vi.waitFor(() => expect(body.locked).toBe(true));
    controller.abort();

    expect(await promise).toBe("aborted");
    expect(push).not.toHaveBeenCalled();
  });

  it("cancels the body if onOpen throws", async () => {
    const server = fakeServer();

    await expect(streamSSE("/s", request({ onOpen: () => { throw new Error("open failed"); } }), () => {})).rejects.toThrow("open failed");
    expect(server.cancelled()).toBe(true);
  });

  it("discards an incomplete final frame at EOF", async () => {
    const server = fakeServer();
    const { frames, onFrame } = collect();
    server.push("data: complete\n\ndata: partial\n");
    server.close();

    expect(await streamSSE("/s", request(), onFrame)).toBe("eof");
    expect(frames).toEqual([{ type: "message", event: "message", data: "complete" }]);
  });

  it("rethrows an error from onFrame when the signal is live", async () => {
    const server = fakeServer();
    server.push("data: bad\n\n");

    await expect(
      streamSSE("/s", request(), () => {
        throw new Error("bad frame");
      })
    ).rejects.toThrow("bad frame");
  });

  it("preserves the status and message from a JSON HTTP error", async () => {
    failWith(422, JSON.stringify({ status: "error", message: "unknown field foo", error_type: "ValidationError" }));

    const err = await streamSSE("/s", request(), () => {}).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(StreamHTTPError);
    expect(err).toMatchObject({
      status: 422,
      message: "unknown field foo",
    });
  });

  it("falls back to a labelled message when a 5xx body is not JSON", async () => {
    failWith(502, "<html>Bad Gateway</html>");

    await expect(streamSSE("/s", request(), () => {})).rejects.toMatchObject({
      status: 502,
      message: "Test stream failed (502)",
    });
  });

  it("runs the session-expired flow once on a 401", async () => {
    const push = vi.spyOn(router, "push").mockResolvedValue(undefined);
    const clearState = vi.spyOn(useAuthStore(), "clearState");
    const toast = vi.spyOn(errorHandler, "showErrorToast").mockImplementation(() => {});
    failWith(401, JSON.stringify({ status: "error", message: "session expired", error_type: "AuthenticationError" }));

    await expect(streamSSE("/s", request(), () => {})).rejects.toMatchObject({ status: 401 });

    expect(clearState).toHaveBeenCalledTimes(1);
    expect(toast).toHaveBeenCalledTimes(1);
    expect(push).toHaveBeenCalledTimes(1);
    expect(push.mock.calls[0][0]).toMatchObject({ name: "Login", replace: true });
  });

  it("does not run the session-expired flow on other errors", async () => {
    const push = vi.spyOn(router, "push").mockResolvedValue(undefined);
    failWith(403, JSON.stringify({ status: "error", message: "forbidden", error_type: "AuthorizationError" }));

    await expect(streamSSE("/s", request(), () => {})).rejects.toMatchObject({ status: 403 });

    expect(push).not.toHaveBeenCalled();
  });
});

describe("expireSession", () => {
  beforeEach(() => setActivePinia(createPinia()));
  afterEach(() => vi.restoreAllMocks());

  it.each([
    { toast: true, path: "/explore", notifications: 1 },
    { toast: false, path: "/explore", notifications: 0 },
    { toast: true, path: "/auth/login", notifications: 0 },
  ])("clears auth and redirects with toast=$toast at $path", ({ toast, path, notifications }) => {
    const previousPath = window.location.href;
    window.history.replaceState(null, "", path);
    try {
      const push = vi.spyOn(router, "push").mockResolvedValue(undefined);
      const clearState = vi.spyOn(useAuthStore(), "clearState");
      const showErrorToast = vi.spyOn(errorHandler, "showErrorToast").mockImplementation(() => {});

      expireSession({ toast });

      expect(clearState).toHaveBeenCalledTimes(1);
      expect(showErrorToast).toHaveBeenCalledTimes(notifications);
      expect(push).toHaveBeenCalledExactlyOnceWith({ name: "Login", query: { redirect: router.options.history.location }, replace: true });
    } finally {
      window.history.replaceState(null, "", previousPath);
    }
  });
});

const callers = [
  { name: "live tail", label: "Live tail request", run: (signal: AbortSignal) => subscribeToTail("/tail", signal, {}) },
  { name: "windowed query", label: "Windowed query", run: (signal: AbortSignal) => streamWindowedQuery(1, 2, { query_text: "", query_language: "logchefql" }, signal, () => {}) },
  { name: "Ask Chef", label: "Ask Chef request", run: (signal: AbortSignal) => streamTurn({
    team_id: 1, source_id: 2, timezone: "UTC", context: { kind: "explorer", start: 0, end: 1 }, history: [], evidence: [], message: "Explain these logs",
  }, () => {}, signal) },
];

describe.each(callers)("$name stream errors", ({ run, label }) => {
  beforeEach(() => setActivePinia(createPinia()));
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("rejects a missing response body with the request label", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 200 })));

    await expect(run(new AbortController().signal)).rejects.toThrow(`${label} returned no stream`);
  });

  it("expires the session once on a 401", async () => {
    const push = vi.spyOn(router, "push").mockResolvedValue(undefined);
    const clearState = vi.spyOn(useAuthStore(), "clearState");
    const toast = vi.spyOn(errorHandler, "showErrorToast").mockImplementation(() => {});
    failWith(401, JSON.stringify({ status: "error", message: "session expired", error_type: "AuthenticationError" }));

    await expect(run(new AbortController().signal)).rejects.toThrow("session expired");

    expect(clearState).toHaveBeenCalledTimes(1);
    expect(toast).toHaveBeenCalledTimes(1);
    expect(push).toHaveBeenCalledExactlyOnceWith({ name: "Login", query: { redirect: router.options.history.location }, replace: true });
  });
});
