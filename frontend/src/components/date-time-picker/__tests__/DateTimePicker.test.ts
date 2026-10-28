import { afterEach, describe, expect, it, vi } from "vitest";
import { createApp, h, nextTick, ref } from "vue";
import { i18n } from "@/i18n";
import { epochMs, formatWallClock, parseZoneId, resolveWindow, type ResolvedWindow, type TimeWindow, type ZoneId } from "@/utils/timeWindow";
import DateTimePicker from "../DateTimePicker.vue";

// The browser runs in another zone than the picker, so browser-zone parsing shows up.
process.env.TZ = "Asia/Kolkata";

function zone(name: string): ZoneId {
  const parsed = parseZoneId(name);
  if (!parsed) throw new Error(`no zone data for ${name}`);
  return parsed;
}

const newYork = zone("America/New_York");

let unmount: (() => void) | undefined;

afterEach(() => {
  unmount?.();
  unmount = undefined;
  document.body.innerHTML = "";
  localStorage.clear();
});

// A run months before the test clock, so a picker that resolves its own "now" shows up.
const anchor = epochMs(Date.parse("2026-03-02T15:00:00Z"));

async function openPicker(window: TimeWindow, resolved: ResolvedWindow = resolveWindow(window, newYork, anchor), zonePreference = "local") {
  const emitted: TimeWindow[] = [];
  const zones: string[] = [];
  const app = createApp(DateTimePicker, {
    modelValue: window,
    resolved,
    zonePreference,
    "onUpdate:modelValue": (next: TimeWindow) => emitted.push(next),
    "onUpdate:zonePreference": (next: string) => zones.push(next),
  });
  const host = document.createElement("div");
  document.body.appendChild(host);
  app.use(i18n).mount(host);
  unmount = () => app.unmount();
  host.querySelector("button")?.click();
  await nextTick();
  await nextTick();
  return { emitted, zones };
}

function button(text: string): HTMLButtonElement {
  const match = [...document.body.querySelectorAll("button")].find((b) => b.textContent?.trim() === text);
  if (!match) throw new Error(`no button "${text}"`);
  return match;
}

