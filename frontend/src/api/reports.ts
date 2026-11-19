import { apiClient } from "./apiUtils";
import type { APIResponse } from "./types";

// Scheduled reports (RFC 0008). Mirrors internal/core/reports.go and
// pkg/models/reports.go, which stay the authority.

export type ReportFrequency = "hourly" | "daily" | "weekly" | "monthly";

/** A field the frequency does not use must be absent: the server rejects it. */
export interface ReportSchedule {
  frequency: ReportFrequency;
  minute: number;
  hour?: number;
  /** 0 to 6, Sunday is 0. Weekly only. */
  weekdays?: number[];
  /** 1 to 28. Monthly only. */
  day_of_month?: number;
}

export type ReportRangeAnchor = "send_time" | "day_start";
export type ReportState = "active" | "paused";
export type ReportPauseReason = "" | "user" | "owner_inactive" | "failures";
export type ReportRunStatus = "running" | "sent" | "partial" | "skipped" | "failed" | "interrupted";
export type ReportRunTrigger = "schedule" | "manual";
export type ReportScope = "mine" | "received" | "all";

export interface ReportSourceRef {
  kind: "saved_query" | "dashboard";
  saved_query_id?: number;
  dashboard_id?: number;
  tab_id?: string;
}

/**
 * A user, team or address row. A team expands to its members when a report
 * runs, and an address that belongs to a user is that user.
 */
export type ReportRecipientInput = { kind: "user"; user_id: number } | { kind: "team"; team_id: number } | { kind: "email"; email: string };

/** A recipient row as an editor reads it, with the name it shows as. */
export type ReportRecipient = ReportRecipientInput & { name: string; email?: string };

/** The users and teams the caller may add as recipients, and whether external addresses are on. */
export interface ReportRecipientOptions {
  users: { id: number; name: string; email: string }[];
  teams: { id: number; name: string }[];
  /** Domains are lower case; "*" allows every domain. */
  external: { enabled: boolean; domains: string[] };
}

export interface ReportInput {
  name: string;
  source: ReportSourceRef;
  schedule: ReportSchedule;
  timezone: string;
  time_range: string;
  range_anchor: ReportRangeAnchor;
  variables: Record<string, string | string[]>;
  max_rows: number;
  skip_if_empty: boolean;
  attach_csv: boolean;
  /** An empty list sends the report to its owner alone. */
  recipients: ReportRecipientInput[];
}

export interface ReportWarning {
  code: "no_frontend_url" | "smtp_not_configured" | "panel_locked_for_owner";
  panel_id?: string;
}

export interface Report {
  id: number;
  name: string;
  source: ReportSourceRef & { name: string };
  schedule: ReportSchedule;
  timezone: string;
  time_range: string;
  range_anchor: ReportRangeAnchor;
  variables: Record<string, unknown>;
  max_rows: number;
  skip_if_empty: boolean;
  attach_csv: boolean;
  /** Set on one report read by its editor; a list leaves it out. */
  recipients?: ReportRecipient[];
  owner: { id: number; name: string; email: string } | null;
  state: ReportState;
  pause_reason: ReportPauseReason;
  /** The next tick; null for a paused report, one without an owner, and a recipient's reduced view. */
  next_run_at: string | null;
  last_run: { id: number; status: ReportRunStatus; reason: string; started_at: string; finished_at?: string } | null;
  can_edit: boolean;
  warnings: ReportWarning[];
  created_at: string;
  updated_at: string;
}

export interface ReportPanelOutcome {
  panel_id: string;
  title: string;
  status: "ok" | "empty" | "locked" | "failed" | "unsupported";
  rows: number;
  error?: string;
}

export interface ReportDelivery {
  class: "user" | "external";
  target: string;
  via: string;
  status: "sent" | "failed" | "skipped" | "unknown";
  attempts: number;
  reason?: string;
}

export interface ReportRun {
  id: number;
  report_id: number;
  trigger: ReportRunTrigger;
  scheduled_for: string;
  actor_id?: number;
  status: ReportRunStatus;
  reason: string;
  window_start?: string;
  window_end?: string;
  panels: ReportPanelOutcome[];
  deliveries: ReportDelivery[];
  started_at: string;
  finished_at?: string;
}

export interface ReportPreview {
  subject: string;
  html: string;
}

export const reportsApi = {
  list: (scope: ReportScope) => apiClient.get<Report[]>(`/reports?scope=${scope}`),
  get: (id: number) => apiClient.get<Report>(`/reports/${id}`),
  create: (input: ReportInput) => apiClient.post<Report>("/reports", input),
  update: (id: number, input: ReportInput) => apiClient.put<Report>(`/reports/${id}`, input),
  // A delete answers 204 with no body; a failure rejects with its envelope.
  remove: async (id: number): Promise<APIResponse<null>> =>
    (await apiClient.delete<null>(`/reports/${id}`)) || { status: "success", data: null },
  pause: (id: number) => apiClient.post<Report>(`/reports/${id}/pause`),
  resume: (id: number) => apiClient.post<Report>(`/reports/${id}/resume`),
  claim: (id: number) => apiClient.post<Report>(`/reports/${id}/claim`),
  run: (id: number, recipients: "me" | "all") => apiClient.post<ReportRun>(`/reports/${id}/run`, { recipients }),
  preview: (id: number) => apiClient.post<ReportPreview>(`/reports/${id}/preview`),
  recipientOptions: () => apiClient.get<ReportRecipientOptions>("/reports/recipient-options"),
  // Leaving answers 204 with no body; a failure rejects with its envelope.
  leave: async (id: number): Promise<APIResponse<null>> =>
    (await apiClient.delete<null>(`/reports/${id}/recipients/me`)) || { status: "success", data: null },
  runs: (id: number, before = 0, limit = 20) => apiClient.get<ReportRun[]>(`/reports/${id}/runs?before=${before}&limit=${limit}`),
};
