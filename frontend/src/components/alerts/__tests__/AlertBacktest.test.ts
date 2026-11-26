import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, h, nextTick, ref } from "vue";
import { createPinia } from "pinia";
import { i18n } from "@/i18n";
import AlertBacktest from "../AlertBacktest.vue";
import { alertsApi, type BacktestAlertRequest, type BacktestResult } from "@/api/alerts";

const request: Omit<BacktestAlertRequest, "range_seconds"> = {
  source_id: 3,
  query: "* | stats count() as value",
  threshold_operator: "gt",
  threshold_value: 10,
  lookback_seconds: 300,
  frequency_seconds: 900,
};

function backtestResult(overrides: Partial<BacktestResult> = {}): BacktestResult {
  return {
    start: "2026-11-14T10:00:00Z",
    end: "2026-11-15T10:00:00Z",
    step_seconds: 900,
    evaluations: 96,
    coverage: 0.33,
    partial: false,
    summary: { episodes: 2, firing_seconds: 2700, breaching_evaluations: 3, notifications: 4, no_data_evaluations: 0, errors: 0 },
    series: [
      { t: "2026-11-14T10:15:00Z", status: "ok", value: 1, firing_groups: 0 },
      { t: "2026-11-14T10:30:00Z", status: "firing", value: 12, firing_groups: 1 },
      { t: "2026-11-14T10:45:00Z", status: "error", value: null, firing_groups: 1 },
    ],
    groups: [],
    groups_omitted: 0,
    ...overrides,
  };
}

async function flush() {
  for (let i = 0; i < 5; i++) await nextTick();
  await new Promise((resolve) => setTimeout(resolve, 0));
}

function mount(current = ref<typeof request | null>(request)) {
  const element = document.createElement("div");
  const app = createApp({ render: () => h(AlertBacktest, { request: current.value, disabled: false }) });
  app.use(createPinia());
  app.use(i18n);
  app.mount(element);
  const run = () => {
    const button = [...element.querySelectorAll("button")].find((b) => b.textContent?.includes("Run backtest"));
    if (!button) throw new Error("no run button");
    button.click();
  };
  return { element, app, run, current };
}

afterEach(() => vi.restoreAllMocks());

describe("alert backtest panel", () => {
  it("sends the rule with the chosen range and shows the summary and strip", async () => {
    const spy = vi.spyOn(alertsApi, "backtest").mockResolvedValue({ status: "success", data: backtestResult() });
    const { element, app, run } = mount();
    try {
      run();
      await flush();
      expect(spy).toHaveBeenCalledWith({ ...request, range_seconds: 86400 }, expect.objectContaining({ timeout: 75 }));
      const text = element.textContent ?? "";
      expect(text).toContain("2 episodes");
      expect(text).toContain("4 notifications");
      expect(text).toContain("firing for 45 min");
      expect(text).toContain("Evaluated 96 of 96 ticks, covering 33% of the range");
      const strip = element.querySelector('[role="img"]');
      expect(strip?.children).toHaveLength(3);
      expect(strip?.children[1].className).toContain("bg-red-500");
    } finally {
      app.unmount();
    }
  });

  it("shows a partial run, failed evaluations and the server's refusal", async () => {
    const spy = vi.spyOn(alertsApi, "backtest").mockResolvedValueOnce({
      status: "success",
      data: backtestResult({ partial: true, summary: { ...backtestResult().summary, errors: 1 }, first_error: "upstream down" }),
    });
    const { element, app, run } = mount();
    try {
      run();
      await flush();
      expect(element.textContent).toContain("60-second limit");
      expect(element.textContent).toContain("1 evaluation failed: upstream down");

      // The API interceptor rejects with the response envelope.
      spy.mockRejectedValueOnce({ status: "error", message: "This query reads the current time.", error_type: "ValidationError" });
      run();
      await flush();
      expect(element.textContent).toContain("This query reads the current time.");
      expect(element.querySelector('[data-testid="backtest-result"]')).toBeNull();
    } finally {
      app.unmount();
    }
  });

  it("drops a result once the rule changes", async () => {
    vi.spyOn(alertsApi, "backtest").mockResolvedValue({ status: "success", data: backtestResult() });
    const { element, app, run, current } = mount();
    try {
      run();
      await flush();
      expect(element.querySelector('[data-testid="backtest-result"]')).not.toBeNull();
      current.value = { ...request, threshold_value: 20 };
      await flush();
      expect(element.querySelector('[data-testid="backtest-result"]')).toBeNull();
    } finally {
      app.unmount();
    }
  });

  it("shows each group's lowest value for an lt rule", async () => {
    const groups = [{ group: { service: "api" }, episodes: 1, breaching_evaluations: 2, max_value: 9, min_value: 1 }];
    vi.spyOn(alertsApi, "backtest").mockResolvedValue({ status: "success", data: backtestResult({ groups }) });
    const { element, app, run } = mount(ref({ ...request, threshold_operator: "lt" as const }));
    try {
      run();
      await flush();
      const table = element.querySelector("table");
      expect(table?.textContent).toContain("Min value");
      expect(table?.querySelector("tbody td:last-child")?.textContent).toBe("1");
    } finally {
      app.unmount();
    }
  });
});