describe("DateTimePicker", () => {
  it("anchors the open popover to its trigger so it is placed on screen", async () => {
    await openPicker({ kind: "relative", spec: "15m" });
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();

    const wrapper = document.body.querySelector<HTMLElement>("[data-reka-popper-content-wrapper]");
    expect(wrapper).not.toBeNull();
    // Reka parks an unanchored popover at translate(0, -200%), off screen.
    expect(wrapper?.style.transform).not.toContain("-200%");
  });

  it("emits the instants it received when applied unchanged, even in a DST fold", async () => {
    // Starts at the second 01:30 of the New York fall-back day, with milliseconds.
    const window: TimeWindow = { kind: "absolute", start: epochMs(Date.parse("2026-11-01T06:30:00.250Z")), end: epochMs(Date.parse("2026-11-01T07:15:00.999Z")) };
    const { emitted } = await openPicker(window);

    const from = document.body.querySelector<HTMLInputElement>("input.font-mono");
    expect(from?.value).toBe("2026-11-01 01:30:00 -05:00");
    button("Apply time range").click();

    expect(emitted).toEqual([window]);
  });

  it("reads an edited fold time by its offset, in the picker's zone", async () => {
    const window: TimeWindow = { kind: "absolute", start: epochMs(Date.parse("2026-11-01T05:30:00Z")), end: epochMs(Date.parse("2026-11-01T08:00:00Z")) };
    const { emitted } = await openPicker(window);

    const from = document.body.querySelector<HTMLInputElement>("input.font-mono");
    if (!from) throw new Error("no from input");
    from.value = "2026-11-01 01:30:00 -05:00";
    from.dispatchEvent(new Event("input"));
    await nextTick();
    button("Apply time range").click();

    expect(emitted).toEqual([{ kind: "absolute", start: Date.parse("2026-11-01T06:30:00Z"), end: window.end }]);
  });

  it("keeps a relative window when applied unchanged, and shows the instants it was resolved to", async () => {
    const window: TimeWindow = { kind: "relative", spec: "today" };
    const resolved = resolveWindow(window, newYork, anchor);
    const range = `${formatWallClock(resolved.start, newYork)} - ${formatWallClock(resolved.end, newYork)}`;
    expect(range).toBe("2026-03-02 00:00:00 - 2026-03-02 10:00:00");

    const { emitted } = await openPicker(window, resolved);
    expect(document.body.querySelector("button")?.textContent?.trim()).toBe(range);
    button("Apply time range").click();

    expect(emitted).toEqual([{ kind: "relative", spec: "today" }]);
  });

  it("selects by the zone preference, even when the browser zone is UTC", async () => {
    const window: TimeWindow = { kind: "relative", spec: "15m" };
    const { zones } = await openPicker(window, resolveWindow(window, zone("UTC"), anchor), "local");

    expect(button("Local").getAttribute("aria-pressed")).toBe("true");
    button("Local").click();
    expect(zones).toEqual([]);
    button("UTC").click();
    expect(zones).toEqual(["utc"]);
  });

  it("reopens a recent range at the same instants after the zone changes", async () => {
    const window: TimeWindow = { kind: "absolute", start: epochMs(Date.parse("2026-11-01T05:30:00Z")), end: epochMs(Date.parse("2026-11-01T08:00:00Z")) };
    const first = await openPicker(window);
    const from = document.body.querySelector<HTMLInputElement>("input.font-mono");
    if (!from) throw new Error("no from input");
    from.value = "2026-10-31 22:00:00";
    from.dispatchEvent(new Event("input"));
    await nextTick();
    button("Apply time range").click();
    const saved = { kind: "absolute", start: Date.parse("2026-11-01T02:00:00Z"), end: window.end };
    expect(first.emitted).toEqual([saved]);
    unmount?.();
    document.body.innerHTML = "";

    const utc = zone("UTC");
    const reopened = await openPicker(window, resolveWindow(window, utc, anchor), "utc");
    button("2026-11-01 02:00:00 to 2026-11-01 08:00:00").click();
    expect(reopened.emitted).toEqual([saved]);
  });

  it("drops recent ranges stored as zone-less text", async () => {
    localStorage.setItem("logchef_recent_ranges", JSON.stringify([{ from: "2026-10-31 22:00:00", to: "2026-11-01 03:00:00", label: "x" }]));
    await openPicker({ kind: "relative", spec: "15m" });
    expect(document.body.textContent).not.toContain("Recent");
  });

  it("emits a quick range as a relative window", async () => {
    const { emitted } = await openPicker({ kind: "relative", spec: "15m" });
    button("Last 6 hours").click();
    expect(emitted).toEqual([{ kind: "relative", spec: "6h" }]);
  });

  it("closes the open popover when it becomes disabled, and ignores its ranges", async () => {
    const window: TimeWindow = { kind: "relative", spec: "15m" };
    const emitted: TimeWindow[] = [];
    const disabledReason = ref<string>();
    const app = createApp({
      render: () =>
        h(DateTimePicker, {
          modelValue: window,
          resolved: resolveWindow(window, newYork, anchor),
          zonePreference: "local",
          disabledReason: disabledReason.value,
          "onUpdate:modelValue": (next: TimeWindow) => emitted.push(next),
        }),
    });
    const host = document.createElement("div");
    document.body.appendChild(host);
    app.use(i18n).mount(host);
    unmount = () => app.unmount();
    host.querySelector("button")?.click();
    await nextTick();
    await nextTick();
    expect(button("Last 6 hours")).toBeTruthy();

    disabledReason.value = "Native SQL sets its own time range";
    await new Promise((resolve) => setTimeout(resolve));
    [...document.body.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Last 6 hours")?.click();

    expect(emitted).toEqual([]);
    expect(document.body.textContent).not.toContain("Last 6 hours");
  });

  it("ignores a range clicked while the popover plays its exit animation", async () => {
    // jsdom has no CSS animations; reporting an animation name keeps the closing popover mounted like a browser does.
    let animationName = "enter";
    const realComputedStyle = globalThis.getComputedStyle.bind(globalThis);
    const computedStyle = vi.spyOn(globalThis, "getComputedStyle").mockImplementation((element, pseudo) => {
      const styles = realComputedStyle(element, pseudo);
      return new Proxy(styles, {
        get: (target, key) => (key === "animationName" ? animationName : Reflect.get(target, key)),
      });
    });
    const window: TimeWindow = { kind: "relative", spec: "15m" };
    const emitted: TimeWindow[] = [];
    const disabledReason = ref<string>();
    const app = createApp({
      render: () =>
        h(DateTimePicker, {
          modelValue: window,
          resolved: resolveWindow(window, newYork, anchor),
          zonePreference: "local",
          disabledReason: disabledReason.value,
          "onUpdate:modelValue": (next: TimeWindow) => emitted.push(next),
        }),
    });
    const host = document.createElement("div");
    document.body.appendChild(host);
    app.use(i18n).mount(host);
    unmount = () => {
      app.unmount();
      computedStyle.mockRestore();
    };
    host.querySelector("button")?.click();
    await nextTick();
    await nextTick();

    animationName = "exit";
    disabledReason.value = "Native SQL sets its own time range";
    await new Promise((resolve) => setTimeout(resolve));
    const closing = [...document.body.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Last 6 hours");
    expect(closing).toBeTruthy();
    closing?.click();

    expect(emitted).toEqual([]);
  });
});
