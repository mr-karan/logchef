import { createApp, nextTick } from "vue";
import { createPinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";
import type { SchemaAdvice, SchemaAdvisorReport } from "@/api/sources";

const api = vi.hoisted(() => ({
  getSchemaAdvice: vi.fn(),
  refreshSchemaAdvice: vi.fn(),
  dismissSchemaFinding: vi.fn(),
  restoreSchemaFinding: vi.fn(),
}));
vi.mock("@/api/sources", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/sources")>()),
  sourcesApi: api,
}));

const { default: SourceInspectionAdvisor } = await import("../SourceInspectionAdvisor.vue");

const ddl = [
  "ALTER TABLE `logs`.`app` ADD INDEX IF NOT EXISTS `idx_request_id_bloom` `request_id` TYPE bloom_filter GRANULARITY 1;",
  "ALTER TABLE `logs`.`app` MATERIALIZE INDEX `idx_request_id_bloom`;",
];
const report: SchemaAdvisorReport = {
  status: "partial",
  clickhouse_version: "26.8.1",
  target: { kind: "table", database: "logs", table: "app", engine: "MergeTree" },
  inputs: [
    { name: "system.columns", state: "ok", read_rows: 10, read_bytes: 100 },
    { name: "system.query_log", state: "denied", detail: "ACCESS_DENIED", read_rows: 0, read_bytes: 0 },
  ],
  queries_analyzed: 1900,
  queries_unanalyzed: 3,
  findings: [{
    id: "0123456789abcdef",
    rule: "skip_index_bloom",
    impact: "medium",
    title: "Add a bloom filter index on request_id",
    column: "request_id",
    evidence: [{ label: "Queries with = or IN", value: "412 of 1,900 (22%)", source: "logchef" }],
    ddl,
    rebuild: false,
    candidate: true,
    gain: "ClickHouse skips granules that cannot hold the value.",
    cost: "MATERIALIZE INDEX is a mutation over every part.",
    verify: "EXPLAIN indexes = 1 SELECT count() FROM `logs`.`app` WHERE `request_id` = '<value>'",
  }],
  notes: ["usage record is incomplete"],
};
const empty: SchemaAdvice = { source_id: 7, generated_at: null, generated_by: null, window_days: null, stale: false, report: null, dismissals: [] };
const stored: SchemaAdvice = { source_id: 7, generated_at: "2026-11-16T10:00:00Z", generated_by: 1, window_days: 30, stale: true, report, dismissals: [] };
const findingId = "0123456789abcdef";

const ok = <T>(data: T) => Promise.resolve({ status: "success" as const, data });
const fail = (message: string, error_type: string) => Promise.reject({ status: "error", message, error_type });

const clipboard = Object.getOwnPropertyDescriptor(navigator, "clipboard");
let host: HTMLElement | null = null;
let unmount: (() => void) | null = null;

async function flush() {
  for (let i = 0; i < 5; i++) {
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
  }
}

async function render(): Promise<HTMLElement> {
  host = document.createElement("div");
  document.body.appendChild(host);
  const app = createApp(SourceInspectionAdvisor, { sourceId: 7 });
  app.use(createPinia());
  app.use(i18n);
  app.mount(host);
  unmount = () => app.unmount();
  await flush();
  return host;
}

function button(root: HTMLElement, text: string): HTMLButtonElement {
  const found = [...root.querySelectorAll("button")].find((b) => b.textContent?.trim() === text);
  if (!found) throw new Error(`no button "${text}"`);
  return found;
}

beforeEach(() => {
  for (const fn of Object.values(api)) fn.mockReset();
});

async function chooseDismiss(root: HTMLElement) {
  const trigger = root.querySelector<HTMLButtonElement>(`[data-finding="${findingId}"] button[aria-label="${i18n.global.t("ui.actions")}"]`);
  if (!trigger) throw new Error("no actions menu on the finding");
  trigger.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
  await vi.waitFor(() => expect(document.querySelector("[role=menu]")).not.toBeNull());
  const item = [...document.querySelectorAll<HTMLElement>("[role=menuitem]")].find((el) => el.textContent?.trim() === i18n.global.t("advisor.dismiss"));
  if (!item) throw new Error("no Dismiss item");
  item.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, pointerType: "mouse" }));
  item.dispatchEvent(new PointerEvent("pointerup", { bubbles: true, pointerType: "mouse" }));
  item.click();
  await vi.waitFor(() => expect(document.querySelector("#advisor-dismiss-reason")).not.toBeNull());
}

