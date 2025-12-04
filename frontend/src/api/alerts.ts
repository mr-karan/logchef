import { apiClient } from "./apiUtils";

export type AlertThresholdOperator = "gt" | "gte" | "lt" | "lte" | "eq" | "neq";
export type AlertSeverity = "info" | "warning" | "critical";
export type AlertQueryType = "sql" | "condition";
export interface Alert {
  id: number;
  team_id: number;
  source_id: number;
  name: string;
  description?: string;
  query_type: AlertQueryType;
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
  is_active: boolean;
  last_state: "firing" | "resolved";
  last_evaluated_at?: string | null;
  last_triggered_at?: string | null;
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
  name: string;
  description?: string;
  query_type: AlertQueryType;
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
  is_active: boolean;
}

export interface UpdateAlertRequest {
  name?: string;
  description?: string;
  query_type?: AlertQueryType;
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
  is_active?: boolean;
}

export interface ResolveAlertRequest {
  message?: string;
}

export interface TestAlertQueryRequest {
  query: string;
  query_type: AlertQueryType;
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
  listAlerts: (teamId: number, sourceId: number) =>
    apiClient.get<Alert[]>(`/teams/${teamId}/sources/${sourceId}/alerts`),
  getAlert: (teamId: number, sourceId: number, alertId: number) =>
    apiClient.get<Alert>(`/teams/${teamId}/sources/${sourceId}/alerts/${alertId}`),
  createAlert: (teamId: number, sourceId: number, payload: CreateAlertRequest) =>
    apiClient.post<Alert>(`/teams/${teamId}/sources/${sourceId}/alerts`, payload),
  updateAlert: (teamId: number, sourceId: number, alertId: number, payload: UpdateAlertRequest) =>
    apiClient.put<Alert>(`/teams/${teamId}/sources/${sourceId}/alerts/${alertId}`, payload),
  deleteAlert: (teamId: number, sourceId: number, alertId: number) =>
    apiClient.delete<{ message: string }>(`/teams/${teamId}/sources/${sourceId}/alerts/${alertId}`),
  resolveAlert: (teamId: number, sourceId: number, alertId: number, payload: ResolveAlertRequest) =>
    apiClient.post<{ message: string }>(`/teams/${teamId}/sources/${sourceId}/alerts/${alertId}/resolve`, payload),
  listAlertHistory: (teamId: number, sourceId: number, alertId: number, limit?: number) => {
    const search = limit ? `?limit=${encodeURIComponent(limit)}` : "";
    return apiClient.get<AlertHistoryEntry[]>(
      `/teams/${teamId}/sources/${sourceId}/alerts/${alertId}/history${search}`
    );
  },
  testAlertQuery: (teamId: number, sourceId: number, payload: TestAlertQueryRequest) =>
    apiClient.post<TestAlertQueryResponse>(`/teams/${teamId}/sources/${sourceId}/alerts/test`, payload),
};
