import { defineStore } from "pinia";
import { computed, ref, watch } from "vue";
import {
  AskChefError,
  evidenceNumber,
  streamTurn,
  type AskChefEvent,
  type DoneEvent,
  type EvidenceId,
  type EvidenceRecord,
  type HistoryMessage,
  type TurnRequest,
} from "@/api/askChef";
import type { Alert, AlertHistoryEntry, AlertThresholdOperator } from "@/api/alerts";
import { i18n } from "@/i18n";
import router from "@/router";
import { alertHistoryWindow, evidenceExploreQuery, type ExploreWindow } from "@/utils/alertExplore";
import type { ZoneId } from "@/utils/timeWindow";
import { useAuthStore } from "./auth";
import { useExploreStore } from "./explore";
import { usePreferencesStore } from "./preferences";
import { useTeamsStore } from "./teams";

const MAX_HISTORY_ITEMS = 20;
const MAX_HISTORY_TEXT = 8000;
const MAX_EVIDENCE = 50;
export const MAX_MESSAGE_LENGTH = 4000;

export type AskChefSubject =
  | { kind: "explorer" }
  | {
      kind: "alert";
      alertId: number;
      alertHistoryId: number;
      alertName: string;
      triggeredAt: string;
      value: number | null;
      thresholdOperator: AlertThresholdOperator;
      thresholdValue: number;
    };

// `zone` is the zone the context's times are shown in.
export type AskChefContext = ExploreWindow & { subject: AskChefSubject; zone: ZoneId };

export type ToolStep = { callId: string; tool: string } & (
  | { status: "running" }
  | { status: "ok" | "failed"; summary: string }
);

export interface UserTurn {
  role: "user";
  text: string;
}

export interface AssistantTurn {
  role: "assistant";
  steps: ToolStep[];
  text: string;
  evidenceIds: EvidenceId[];
  done: DoneEvent | null;
}

export type Turn = UserTurn | AssistantTurn;

export type AskChefStatus =
  | { kind: "idle" }
  | { kind: "streaming" }
  | { kind: "error"; message: string }
  | { kind: "disabled" };

export interface AskChefSession {
  transcript: Turn[];
  evidence: Map<EvidenceId, EvidenceRecord>;
  status: AskChefStatus;
}

export function newSession(): AskChefSession {
  return { transcript: [], evidence: new Map(), status: { kind: "idle" } };
}

function currentAssistantTurn(session: AskChefSession): AssistantTurn | null {
  const last = session.transcript[session.transcript.length - 1];
  return last?.role === "assistant" ? last : null;
}

// Applies one stream event to the turn in progress.
export function applyAskChefEvent(session: AskChefSession, event: AskChefEvent): void {
  const turn = currentAssistantTurn(session);
  if (!turn) return;
  switch (event.type) {
    case "tool_start":
    case "tool_end": {
      const step: ToolStep =
        event.type === "tool_start"
          ? { callId: event.call_id, tool: event.tool, status: "running" }
          : {
              callId: event.call_id,
              tool: turn.steps.find((s) => s.callId === event.call_id)?.tool ?? "",
              status: event.ok ? "ok" : "failed",
              summary: event.summary,
            };
      const index = turn.steps.findIndex((s) => s.callId === step.callId);
      if (index === -1) turn.steps.push(step);
      else turn.steps[index] = step;
      if (event.type === "tool_end") {
        for (const record of event.evidence) {
          session.evidence.set(record.id, record);
          if (!turn.evidenceIds.includes(record.id)) turn.evidenceIds.push(record.id);
        }
      }
      return;
    }
    case "text":
      turn.text += event.delta;
      return;
    case "done":
      turn.done = event;
      session.status = { kind: "idle" };
      return;
    case "error":
      session.status = { kind: "error", message: event.message };
      return;
  }
}

// History holds only finished exchanges, so it always opens with a user message.
function historyFrom(transcript: Turn[]): HistoryMessage[] {
  const history: HistoryMessage[] = [];
  for (let i = 0; i + 1 < transcript.length; i++) {
    const question = transcript[i];
    const answer = transcript[i + 1];
    if (question.role !== "user" || answer.role !== "assistant" || !answer.text.trim()) continue;
    history.push(
      { role: "user", text: question.text.slice(0, MAX_HISTORY_TEXT) },
      { role: "assistant", text: answer.text.slice(0, MAX_HISTORY_TEXT) }
    );
  }
  return history.slice(-MAX_HISTORY_ITEMS);
}

export function buildTurnRequest(context: AskChefContext, session: AskChefSession, message: string): TurnRequest {
  const { subject, teamId, sourceId, start, end, zone, mode, query } = context;
  const window = { start, end, ...(mode ? { mode } : {}), ...(query ? { query } : {}) };
  return {
    team_id: teamId,
    source_id: sourceId,
    timezone: zone,
    context:
      subject.kind === "alert"
        ? { kind: "alert", ...window, alert_id: subject.alertId, alert_history_id: subject.alertHistoryId }
        : { kind: "explorer", ...window },
    history: historyFrom(session.transcript),
    evidence: [...session.evidence.values()]
      .sort((a, b) => evidenceNumber(a.id) - evidenceNumber(b.id))
      .slice(-MAX_EVIDENCE),
    message,
  };
}