function typeReason(text: string) {
  const textarea = document.querySelector<HTMLTextAreaElement>("#advisor-dismiss-reason");
  if (!textarea) throw new Error("no reason field");
  textarea.value = text;
  textarea.dispatchEvent(new Event("input", { bubbles: true }));
}

function dialogSubmit(): HTMLButtonElement {
  const found = [...document.querySelectorAll<HTMLButtonElement>("[role=dialog] button[type=submit]")][0];
  if (!found) throw new Error("no submit button");
  return found;
}

afterEach(() => {
  unmount?.();
  unmount = null;
  host?.remove();
  host = null;
  Reflect.deleteProperty(document, "execCommand");
  if (clipboard) Object.defineProperty(navigator, "clipboard", clipboard);
  else Reflect.deleteProperty(navigator, "clipboard");
});

describe("SourceInspectionAdvisor", () => {
  it("runs the first analysis and shows its finding", async () => {
    api.getSchemaAdvice.mockReturnValue(ok(empty));
    api.refreshSchemaAdvice.mockReturnValue(ok(stored));
    const root = await render();
    expect(root.textContent).toContain(i18n.global.t("advisor.noReport"));

    button(root, i18n.global.t("advisor.runAnalysis")).click();
    await flush();

    expect(api.refreshSchemaAdvice).toHaveBeenCalledWith(7);
    const card = root.querySelector('[data-finding="0123456789abcdef"]');
    expect(card?.textContent).toContain("Add a bloom filter index on request_id");
    expect(card?.textContent).toContain(i18n.global.t("advisor.impactMedium"));
    expect(card?.textContent).toContain(i18n.global.t("advisor.validateFirst"));
    expect(card?.textContent).not.toContain(i18n.global.t("advisor.needsRebuild"));
    expect(card?.textContent).toContain("412 of 1,900 (22%)");
    expect(card?.querySelector("pre")?.textContent).toBe(ddl.join("\n"));
    expect(root.textContent).toContain("usage record is incomplete");
    expect(root.textContent).toContain(i18n.global.t("advisor.stale"));
    expect(root.textContent).toContain(`system.query_log: ${i18n.global.t("advisor.inputDenied")}`);
    expect(root.textContent).not.toContain(`system.columns: ${i18n.global.t("advisor.inputOk")}`);
  });

  it("copies the DDL statements exactly, one per line", async () => {
    api.getSchemaAdvice.mockReturnValue(ok(stored));
    Reflect.deleteProperty(navigator, "clipboard");
    let copied = "";
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      value: vi.fn(() => {
        copied = document.querySelector("textarea")?.value ?? "";
        return true;
      }),
    });
    const root = await render();

    root.querySelector<HTMLButtonElement>(`button[aria-label="${i18n.global.t("advisor.copyDdl")}"]`)?.click();
    await flush();

    expect(copied).toBe(ddl.join("\n"));
  });

  it("keeps the stored report when an analysis fails", async () => {
    api.getSchemaAdvice.mockReturnValue(ok(stored));
    api.refreshSchemaAdvice.mockImplementation(() => fail("ClickHouse could not be analyzed; the stored report is kept", "ExternalServiceError"));
    const root = await render();

    button(root, i18n.global.t("advisor.runAnalysis")).click();
    await flush();

    expect(root.textContent).toContain("ClickHouse could not be analyzed; the stored report is kept");
    expect(root.querySelector('[data-finding="0123456789abcdef"]')).not.toBeNull();
    expect(button(root, i18n.global.t("ui.retry"))).toBeDefined();
  });

  it("shows why an unavailable report has no findings", async () => {
    api.getSchemaAdvice.mockReturnValue(ok({ ...stored, report: { ...report, status: "unavailable", reason: "system.columns: access denied", findings: [] } }));
    const root = await render();

    expect(root.textContent).toContain(i18n.global.t("advisor.unavailable"));
    expect(root.textContent).toContain("system.columns: access denied");
    expect(root.textContent).not.toContain(i18n.global.t("advisor.noFindings"));
  });

  it("does not offer Retry for a source that is not ClickHouse", async () => {
    api.getSchemaAdvice.mockImplementation(() => fail("Schema advice is available for ClickHouse sources only", "ValidationError"));
    const root = await render();

    expect(root.textContent).toContain("Schema advice is available for ClickHouse sources only");
    expect([...root.querySelectorAll("button")].some((b) => b.textContent?.trim() === i18n.global.t("ui.retry"))).toBe(false);
    expect(button(root, i18n.global.t("advisor.runAnalysis")).disabled).toBe(true);
  });

  it("dismisses a finding with a reason and restores it", async () => {
    api.getSchemaAdvice.mockReturnValue(ok(stored));
    api.dismissSchemaFinding.mockReturnValue(ok({ finding_id: findingId, reason: "covered by the replica", dismissed_by: 1, dismissed_at: "2026-11-16T11:00:00Z" }));
    api.restoreSchemaFinding.mockReturnValue(ok(null));
    const root = await render();
    expect(root.textContent).not.toContain(i18n.global.t("advisor.showDismissed", { count: 1 }));

    await chooseDismiss(root);
    typeReason("  covered by the replica  ");
    await flush();
    dialogSubmit().click();
    await flush();

    expect(api.dismissSchemaFinding).toHaveBeenCalledWith(7, findingId, "covered by the replica");
    expect(root.querySelector(`[data-finding="${findingId}"]`)).toBeNull();
    expect(root.textContent).toContain(i18n.global.t("advisor.noFindings"));
    expect(root.querySelector(`[data-dismissed="${findingId}"]`)).toBeNull();

    root.querySelector<HTMLButtonElement>("#advisor-show-dismissed")?.click();
    await flush();
    const row = root.querySelector(`[data-dismissed="${findingId}"]`);
    expect(row?.textContent).toContain("Add a bloom filter index on request_id");
    expect(row?.textContent).toContain("covered by the replica");

    button(root, i18n.global.t("advisor.restore")).click();
    await flush();

    expect(api.restoreSchemaFinding).toHaveBeenCalledWith(7, findingId);
    expect(root.querySelector(`[data-finding="${findingId}"]`)).not.toBeNull();
    expect(root.querySelector(`[data-dismissed="${findingId}"]`)).toBeNull();
  });

  it("lists a dismissal whose finding left the report", async () => {
    const gone = "fedcba9876543210";
    api.getSchemaAdvice.mockReturnValue(ok({ ...stored, dismissals: [{ finding_id: gone, reason: "", dismissed_by: null, dismissed_at: "2026-11-01T10:00:00Z" }] }));
    const root = await render();

    expect(root.querySelector(`[data-finding="${findingId}"]`)).not.toBeNull();
    root.querySelector<HTMLButtonElement>("#advisor-show-dismissed")?.click();
    await flush();
    expect(root.querySelector(`[data-dismissed="${gone}"]`)?.textContent).toContain(i18n.global.t("advisor.findingGone", { id: gone }));
  });

  it("keeps the dialog open and blocks a reason over 500 bytes", async () => {
    api.getSchemaAdvice.mockReturnValue(ok(stored));
    const root = await render();

    await chooseDismiss(root);
    typeReason("é".repeat(251));
    await flush();

    expect(document.body.textContent).toContain(i18n.global.t("advisor.reasonTooLong", { max: 500 }));
    expect(dialogSubmit().disabled).toBe(true);
    typeReason("é".repeat(250));
    await flush();
    expect(dialogSubmit().disabled).toBe(false);
    expect(api.dismissSchemaFinding).not.toHaveBeenCalled();
  });
});
