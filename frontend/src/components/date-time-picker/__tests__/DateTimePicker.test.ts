import { afterEach, describe, expect, it } from "vitest";
import { createApp, nextTick } from "vue";
import { i18n } from "@/i18n";
import { epochMs, parseZoneId, type TimeWindow } from "@/utils/timeWindow";
import DateTimePicker from "../DateTimePicker.vue";

// The browser runs in another zone than the picker, so browser-zone parsing shows up.
process.env.TZ = "Asia/Kolkata";

const newYork = parseZoneId("America/New_York");
if (!newYork) throw new Error("no zone data");

let unmount: (() => void) | undefined;

afterEach(() => {
  unmount?.();
  unmount = undefined;
  document.body.innerHTML = "";
});

async function openPicker(window: TimeWindow) {
  const emitted: TimeWindow[] = [];
  const app = createApp(DateTimePicker, { modelValue: window, zone: newYork, "onUpdate:modelValue": (next: TimeWindow) => emitted.push(next) });
  const host = document.createElement("div");
  document.body.appendChild(host);
  app.use(i18n).mount(host);
  unmount = () => app.unmount();
  host.querySelector("button")?.click();
  await nextTick();
  await nextTick();
  return emitted;
}

function button(text: string): HTMLButtonElement {
  const match = [...document.body.querySelectorAll("button")].find((b) => b.textContent?.trim() === text);
  if (!match) throw new Error(`no button "${text}"`);
  return match;
}

describe("DateTimePicker", () => {
  it("emits the instants it received when applied unchanged, even in a DST fold", async () => {
    // Starts at the second 01:30 of the New York fall-back day, with milliseconds.
    const window: TimeWindow = { kind: "absolute", start: epochMs(Date.parse("2026-11-01T06:30:00.250Z")), end: epochMs(Date.parse("2026-11-01T07:15:00.999Z")) };
    const emitted = await openPicker(window);

    const from = document.body.querySelector<HTMLInputElement>("input.font-mono");
    expect(from?.value).toBe("2026-11-01 01:30:00 -05:00");
    button("Apply time range").click();

    expect(emitted).toEqual([window]);
  });

  it("reads an edited fold time by its offset, in the picker's zone", async () => {
    const window: TimeWindow = { kind: "absolute", start: epochMs(Date.parse("2026-11-01T05:30:00Z")), end: epochMs(Date.parse("2026-11-01T08:00:00Z")) };
    const emitted = await openPicker(window);

    const from = document.body.querySelector<HTMLInputElement>("input.font-mono");
    if (!from) throw new Error("no from input");
    from.value = "2026-11-01 01:30:00 -05:00";
    from.dispatchEvent(new Event("input"));
    await nextTick();
    button("Apply time range").click();

    expect(emitted).toEqual([{ kind: "absolute", start: Date.parse("2026-11-01T06:30:00Z"), end: window.end }]);
  });

  it("emits a quick range as a relative window", async () => {
    const emitted = await openPicker({ kind: "relative", spec: "15m" });
    button("Last 6 hours").click();
    expect(emitted).toEqual([{ kind: "relative", spec: "6h" }]);
  });
});
