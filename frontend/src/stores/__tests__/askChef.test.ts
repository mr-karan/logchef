import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { Alert, AlertHistoryEntry } from "@/api/alerts";
import { authApi } from "@/api/auth";
import type { EvidenceRecord } from "@/api/askChef";
import type { User } from "@/types";
import { applyAskChefEvent, buildTurnRequest, newSession, useAskChefStore, type AskChefContext, type AskChefSession } from "../askChef";
import { useAuthStore } from "../auth";

vi.mock("@/api/auth", () => ({
  authApi: { localLogin: vi.fn(), getSession: vi.fn(), logout: vi.fn() },
}));

function evidence(n: number): EvidenceRecord {
  return {
    id: `E${n}`,
    tool: "compare_windows",
    team_id: 1,
    source_id: 3,
    mode: "logchefql",
    query: 'level="error"',
    start: 1000,
    end: 2000,
    summary: `finding ${n}`,
  };
}

function streamingSession(): AskChefSession {
  const session = newSession();
  session.transcript.push(
    { role: "user", text: "Why did this fire?" },
    { role: "assistant", steps: [], text: "", evidenceIds: [], done: null }
  );
  session.status = { kind: "streaming" };
  return session;
}

function assistantTurn(session: AskChefSession) {
  const turn = session.transcript[session.transcript.length - 1];
  if (turn.role !== "assistant") throw new Error("expected an assistant turn");
  return turn;
}

const done = { type: "done", stop_reason: "end", tool_calls: 2, input_tokens: 0, output_tokens: 0, duration_ms: 6200 } as const;

describe("applyAskChefEvent", () => {
  it("updates a tool step in place by call_id", () => {
    const session = streamingSession();
    applyAskChefEvent(session, { type: "tool_start", call_id: "c1", tool: "compare_windows" });
    applyAskChefEvent(session, { type: "tool_start", call_id: "c2", tool: "top_values" });
    applyAskChefEvent(session, { type: "tool_end", call_id: "c1", ok: true, summary: "23 vs 3", evidence: [] });
    applyAskChefEvent(session, { type: "tool_end", call_id: "c2", ok: false, summary: "query failed", evidence: [] });

    expect(assistantTurn(session).steps).toEqual([
      { callId: "c1", tool: "compare_windows", status: "ok", summary: "23 vs 3" },
      { callId: "c2", tool: "top_values", status: "failed", summary: "query failed" },
    ]);
  });

  it("accumulates text deltas, including text between tool calls", () => {
    const session = streamingSession();
    applyAskChefEvent(session, { type: "text", delta: "Errors rose " });
    applyAskChefEvent(session, { type: "tool_start", call_id: "c1", tool: "top_values" });
    applyAskChefEvent(session, { type: "text", delta: "7.7x [E1]." });

    expect(assistantTurn(session).text).toBe("Errors rose 7.7x [E1].");
  });

  it("puts evidence in the ledger and on the turn that produced it", () => {
    const session = streamingSession();
    applyAskChefEvent(session, { type: "tool_end", call_id: "c1", ok: true, summary: "23 vs 3", evidence: [evidence(4), evidence(5)] });

    expect(session.evidence.get("E4")).toEqual(evidence(4));
    expect(session.evidence.get("E5")).toEqual(evidence(5));
    expect(assistantTurn(session).evidenceIds).toEqual(["E4", "E5"]);
  });

  it("returns to idle on done and keeps the summary on the turn", () => {
    const session = streamingSession();
    applyAskChefEvent(session, done);

    expect(session.status).toEqual({ kind: "idle" });
    expect(assistantTurn(session).done).toEqual(done);
  });

  it("enters the error state on an error event", () => {
    const session = streamingSession();
    applyAskChefEvent(session, { type: "error", message: "model timed out" });

    expect(session.status).toEqual({ kind: "error", message: "model timed out" });
  });
});