function failureStatus(err: unknown): AskChefStatus {
  if (err instanceof AskChefError) {
    if (err.reason === "disabled") return { kind: "disabled" };
    return { kind: "error", message: err.message };
  }
  return { kind: "error", message: i18n.global.t("askChef.failed") };
}

export const useAskChefStore = defineStore("askChef", () => {
  const open = ref(false);
  // An alert investigation keeps the alert's window. Otherwise the context is
  // whatever the Explorer shows, read again for every turn.
  const pinned = ref<AskChefContext | null>(null);
  const context = computed(() => pinned.value ?? explorerContext());
  const session = ref<AskChefSession>(newSession());
  const activeEvidenceId = ref<EvidenceId | null>(null);
  let controller: AbortController | null = null;

  const status = computed(() => session.value.status);
  const transcript = computed(() => session.value.transcript);
  const evidence = computed(() => session.value.evidence);

  function explorerContext(): AskChefContext | null {
    const explore = useExploreStore();
    const teamId = useTeamsStore().currentTeamId;
    if (!teamId || !explore.sourceId || !explore.hasWindow) return null;
    const mode = explore.activeMode;
    const query = (mode === "native" ? explore.nativeQuery : explore.logchefqlCode).trim();
    // The window the Explorer's log query reads.
    const { start, end, zone } = explore.resolved;
    return {
      subject: { kind: "explorer" },
      teamId,
      sourceId: explore.sourceId,
      start,
      end,
      zone,
      mode,
      ...(query ? { query } : {}),
    };
  }

  function abortTurn() {
    controller?.abort();
    controller = null;
  }

  function clearConversation() {
    abortTurn();
    session.value = newSession();
    activeEvidenceId.value = null;
  }

  // Resumes the current conversation, or starts one from what the Explorer shows.
  function openForExplorer() {
    if (!session.value.transcript.length) pinned.value = null;
    open.value = true;
  }

  function close() {
    open.value = false;
  }

  async function send(text: string): Promise<void> {
    const message = text.trim().slice(0, MAX_MESSAGE_LENGTH);
    const turnContext = pinned.value ?? explorerContext();
    if (!message || !turnContext || session.value.status.kind === "streaming") return;

    const request = buildTurnRequest(turnContext, session.value, message);
    session.value.transcript.push({ role: "user", text: message }, { role: "assistant", steps: [], text: "", evidenceIds: [], done: null });
    session.value.status = { kind: "streaming" };
    const turn = new AbortController();
    controller = turn;
    try {
      await streamTurn(
        request,
        (event) => {
          if (controller === turn) applyAskChefEvent(session.value, event);
        },
        turn.signal
      );
    } catch (err) {
      if (controller !== turn) return;
      session.value.status = failureStatus(err);
      const last = currentAssistantTurn(session.value);
      if (last && !last.steps.length && !last.text) session.value.transcript.pop();
    } finally {
      if (controller === turn) controller = null;
    }
  }

  function investigateAlert(alert: Alert, entry: AlertHistoryEntry, teamId: number) {
    clearConversation();
    pinned.value = {
      ...alertHistoryWindow(alert, entry, teamId),
      zone: usePreferencesStore().zone,
      subject: {
        kind: "alert",
        alertId: alert.id,
        alertHistoryId: entry.id,
        alertName: alert.name,
        triggeredAt: entry.triggered_at,
        value: entry.value ?? null,
        thresholdOperator: alert.threshold_operator,
        thresholdValue: alert.threshold_value,
      },
    };
    open.value = true;
    void send(i18n.global.t("askChef.whyDidThisFire"));
  }

  function stop() {
    abortTurn();
    if (session.value.status.kind === "streaming") session.value.status = { kind: "idle" };
  }

  function reset() {
    clearConversation();
    pinned.value = null;
  }

  // Evidence cites one source, so a conversation ends when the Explorer moves
  // to another team or source. A team switch clears the source first, so
  // compare with the last source that was set.
  let lastSource: string | null = null;
  watch(
    () => {
      const explore = useExploreStore();
      const teamId = useTeamsStore().currentTeamId;
      return teamId && explore.sourceId ? `${teamId}:${explore.sourceId}` : null;
    },
    (next) => {
      if (!next) return;
      if (!pinned.value && lastSource && next !== lastSource) clearConversation();
      lastSource = next;
    },
    { immediate: true }
  );

  // A conversation belongs to the user who had it, so logout or a new login ends it.
  watch(
    () => useAuthStore().user?.id,
    () => {
      clearConversation();
      pinned.value = null;
      open.value = false;
    },
    { flush: "sync" }
  );

  function openEvidence(id: EvidenceId) {
    const record = session.value.evidence.get(id);
    if (!record) return;
    activeEvidenceId.value = id;
    void router.push({ name: "LogExplorer", query: evidenceExploreQuery(record) });
  }

  return {
    open,
    context,
    status,
    transcript,
    evidence,
    activeEvidenceId,
    openForExplorer,
    close,
    send,
    investigateAlert,
    stop,
    reset,
    openEvidence,
  };
});
