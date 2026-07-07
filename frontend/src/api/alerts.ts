import { apiClient } from "./apiUtils";
import type { AlertEditorMode, QueryLanguage } from "@/lib/queryMetadata";

export type AlertThresholdOperator = "gt" | "gte" | "lt" | "lte" | "eq" | "neq";
export type AlertSeverity = "info" | "warning" | "critical";
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
  recipient_user_ids: number[];
  webhook_urls: string[];
  is_active: boolean;
  last_state: "firing" | "resolved";
  last_evaluated_at?: string | null;
  last_triggered_at?: string | null;
  created_by?: number | null;
  created_at: string;
  updated_at: string;
}

export interface AlertHistoryEntry {
  id: number;
  alert_id: number;
  status: "triggered" | "resolved" | "error";
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
  recipient_user_ids?: number[];
  webhook_urls?: string[];
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
  recipient_user_ids?: number[];
  webhook_urls?: string[];
  is_active?: boolean;
}

export interface ResolveAlertRequest {
  message?: string;
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
}

export interface TestAlertQueryResponse {
  value: number;
  threshold_met: boolean;
  execution_time_ms: number;
  rows_returned: number;
  warnings: string[];
}

export const alertsApi = {
  list: (sourceId?: number) => {
    const url =
      sourceId !== undefined && sourceId !== null
        ? `/alerts?source_id=${sourceId}`
        : "/alerts";
    return apiClient.get<Alert[]>(url);
  },
  get: (alertId: number) =>
    apiClient.get<Alert>(`/alerts/${alertId}`),
  create: (payload: CreateAlertRequest) =>
    apiClient.post<Alert>("/alerts", payload),
  update: (alertId: number, payload: UpdateAlertRequest) =>
    apiClient.put<Alert>(`/alerts/${alertId}`, payload),
  delete: (alertId: number) =>
    apiClient.delete<{ message: string }>(`/alerts/${alertId}`),
  resolve: (alertId: number, payload: ResolveAlertRequest) =>
    apiClient.post<{ message: string }>(`/alerts/${alertId}/resolve`, payload),
  history: (alertId: number, limit?: number) => {
    const search = limit ? `?limit=${encodeURIComponent(limit)}` : "";
    return apiClient.get<AlertHistoryEntry[]>(`/alerts/${alertId}/history${search}`);
  },
  testQuery: (payload: TestAlertQueryRequest) =>
    apiClient.post<TestAlertQueryResponse>("/alerts/test", payload),
};
