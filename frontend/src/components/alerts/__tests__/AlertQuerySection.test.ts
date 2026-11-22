import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, h, nextTick, reactive } from "vue";
import { createPinia } from "pinia";
import { i18n } from "@/i18n";
import AlertQuerySection from "../AlertQuerySection.vue";
import type { AlertFormState } from "@/composables/useAlertForm";

function formState(group_by: string): AlertFormState {
  return {
    name: "", description: "", editor_mode: "native", query: "", condition_json: "", aggregate_function: "count",
    aggregate_field: "", group_by, lookback_seconds: 300, threshold_operator: "gt", threshold_value: 1,
    frequency_seconds: 60, severity: "warning", no_data_policy: "zero", notify_on_resolve: true, is_active: true,
    labels: [], annotations: [], destination_ids: [],
  };
}

describe("AlertQuerySection", () => {
  const hosts: HTMLElement[] = [];
  afterEach(() => hosts.splice(0).forEach((el) => el.remove()));

  async function mount(form: AlertFormState) {
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
    const element = document.createElement("div");
    hosts.push(element);
    const app = createApp({
      render: () => h(AlertQuerySection, {
        form, disabled: false, supportsConditionEditor: true, nativeEditorLabel: "SQL", nativeQueryLabel: "SQL",
        nativeQueryPlaceholder: "", nativeQueryHelpText: "", aggregateFieldSuggestions: [], conditionTemplates: [],
        queryTemplates: [], lookbackWarning: null, conditionGroupBy: true, isTestingQuery: false, testQueryResult: null,
        testQueryError: null, backtestRequest: null, onApplyConditionTemplate: () => {}, onApplyTemplate: () => {},
        onTestQuery: () => {},
      }),
    });
    app.use(createPinia()).use(i18n).mount(element);
    await nextTick();
    return element;
  }

  it("asks a grouped native rule for one row per group, not a single value", async () => {
    const form = reactive(formState(""));
    const element = await mount(form);
    const single = i18n.global.t("alerts.nativeQueryHint", { language: "SQL" });
    const grouped = i18n.global.t("alerts.nativeQueryHintGrouped", { language: "SQL" });
    expect(element.textContent).toContain(single);

    form.group_by = "service";
    await nextTick();
    expect(element.textContent).toContain(grouped);
    expect(element.textContent).not.toContain(single);
  });
});
