import { describe, expect, it } from "vitest";
import { createApp, h } from "vue";
import { i18n } from "@/i18n";
import AlertDeliveryStatus from "../AlertDeliveryStatus.vue";
import { alertDeliveryFailure } from "@/stores/alertHistory";
import type { AlertHistoryEntry } from "@/api/alerts";

function historyEntry(payload: Record<string, unknown>): AlertHistoryEntry {
  return { id: 1, alert_id: 1, status: "triggered", triggered_at: "2026-01-01T00:00:00Z", created_at: "2026-01-01T00:00:00Z", payload };
}

function render(entry: AlertHistoryEntry): string {
  const element = document.createElement("div");
  const app = createApp({ render: () => h(AlertDeliveryStatus, { failure: alertDeliveryFailure(entry) }) });
  app.use(i18n);
  app.mount(element);
  const text = element.textContent ?? "";
  app.unmount();
  return text;
}

describe("alert history delivery status", () => {
  it("shows the stored failure and exhausted retries", () => {
    const text = render(historyEntry({ delivery_failed: true, delivery_error: "webhook 2: status 500", delivery_retries_exhausted: true }));
    expect(text).toContain("Delivery failed");
    expect(text).toContain("webhook 2: status 500");
    expect(text).toContain("Delivery retries exhausted");
  });
  it("shows exhausted retries on older history rows", () => {
    expect(render(historyEntry({ delivery_failed: true, delivery_attempts: 3 }))).toContain("Delivery retries exhausted");
  });
  it("shows a stored error without a failure flag", () => {
    expect(render(historyEntry({ delivery_error: "request timed out" }))).toContain("request timed out");
  });
  it("hides exhaustion while retries remain and hides successful delivery", () => {
    expect(render(historyEntry({ delivery_failed: true, delivery_error: "request timed out", delivery_attempts: 1 }))).not.toContain("Delivery retries exhausted");
    expect(render(historyEntry({ delivery_failed: false }))).toBe("");
  });
});