describe("buildTurnRequest", () => {
  const alertContext: AskChefContext = {
    teamId: 1,
    sourceId: 3,
    start: 1000,
    end: 2000,
    mode: "logchefql",
    query: 'level="error"',
    subject: {
      kind: "alert",
      alertId: 7,
      alertHistoryId: 2,
      alertName: "High error rate",
      triggeredAt: "2026-10-15T06:08:00Z",
      value: 23,
      thresholdOperator: "gt",
      thresholdValue: 10,
    },
  };

  it("sends finished exchanges as history and every known evidence record", () => {
    const session = streamingSession();
    applyAskChefEvent(session, { type: "tool_end", call_id: "c1", ok: true, summary: "x", evidence: [evidence(1)] });
    applyAskChefEvent(session, { type: "text", delta: "It fired [E1]." });
    applyAskChefEvent(session, done);
    session.transcript.push({ role: "user", text: "Which hosts?" }, { role: "assistant", steps: [], text: "", evidenceIds: [], done: null });

    expect(buildTurnRequest(alertContext, session, "Show sample errors", "Asia/Kolkata")).toEqual({
      team_id: 1,
      source_id: 3,
      timezone: "Asia/Kolkata",
      context: { kind: "alert", mode: "logchefql", query: 'level="error"', start: 1000, end: 2000, alert_id: 7, alert_history_id: 2 },
      history: [
        { role: "user", text: "Why did this fire?" },
        { role: "assistant", text: "It fired [E1]." },
      ],
      evidence: [evidence(1)],
      message: "Show sample errors",
    });
  });

  it("keeps the request inside the contract limits", () => {
    const session = newSession();
    for (let i = 0; i < 15; i++) {
      session.transcript.push({ role: "user", text: `q${i}` }, { role: "assistant", steps: [], text: "a".repeat(9000), evidenceIds: [], done: null });
    }
    for (let n = 1; n <= 60; n++) session.evidence.set(`E${n}`, evidence(n));

    const request = buildTurnRequest({ ...alertContext, subject: { kind: "explorer" } }, session, "next", "UTC");

    expect(request.context).toEqual({ kind: "explorer", mode: "logchefql", query: 'level="error"', start: 1000, end: 2000 });
    expect(request.history).toHaveLength(20);
    expect(request.history[0]).toEqual({ role: "user", text: "q5" });
    expect(Math.max(...request.history.map((m) => m.text.length))).toBe(8000);
    expect(request.evidence.map((e) => e.id)).toEqual(Array.from({ length: 50 }, (_, i) => `E${i + 11}`));
  });
});

function sseResponse(chunks: string[]): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

function jsonResponse(status: number, message: string): Response {
  return new Response(JSON.stringify({ status: "error", message, error_type: "x" }), { status, headers: { "Content-Type": "application/json" } });
}

function frame(type: string, data: unknown): string {
  return `event: ${type}\ndata: ${JSON.stringify(data)}\n\n`;
}

const alert = {
  id: 7,
  source_id: 3,
  name: "High error rate",
  query_language: "logchefql",
  editor_mode: "condition",
  query: "",
  condition_json: 'level="error"',
  lookback_seconds: 300,
  threshold_operator: "gt",
  threshold_value: 10,
  frequency_seconds: 60,
  severity: "critical",
  recipient_user_ids: [],
  webhook_urls: [],
  is_active: true,
  last_state: "firing",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
} satisfies Alert;

const entry = {
  id: 2,
  alert_id: 7,
  status: "triggered",
  triggered_at: "2026-10-15T06:08:00Z",
  value: 23,
  created_at: "2026-10-15T06:08:00Z",
} satisfies AlertHistoryEntry;

