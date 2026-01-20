import { apiClient } from "./apiUtils";

export interface SystemSetting {
  key: string;
  value: string;
  value_type: "string" | "number" | "boolean" | "duration";
  category: "alerts" | "ai" | "auth" | "server";
  description?: string;
  is_sensitive: boolean;
  masked_value?: string;
  created_at: string;
  updated_at: string;
}

export interface SettingsByCategory {
  category: string;
  settings: SystemSetting[];
}

export interface UpdateSettingRequest {
  value: string;
  value_type: "string" | "number" | "boolean" | "duration";
  category: "alerts" | "ai" | "auth" | "server";
  description: string;
  is_sensitive: boolean;
}

export interface TestEmailRequest {
  recipient_email?: string;
}

export interface TestWebhookRequest {
  webhook_url: string;
}

export interface TestNotificationResponse {
  message: string;
  recipient?: string;
  url?: string;
}

export const settingsApi = {
  listSettings: () => apiClient.get<SettingsByCategory[]>("/admin/settings"),

  listSettingsByCategory: (category: string) =>
    apiClient.get<SystemSetting[]>(`/admin/settings/category/${category}`),

  getSetting: (key: string) =>
    apiClient.get<{ key: string; value: string }>(`/admin/settings/${key}`),

  updateSetting: (key: string, data: UpdateSettingRequest) =>
    apiClient.put<{ message: string; key: string }>(`/admin/settings/${key}`, data),

  deleteSetting: (key: string) =>
    apiClient.delete<{ message: string }>(`/admin/settings/${key}`),

  testEmail: (data?: TestEmailRequest) =>
    apiClient.post<TestNotificationResponse>('/admin/settings/test-email', data || {}),

  testWebhook: (data: TestWebhookRequest) =>
    apiClient.post<TestNotificationResponse>('/admin/settings/test-webhook', data),
};
