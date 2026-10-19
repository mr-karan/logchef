import { apiBaseURL } from "./config";
import { createSSEParser } from "@/lib/sse";

export type QueryMode = "logchefql" | "native";
export type EvidenceId = `E${number}`;
export type StopReason = "end" | "tool_limit" | "deadline" | "max_tokens" | "budget" | "preempted";

export interface EvidenceRecord {
  id: EvidenceId;
  tool: string;
  team_id: number;
  source_id: number;
  mode: QueryMode;
  query: string;
  start: number;
  end: number;
  summary: string;
}

interface TurnWindow {
  mode?: QueryMode;
  query?: string;
  start: number;
  end: number;
}

export type TurnContext =
  | (TurnWindow & { kind: "explorer" })
  | (TurnWindow & { kind: "alert"; alert_id: number; alert_history_id: number });

export interface HistoryMessage {
  role: "user" | "assistant";
  text: string;
}

export interface TurnRequest {
  team_id: number;
  source_id: number;
  timezone: string;
  context: TurnContext;
  history: HistoryMessage[];
  evidence: EvidenceRecord[];
  message: string;
}

export interface DoneEvent {
  type: "done";
  stop_reason: StopReason;
  tool_calls: number;
  input_tokens: number;
  output_tokens: number;
  duration_ms: number;
}

export type AskChefEvent =
  | { type: "tool_start"; call_id: string; tool: string }
  | { type: "tool_end"; call_id: string; ok: boolean; summary: string; evidence: EvidenceRecord[] }
  | { type: "text"; delta: string }
  | DoneEvent
  | { type: "error"; message: string };

// disabled is the server's 404.
export type AskChefErrorReason = "disabled" | "failed" | "protocol";

export class AskChefError extends Error {
  constructor(public reason: AskChefErrorReason, message: string) {
    super(message);
    this.name = "AskChefError";
  }
}

export function isEvidenceId(value: string): value is EvidenceId {
  return /^E\d+$/.test(value);
}

export function evidenceNumber(id: EvidenceId): number {
  return Number(id.slice(1));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isQueryMode(value: unknown): value is QueryMode {
  return value === "logchefql" || value === "native";
}

const STOP_REASONS: ReadonlySet<unknown> = new Set<StopReason>(["end", "tool_limit", "deadline", "max_tokens", "budget", "preempted"]);

function isStopReason(value: unknown): value is StopReason {
  return STOP_REASONS.has(value);
}

function parseEvidence(raw: unknown): EvidenceRecord | null {
  if (!isRecord(raw)) return null;
  const { id, tool, team_id, source_id, mode, query, start, end, summary } = raw;
  if (typeof id !== "string" || !isEvidenceId(id)) return null;
  if (typeof tool !== "string" || typeof query !== "string" || typeof summary !== "string") return null;
  if (typeof team_id !== "number" || typeof source_id !== "number") return null;
  if (typeof start !== "number" || typeof end !== "number" || !isQueryMode(mode)) return null;
  return { id, tool, team_id, source_id, mode, query, start, end, summary };
}

function parsePayload(type: AskChefEvent["type"], raw: Record<string, unknown>): AskChefEvent | null {
  switch (type) {
    case "tool_start": {
      const { call_id, tool } = raw;
      return typeof call_id === "string" && typeof tool === "string" ? { type, call_id, tool } : null;
    }
    case "tool_end": {
      const { call_id, ok, summary } = raw;
      if (typeof call_id !== "string" || typeof ok !== "boolean" || typeof summary !== "string") return null;
      if (!Array.isArray(raw.evidence)) return null;
      const evidence: EvidenceRecord[] = [];
      for (const item of raw.evidence) {
        const record = parseEvidence(item);
        if (!record) return null;
        evidence.push(record);
      }
      return { type, call_id, ok, summary, evidence };
    }
    case "text":
      return typeof raw.delta === "string" ? { type, delta: raw.delta } : null;
    case "done": {
      const { stop_reason, tool_calls, input_tokens, output_tokens, duration_ms } = raw;
      if (!isStopReason(stop_reason) || typeof tool_calls !== "number" || typeof duration_ms !== "number") return null;
      return {
        type,
        stop_reason,
        tool_calls,
        duration_ms,
        input_tokens: typeof input_tokens === "number" ? input_tokens : 0,
        output_tokens: typeof output_tokens === "number" ? output_tokens : 0,
      };
    }
    case "error":
      return typeof raw.message === "string" ? { type, message: raw.message } : null;
  }
}

const EVENT_TYPES: ReadonlySet<string> = new Set<AskChefEvent["type"]>(["tool_start", "tool_end", "text", "done", "error"]);

function isEventType(value: string): value is AskChefEvent["type"] {
  return EVENT_TYPES.has(value);
}

// Turns one SSE frame into a typed event. Unknown event names are skipped so
// the server can add events without breaking older clients.
export function parseAskChefEvent(name: string, data: string): AskChefEvent | null {
  if (!isEventType(name)) return null;
  let raw: unknown;
  try {
    raw = JSON.parse(data);
  } catch {
    throw new AskChefError("protocol", `Malformed ${name} event`);
  }
  const event = isRecord(raw) ? parsePayload(name, raw) : null;
  if (!event) throw new AskChefError("protocol", `Invalid ${name} event`);
  return event;
}

export interface AskChefStreamParser {
  push(chunk: string): AskChefEvent[];
}

export function createAskChefStreamParser(): AskChefStreamParser {
  const sse = createSSEParser();
  return {
    push(chunk) {
      const events: AskChefEvent[] = [];
      for (const frame of sse.push(chunk)) {
        if (frame.type !== "message") continue;
        const event = parseAskChefEvent(frame.event, frame.data);
        if (event) events.push(event);
      }
      return events;
    },
  };
}

async function readError(response: Response): Promise<AskChefError> {
  const body: unknown = await response.json().catch(() => null);
  const message = isRecord(body) && typeof body.message === "string" ? body.message : `Ask Chef request failed (${response.status})`;
  if (response.status === 404) return new AskChefError("disabled", message);
  return new AskChefError("failed", message);
}

// Runs one turn and calls onEvent for each event until done or error.
// Resolves without error when the signal aborts.
export async function streamTurn(
  request: TurnRequest,
  onEvent: (event: AskChefEvent) => void,
  signal: AbortSignal
): Promise<void> {
  let response: Response;
  try {
    response = await fetch(`${apiBaseURL}/ask-chef/turns`, {
      method: "POST",
      credentials: "same-origin",
      cache: "no-store",
      signal,
      headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
      body: JSON.stringify(request),
    });
  } catch (err) {
    if (signal.aborted) return;
    throw err;
  }
  if (!response.ok) throw await readError(response);
  if (!response.body) throw new AskChefError("protocol", "Ask Chef returned no stream");

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  const parser = createAskChefStreamParser();
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      for (const event of parser.push(decoder.decode(value, { stream: true }))) {
        onEvent(event);
        if (event.type === "done" || event.type === "error") return;
      }
    }
    throw new AskChefError("protocol", "Connection closed before Ask Chef finished");
  } catch (err) {
    if (signal.aborted) return;
    throw err;
  } finally {
    await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}