describe("useAskChefStore", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("investigates an alert and streams the answer into the transcript", async () => {
    const wire =
      frame("tool_start", { call_id: "c1", tool: "compare_windows" }) +
      frame("tool_end", { call_id: "c1", ok: true, summary: "23 vs 3", evidence: [evidence(1)] }) +
      frame("text", { delta: "Errors rose 7.7x [E1]." }) +
      frame("done", { stop_reason: "end", tool_calls: 1, input_tokens: 1, output_tokens: 1, duration_ms: 6200 });
    const fetchMock = vi.fn().mockResolvedValue(sseResponse([wire.slice(0, 50), wire.slice(50, 51), wire.slice(51)]));
    vi.stubGlobal("fetch", fetchMock);
    const store = useAskChefStore();

    store.investigateAlert(alert, entry, 1);
    await vi.waitFor(() => expect(store.status).toEqual({ kind: "idle" }));

    const body: unknown = JSON.parse(String(fetchMock.mock.calls[0][1].body));
    expect(body).toMatchObject({
      team_id: 1,
      source_id: 3,
      message: "Why did this fire?",
      history: [],
      evidence: [],
      context: { kind: "alert", alert_id: 7, alert_history_id: 2, mode: "logchefql", query: 'level="error"', end: Date.parse(entry.triggered_at) },
    });
    expect(store.open).toBe(true);
    expect(store.transcript).toEqual([
      { role: "user", text: "Why did this fire?" },
      {
        role: "assistant",
        steps: [{ callId: "c1", tool: "compare_windows", status: "ok", summary: "23 vs 3" }],
        text: "Errors rose 7.7x [E1].",
        evidenceIds: ["E1"],
        done: { type: "done", stop_reason: "end", tool_calls: 1, input_tokens: 1, output_tokens: 1, duration_ms: 6200 },
      },
    ]);
    expect(store.evidence.get("E1")).toEqual(evidence(1));
  });

  it("ends a turn that a newer turn preempted without an error", async () => {
    const wire =
      frame("text", { delta: "Errors rose" }) +
      frame("done", { stop_reason: "preempted", tool_calls: 0, input_tokens: 1, output_tokens: 1, duration_ms: 900 });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(sseResponse([wire])));
    const store = useAskChefStore();

    store.investigateAlert(alert, entry, 1);
    await vi.waitFor(() => expect(store.transcript[1]).toMatchObject({ done: { stop_reason: "preempted" } }));

    expect(store.status).toEqual({ kind: "idle" });
    expect(store.transcript[1]).toMatchObject({ role: "assistant", text: "Errors rose" });
  });

  it("switches to disabled when the server has Ask Chef off (404)", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(404, "not found")));
    const store = useAskChefStore();

    store.investigateAlert(alert, entry, 1);
    await vi.waitFor(() => expect(store.status).toEqual({ kind: "disabled" }));
  });

  it("fails the turn when the stream closes before done", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(sseResponse([frame("text", { delta: "Errors rose" })])));
    const store = useAskChefStore();

    store.investigateAlert(alert, entry, 1);
    await vi.waitFor(() => expect(store.status.kind).toBe("error"));

    expect(store.transcript[1]).toMatchObject({ role: "assistant", text: "Errors rose", done: null });
  });

  it("stops a running turn and returns to idle", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((_url: string, init: RequestInit) => {
        const body = new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(new TextEncoder().encode(frame("tool_start", { call_id: "c1", tool: "top_values" })));
            init.signal?.addEventListener("abort", () => controller.error(new DOMException("aborted", "AbortError")));
          },
        });
        return Promise.resolve(new Response(body, { status: 200 }));
      })
    );
    const store = useAskChefStore();

    store.investigateAlert(alert, entry, 1);
    await vi.waitFor(() => expect(store.transcript[1]).toMatchObject({ steps: [{ callId: "c1", status: "running" }] }));
    store.stop();
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(store.status).toEqual({ kind: "idle" });
  });
});

// A fetch that streams one tool_start and then hangs until its signal aborts.
function hangingTurn(onAbort: () => void) {
  return vi.fn((_url: string, init: RequestInit) => {
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode(frame("tool_start", { call_id: "c1", tool: "top_values" })));
        init.signal?.addEventListener("abort", () => {
          onAbort();
          controller.error(new DOMException("aborted", "AbortError"));
        });
      },
    });
    return Promise.resolve(new Response(body, { status: 200 }));
  });
}

describe("useAskChefStore and the signed-in user", () => {
  function user(id: string): User {
    return { id, email: `user${id}@example.com`, full_name: `User ${id}`, role: "member", status: "active", account_type: "human", created_at: "", updated_at: "" };
  }

  beforeEach(() => {
    setActivePinia(createPinia());
    vi.mocked(authApi.localLogin).mockResolvedValue({ status: "success", data: null });
    vi.mocked(authApi.logout).mockResolvedValue({ status: "success", data: undefined });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.resetAllMocks();
  });

  async function signIn(id: string) {
    vi.mocked(authApi.getSession).mockResolvedValueOnce({ status: "success", data: { user: user(id), session: { id: "s", user_id: id, expires_at: "", created_at: "" } } });
    await useAuthStore().localLogin(`user${id}@example.com`, "secret");
  }

  async function startTurn(store: ReturnType<typeof useAskChefStore>, onAbort: () => void) {
    vi.stubGlobal("fetch", hangingTurn(onAbort));
    store.investigateAlert(alert, entry, 1);
    await vi.waitFor(() => expect(store.transcript[1]).toMatchObject({ steps: [{ callId: "c1", status: "running" }] }));
  }

  function expectCleared(store: ReturnType<typeof useAskChefStore>) {
    expect(store.transcript).toEqual([]);
    expect(store.evidence.size).toBe(0);
    expect(store.status).toEqual({ kind: "idle" });
    expect(store.context).toBeNull();
    expect(store.open).toBe(false);
  }

  it("aborts and clears the conversation when another user signs in", async () => {
    await signIn("1");
    const store = useAskChefStore();
    let aborted = false;
    await startTurn(store, () => (aborted = true));

    await signIn("2");

    expect(aborted).toBe(true);
    expectCleared(store);
  });

  it("aborts and clears the conversation on logout", async () => {
    await signIn("1");
    const store = useAskChefStore();
    let aborted = false;
    await startTurn(store, () => (aborted = true));

    await useAuthStore().logout();

    expect(aborted).toBe(true);
    expectCleared(store);
  });
});
