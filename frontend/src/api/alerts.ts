import { apiClient, type RequestOptions } from "./apiUtils";
import type { AlertEditorMode, QueryLanguage } from "@/lib/queryMetadata";

export type AlertThresholdOperator = "gt" | "gte" | "lt" | "lte" | "eq" | "neq";
export type AlertSeverity = "info" | "warning" | "critical";
/** What a group without a row in the result means. */
export type AlertNoDataPolicy = "zero" | "no_data" | "keep";

export interface AlertInstanceCounts {
  firing: number;
  no_data: number;
  ok: number;
}

export interface Alert {
  id: number;
  source_id: number;
  name: string;
  description?: string;
  query_language: QueryLanguage;
  editor_mode: AlertEditorMode;
  query: string;
  condition_json?: string;
  lookback_seconds: number;
  threshold_operator: AlertThresholdOperator;
  threshold_value: number;
  frequency_seconds: number;
  severity: AlertSeverity;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  generator_url?: string;
  destination_ids: number[];
  group_by?: string[] | null;
  no_data_policy: AlertNoDataPolicy;
  notify_on_resolve: boolean;
  instance_counts?: AlertInstanceCounts;
  is_active: boolean;
  last_state: "firing" | "resolved" | "no_data";
  last_evaluated_at?: string | null;
  last_triggered_at?: string | null;
  created_by?: number | null;
  created_at: string;
  updated_at: string;
}

export interface AlertInstance {
  alert_id: number;
  group_key: string;
  group: Record<string, string>;
  state: "ok" | "firing" | "no_data";
  value?: number | null;
  state_since: string;
  last_seen_at?: string | null;
  history_id?: number | null;
}

export interface AlertHistoryEntry {
  id: number;
  alert_id: number;
  group_key: string;
  group?: Record<string, string>;
  status: "triggered" | "resolved" | "error" | "no_data";
  triggered_at: string;
  resolved_at?: string | null;
  value?: number | null;
  payload?: Record<string, unknown>;
  message?: string | null;
  created_at: string;
}

export interface CreateAlertRequest {
  source_id: number;
  name: string;
  description?: string;
  query_language?: QueryLanguage;
  editor_mode?: AlertEditorMode;
  query: string;
  condition_json?: string;
  lookback_seconds: number;
  threshold_operator: AlertThresholdOperator;
  threshold_value: number;
  frequency_seconds: number;
  severity: AlertSeverity;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  generator_url?: string;
  destination_ids?: number[];
  group_by?: string[];
  no_data_policy?: AlertNoDataPolicy;
  notify_on_resolve?: boolean;
  is_active: boolean;
}

export interface UpdateAlertRequest {
  name?: string;
  description?: string;
  query_language?: QueryLanguage;
  editor_mode?: AlertEditorMode;
  query?: string;
  condition_json?: string;
  lookback_seconds?: number;
  threshold_operator?: AlertThresholdOperator;
  threshold_value?: number;
  frequency_seconds?: number;
  severity?: AlertSeverity;
  labels?: Record<string, string>;
  annotations?: Record<string, string>;
  generator_url?: string;
  destination_ids?: number[];
  group_by?: string[];
  no_data_policy?: AlertNoDataPolicy;
  notify_on_resolve?: boolean;
  is_active?: boolean;
}

export interface ResolveAlertRequest {
  message?: string;
  /** Resolves one group; omitted resolves every group. */
  group_key?: string;
}

export interface TestAlertQueryRequest {
  source_id: number;
  query: string;
  query_language?: QueryLanguage;
  editor_mode?: AlertEditorMode;
  condition_json?: string;
  lookback_seconds?: number;
  threshold_operator: AlertThresholdOperator;
  threshold_value: number;
  group_by?: string[];
  no_data_policy?: AlertNoDataPolicy;
}

export interface AlertSample {
  group: Record<string, string>;
  value: number;
  threshold_met: boolean;
}

export interface TestAlertQueryResponse {
  value: number;
  threshold_met: boolean;
  execution_time_ms: number;
  rows_returned: number;
  warnings: string[];
  /** Grouped rules only: at most 20 samples, out of groups_total. */
  samples: AlertSample[];
  groups_total: number;
  /** The result had no row; the rule's no-data policy decides what that means. */
  no_data: boolean;
}

export const alertsApi = {
  list: (sourceId?: number, options?: RequestOptions) => {
    const url =
      sourceId !== undefined && sourceId !== null
        ? `/alerts?source_id=${sourceId}`
        : "/alerts";
    return apiClient.get<Alert[]>(url, options);
  },
  get: (alertId: number, options?: RequestOptions) =>
    apiClient.get<Alert>(`/alerts/${alertId}`, options),
  create: (payload: CreateAlertRequest) =>
    apiClient.post<Alert>("/alerts", payload),
  update: (alertId: number, payload: UpdateAlertRequest) =>
    apiClient.put<Alert>(`/alerts/${alertId}`, payload),
  delete: (alertId: number) =>
    apiClient.delete<{ message: string }>(`/alerts/${alertId}`),
  resolve: (alertId: number, payload: ResolveAlertRequest) =>
    apiClient.post<{ message: string }>(`/alerts/${alertId}/resolve`, payload),
  history: (alertId: number, limit?: number, options?: RequestOptions) => {
    const search = limit ? `?limit=${encodeURIComponent(limit)}` : "";
    return apiClient.get<AlertHistoryEntry[]>(`/alerts/${alertId}/history${search}`, options);
  },
  instances: (alertId: number, options?: RequestOptions) =>
    apiClient.get<AlertInstance[]>(`/alerts/${alertId}/instances`, options),
  testQuery: (payload: TestAlertQueryRequest) =>
    apiClient.post<TestAlertQueryResponse>("/alerts/test", payload),
};
