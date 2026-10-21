import { createSSEParser, type SSEEvent } from "@/lib/sse";
import { showErrorToast } from "@/api/error-handler";
import type { APIErrorResponse } from "@/api/types";
import { useAuthStore } from "@/stores/auth";
import router from "@/router";

export type StreamOutcome = "stop" | "eof" | "aborted" | "no-body";

export interface StreamRequest {
  method: "GET" | "POST";
  body?: unknown;
  signal: AbortSignal;
  label: string;
  onOpen?: (headers: Headers) => void;
}

export class StreamHTTPError extends Error {
  constructor(public status: number, public apiError: APIErrorResponse) {
    super(apiError.message);
    this.name = "StreamHTTPError";
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

async function readAPIError(response: Response, label: string): Promise<APIErrorResponse> {
  const body: unknown = await response.json().catch(() => null);
  const fallback = `${label} failed (${response.status})`;
  if (!isRecord(body)) return { status: "error", message: fallback, error_type: "HTTPError" };
  return {
    status: "error",
    message: typeof body.message === "string" && body.message ? body.message : fallback,
    error_type: typeof body.error_type === "string" ? body.error_type : "HTTPError",
    data: body.data,
  };
}

// Matches the axios interceptor's 401 flow, which fetch-based streams bypass.
function expireSession(): void {
  useAuthStore().clearState();
  const isOnLoginPage = window.location.pathname.includes("/login") || router.currentRoute.value.name === "Login";
  if (!isOnLoginPage) {
    showErrorToast({ status: "error", message: "Your session has expired. Please log in again.", error_type: "AuthenticationError" });
  }
  router.push({ name: "Login", query: { redirect: router.options.history.location }, replace: true });
}

export async function streamSSE(
  url: string,
  request: StreamRequest,
  onFrame: (frame: SSEEvent) => "stop" | void
): Promise<StreamOutcome> {
  const { signal } = request;
  const headers: Record<string, string> = { Accept: "text/event-stream" };
  if (request.body !== undefined) headers["Content-Type"] = "application/json";

  try {
    const response = await fetch(url, {
      method: request.method,
      credentials: "same-origin",
      cache: "no-store",
      signal,
      headers,
      body: request.body === undefined ? undefined : JSON.stringify(request.body),
    });

    if (!response.ok) {
      const apiError = await readAPIError(response, request.label);
      if (signal.aborted) return "aborted";
      if (response.status === 401) expireSession();
      throw new StreamHTTPError(response.status, apiError);
    }
    if (!response.body) return signal.aborted ? "aborted" : "no-body";

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    const parser = createSSEParser();
    try {
      request.onOpen?.(response.headers);
      if (signal.aborted) return "aborted";
      while (!signal.aborted) {
        const { done, value } = await reader.read();
        if (signal.aborted) return "aborted";
        if (done) return "eof";
        for (const frame of parser.push(decoder.decode(value, { stream: true }))) {
          if (signal.aborted) return "aborted";
          if (onFrame(frame) === "stop") return "stop";
        }
      }
      return "aborted";
    } finally {
      await reader.cancel().catch(() => undefined);
      reader.releaseLock();
    }
  } catch (err) {
    if (signal.aborted) return "aborted";
    throw err;
  }
}
