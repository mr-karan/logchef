import { describe, expect, it, vi } from "vitest";
import { createApp, h, nextTick } from "vue";
import { i18n } from "@/i18n";
import AlertDeliveryStatus from "../AlertDeliveryStatus.vue";
import type { AlertHistoryEntry } from "@/api/alerts";

function historyEntry(payload: Record<string, unknown>): AlertHistoryEntry {
  return { id: 1, alert_id: 1, status: "triggered", triggered_at: new Date().toISOString(), created_at: new Date().toISOString(), payload };
}

function render(entry: AlertHistoryEntry, inspect?: (element: HTMLDivElement) => void): string {
  const element = document.createElement("div");
  const app = createApp({ render: () => h(AlertDeliveryStatus, { entry }) });
  app.use(i18n);
  app.mount(element);
  try {
    inspect?.(element);
    return element.textContent ?? "";
  } finally {
    app.unmount();
  }
}

describe("alert history delivery status", () => {
  it("shows a live retry using its deadline instead of the original trigger time", () => {
    const entry = historyEntry({ delivery_in_progress: true, delivery_deadline_at: new Date(Date.now() + 60_000).toISOString() });
    entry.triggered_at = new Date(Date.now() - 280_000).toISOString();
    expect(render(entry)).toContain("Delivering");
  });
  it("shows interrupted delivery after its attempt deadline", () => {
    const entry = historyEntry({ delivery_in_progress: true, delivery_deadline_at: new Date(Date.now() - 1_000).toISOString() });
    expect(render(entry)).toContain("Delivery interrupted");
  });
  it("runs no timer for finished rows", () => {
    vi.useFakeTimers();
    try {
      render(historyEntry({ delivery_in_progress: false }), () => {
        expect(vi.getTimerCount()).toBe(0);
      });
    } finally {
      vi.useRealTimers();
    }
  });
  it("shows interrupted delivery after the delivery window", () => {
    const entry = historyEntry({ delivery_in_progress: true, delivery_failed: false });
    entry.triggered_at = "2026-01-01T00:00:00Z";
    const text = render(entry);
    expect(text).toContain("Delivery interrupted");
    expect(text).not.toContain("Delivering");
  });
  it("expires the delivering badge while the page remains open", async () => {
    vi.useFakeTimers();
    const element = document.createElement("div");
    const entry = historyEntry({ delivery_in_progress: true });
    const app = createApp({ render: () => h(AlertDeliveryStatus, { entry }) });
    app.use(i18n);
    app.mount(element);
    try {
      expect(element.textContent).toContain("Delivering");
      await vi.advanceTimersByTimeAsync(276_000);
      await nextTick();
      expect(element.textContent).toContain("Delivery interrupted");
      expect(element.textContent).not.toContain("Delivering");
    } finally {
      app.unmount();
      vi.useRealTimers();
    }
  });
  it("shows delivery in progress", () => {
    expect(render(historyEntry({ delivery_in_progress: true, delivery_failed: false }))).toContain("Delivering");
  });
  it("hides delivery in progress after manual resolution", () => {
    const entry = historyEntry({ delivery_in_progress: true, delivery_failed: false });
    expect(render(entry)).toContain("Delivering");
    expect(render({ ...entry, status: "resolved" })).not.toContain("Delivering");
    expect(render({ ...entry, resolved_at: "2026-01-01T00:01:00Z" })).not.toContain("Delivering");
  });
  it("shows the stored failure and exhausted retries", () => {
    const text = render(historyEntry({ delivery_failed: true, delivery_error: "webhook 2: status 500", delivery_retries_exhausted: true }));
    expect(text).toContain("Delivery failed");
    expect(text).toContain("webhook 2: status 500");
    expect(text).toContain("Delivery retries exhausted");
  });
  it("shows removed targets as a delivery failure", () => {
    expect(render(historyEntry({ delivery_failed: true, delivery_error: "no pending notification targets remain" }))).toContain("no pending notification targets remain");
  });
  it("preserves the line breaks between target errors", () => {
    render(historyEntry({ delivery_failed: true, delivery_error: "webhook 1: status 500\nemail recipient 2: smtp request timed out" }), (element) => {
      expect(element.querySelector("p")?.classList.contains("whitespace-pre-line")).toBe(true);
    });
  });
  it("shows exhausted retries on older history rows", () => {
    expect(render(historyEntry({ delivery_failed: true, delivery_attempts: 3 }))).toContain("Delivery retries exhausted");
  });
  it("hides failures during delivery even with an earlier error", () => {
    expect(render(historyEntry({ delivery_error: "request timed out" }))).toBe("");
    expect(render(historyEntry({ delivery_failed: false, delivery_error: "request timed out" }))).toBe("");
  });
  it("hides exhaustion while retries remain and hides successful delivery", () => {
    expect(render(historyEntry({ delivery_failed: true, delivery_error: "request timed out", delivery_attempts: 1 }))).not.toContain("Delivery retries exhausted");
    expect(render(historyEntry({ delivery_failed: false }))).toBe("");
  });
});
