import { apiClient } from "./apiUtils";

export type DestinationType = "email" | "webhook" | "slack" | "pagerduty";
export type DestinationScope = "private" | "team" | "global";

export interface DestinationConfig {
  user_ids?: number[];
  addresses?: string[];
  payload_version?: number;
}

export interface NotificationDestination {
  id: number;
  name: string;
  type: DestinationType;
  scope: DestinationScope;
  team_id?: number;
  config: DestinationConfig;
  has_secret: boolean;
  secret_hint?: string;
  is_active: boolean;
  created_by?: number;
  created_at: string;
  updated_at: string;
}

// A blank secret on update keeps the stored one.
export interface DestinationRequest {
  name: string;
  type: DestinationType;
  scope: DestinationScope;
  team_id?: number;
  config: DestinationConfig;
  secret: string;
  is_active?: boolean;
}

export interface DestinationTestResult {
  target: string;
  ok: boolean;
  error?: string;
}

export interface DestinationTestResponse {
  ok: boolean;
  results: DestinationTestResult[];
}

export const notificationDestinationsApi = {
  list: () => apiClient.get<NotificationDestination[]>("/notification-destinations"),
  create: (payload: DestinationRequest) =>
    apiClient.post<NotificationDestination>("/notification-destinations", payload),
  update: (id: number, payload: DestinationRequest) =>
    apiClient.put<NotificationDestination>(`/notification-destinations/${id}`, payload),
  delete: (id: number) =>
    apiClient.delete<{ message: string }>(`/notification-destinations/${id}`),
  test: (payload: { id: number } | DestinationRequest) =>
    apiClient.post<DestinationTestResponse>("/notification-destinations/test", payload),
};
