import { createApp, nextTick } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";

vi.mock("@/components/visualizations/SourceSparkline.vue", () => ({
  default: { template: "<div class='sparkline-stub' />" },
}));

import SourceInspectionActivity from "@/views/sources/components/SourceInspectionActivity.vue";

let host: HTMLDivElement | null = null;
let unmount: (() => void) | undefined;

async function render(props: Record<string, unknown>, onRetry = () => {}) {
  host = document.createElement("div");
  document.body.appendChild(host);
  const app = createApp(SourceInspectionActivity, { ...props, onRetry });
  app.use(i18n).mount(host);
  unmount = () => app.unmount();
  await nextTick();
  return host;
}

function buttonWithText(root: HTMLElement, text: string) {
  return Array.from(root.querySelectorAll("button")).find((button) => button.textContent?.includes(text));
}

afterEach(() => {
  unmount?.();
  unmount = undefined;
  host?.remove();
  host = null;
});

describe("SourceInspectionActivity", () => {
  it("renders loading text", async () => {
    const root = await render({ loading: true });

    expect(root.textContent).toContain(i18n.global.t("sources.loadingRecentActivity"));
  });

  it("renders explicit timeout and unavailable errors", async () => {
    const timeoutRoot = await render({ error: { message: "Activity request timed out", kind: "transient" } });
    expect(timeoutRoot.textContent).toContain("Activity request timed out");

    unmount?.();
    timeoutRoot.remove();
    const unavailableRoot = await render({ error: { message: "Recent activity unavailable", kind: "transient" } });
    expect(unavailableRoot.textContent).toContain("Recent activity unavailable");
  });

  it("emits retry when an error retry button is clicked", async () => {
    let retries = 0;
    const root = await render({ error: { message: "Activity request timed out", kind: "transient" } }, () => {
      retries++;
    });

    buttonWithText(root, "Retry")?.click();
    expect(retries).toBe(1);
  });

  it("renders the full-width activity chart with the day total", async () => {
    let retries = 0;
    const root = await render(
      {
        activity: {
          rows_1h: 1234,
          rows_24h: 5678,
          latest_ts: "2025-01-02T03:04:05Z",
          hourly_buckets: [],
        },
      },
      () => {
        retries++;
      },
    );

    expect(root.textContent).toContain("5,678");
    expect(root.querySelector(".sparkline-stub")).not.toBeNull();
    expect(root.querySelector("button")).toBeNull();
    expect(retries).toBe(0);
  });

  it("explains an empty day instead of rendering a blank chart", async () => {
    const root = await render({ activity: { rows_1h: 0, rows_24h: 0, latest_ts: null, hourly_buckets: [] } });
    expect(root.textContent).toContain(i18n.global.t("inspection.noRecentEvents"));
    expect(root.querySelector(".sparkline-stub")).toBeNull();
  });

  it("keeps Retry for a ClickHouse failure the backend did not mark unsupported", async () => {
    const root = await render({ source: { source_type: "clickhouse" }, error: { message: "Service Unavailable", kind: "transient" } });
    expect(root.textContent).not.toContain("sort key");
    expect(buttonWithText(root, "Retry")).toBeDefined();
  });

  it("shows a permanent failure without Retry", async () => {
    const root = await render({ error: { message: "Source not found", kind: "permanent" } });
    expect(root.textContent).toContain("Source not found");
    expect(buttonWithText(root, "Retry")).toBeUndefined();
  });

  it("does not offer Retry or a ClickHouse explanation for an unsupported VictoriaLogs source", async () => {
    const root = await render({ source: { source_type: "victorialogs" }, error: { message: "Recent activity unavailable", kind: "unsupported" } });
    expect(root.textContent).toContain(i18n.global.t("sources.activityUnavailable"));
    expect(root.textContent).not.toContain("sort key");
    expect(buttonWithText(root, "Retry")).toBeUndefined();
  });
});
