import { apiClient, type RequestOptions } from "./apiUtils";

export type SilenceScheduleKind = "once" | "weekly";

/** A weekly window: it starts at `start` (HH:MM in `timezone`) on each weekday (0 is Sunday). */
export interface SilenceWeeklyWindow {
  weekdays: number[];
  start: string;
  duration_minutes: number;
  timezone: string;
}

export interface AlertSilence {
  id: number;
  source_id: number;
  /** Absent means every rule of the source. */
  alert_id?: number | null;
  matchers: Record<string, string>;
  comment: string;
  schedule_kind: SilenceScheduleKind;
  starts_at?: string | null;
  ends_at?: string | null;
  weekly?: SilenceWeeklyWindow | null;
  created_by?: number | null;
  created_at: string;
  updated_at: string;
  /** Computed by the server when it lists the silence. */
  active: boolean;
}

export interface AlertSilenceRequest {
  source_id: number;
  alert_id: number | null;
  matchers: Record<string, string>;
  comment: string;
  schedule_kind: SilenceScheduleKind;
  starts_at: string | null;
  ends_at: string | null;
  weekly: SilenceWeeklyWindow | null;
}

export const silencesApi = {
  list: (sourceId: number, options?: RequestOptions) =>
    apiClient.get<AlertSilence[]>(`/alert-silences?source_id=${sourceId}`, options),
  create: (payload: AlertSilenceRequest) => apiClient.post<AlertSilence>("/alert-silences", payload),
  update: (id: number, payload: AlertSilenceRequest) => apiClient.put<AlertSilence>(`/alert-silences/${id}`, payload),
  delete: (id: number) => apiClient.delete<{ message: string }>(`/alert-silences/${id}`),
};
