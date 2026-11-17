import { describe, expect, it } from "vitest";
import { createApp, h } from "vue";
import { i18n } from "@/i18n";
import AlertDeliveryStatus from "../AlertDeliveryStatus.vue";
import type { AlertDelivery, AlertHistoryEntry } from "@/api/alerts";

function delivery(fields: Partial<AlertDelivery>): AlertDelivery {
  return {
    destination_id: 1, destination_name: "On-call hook", destination_type: "webhook",
    event: "firing", status: "sent", attempts: 1, created_at: "2026-11-16T10:00:00Z", ...fields,
  };
}

function render(deliveries: AlertDelivery[] | undefined, inspect?: (element: HTMLDivElement) => void): string {
  const entry: AlertHistoryEntry = {
    id: 1, alert_id: 1, group_key: "", status: "triggered", triggered_at: "2026-11-16T10:00:00Z", created_at: "2026-11-16T10:00:00Z", deliveries,
  };
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
  it("shows nothing for sent deliveries and for entries without any", () => {
    expect(render([delivery({ status: "sent" })])).toBe("");
    expect(render(undefined)).toBe("");
    expect(render([])).toBe("");
  });
  it("shows a pending retry with its destination and last error", () => {
    const text = render([delivery({ status: "pending", attempts: 2, last_error: "webhook delivery failed: status 500" })]);
    expect(text).toContain("On-call hook");
    expect(text).toContain("Delivering");
    expect(text).toContain("status 500");
  });
  it("shows an email in its SMTP transaction as delivering", () => {
    expect(render([delivery({ status: "attempting", destination_type: "email" })])).toContain("Delivering");
  });
  it("shows a failed delivery with its error", () => {
    const text = render([delivery({ status: "failed", last_error: "webhook delivery failed: status 400" })]);
    expect(text).toContain("Delivery failed");
    expect(text).toContain("status 400");
  });
  it("shows an unknown outcome as interrupted", () => {
    expect(render([delivery({ status: "unknown", last_error: "interrupted during SMTP" })])).toContain("Delivery interrupted");
  });
  it("names an external email recipient and hides a user id", () => {
    const text = render([
      delivery({ status: "failed", destination_name: "Mail", recipient: "oncall@example.com" }),
      delivery({ status: "failed", destination_name: "Mail", recipient: "user:3" }),
    ]);
    expect(text).toContain("Mail (oncall@example.com)");
    expect(text).not.toContain("user:3");
  });
  it("lists only the deliveries that are not sent", () => {
    render([delivery({ status: "sent", destination_name: "Done" }), delivery({ status: "failed", destination_name: "Broken" })], (element) => {
      expect(element.querySelectorAll("li")).toHaveLength(1);
      expect(element.textContent).not.toContain("Done");
    });
  });
  it("keeps the line breaks of an error", () => {
    render([delivery({ status: "failed", last_error: "first\nsecond" })], (element) => {
      expect(element.querySelector("p")?.classList.contains("whitespace-pre-line")).toBe(true);
    });
  });
});
