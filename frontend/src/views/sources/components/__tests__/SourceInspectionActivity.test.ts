import { createApp, nextTick } from "vue";
import { afterEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";

vi.mock("@/components/visualizations/SourceSparkline.vue", () => ({
  default: { template: "<div class='sparkline-stub' />" },
}));

vi.mock("@/utils/format", () => ({
  formatDate: () => "Formatted timestamp",
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
    const timeoutRoot = await render({ error: "Activity request timed out" });
    expect(timeoutRoot.textContent).toContain("Activity request timed out");

    unmount?.();
    timeoutRoot.remove();
    const unavailableRoot = await render({ error: "Recent activity unavailable" });
    expect(unavailableRoot.textContent).toContain("Recent activity unavailable");
  });

  it("emits retry when an error retry button is clicked", async () => {
    let retries = 0;
    const root = await render({ error: "Activity request timed out" }, () => {
      retries++;
    });

    buttonWithText(root, "Retry")?.click();
    expect(retries).toBe(1);
  });

  it("renders successful activity values and refreshes", async () => {
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

    expect(root.textContent).toContain("1,234");
    expect(root.textContent).toContain("5,678");
    expect(root.textContent).toContain("Formatted timestamp");
    expect(root.querySelector(".sparkline-stub")).not.toBeNull();

    const title = i18n.global.t("sources.refreshRecentActivity");
    root.querySelector<HTMLButtonElement>(`button[title='${title}']`)?.click();
    expect(retries).toBe(1);
  });
});
