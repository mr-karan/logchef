import { afterEach, describe, expect, it } from "vitest";
import { createApp, h, nextTick, type App } from "vue";
import { i18n } from "@/i18n";
import ColumnFilterButton from "../ColumnFilterButton.vue";

let app: App | null = null;

afterEach(() => {
  app?.unmount();
  app = null;
  document.body.innerHTML = "";
});

async function openFilter(kind: "text" | "number") {
  const applied: unknown[] = [];
  const element = document.createElement("div");
  document.body.appendChild(element);
  app = createApp({
    render: () => h(ColumnFilterButton, { column: "status", kind, onApply: (value: unknown) => applied.push(value) }),
  });
  app.use(i18n);
  app.mount(element);
  element.querySelector<HTMLButtonElement>("[data-column-filter]")!.click();
  await nextTick();
  await nextTick();
  const input = document.body.querySelector<HTMLInputElement>("input")!;
  const submit = document.body.querySelector<HTMLButtonElement>("button[type=submit]")!;
  const type = async (text: string) => {
    input.value = text;
    input.dispatchEvent(new Event("input"));
    await nextTick();
  };
  return { applied, input, submit, type };
}

describe("column filter button", () => {
  it("applies a number typed into a numeric column", async () => {
    const { applied, input, submit, type } = await openFilter("number");
    expect(input.type).toBe("number");
    await type("300");
    expect(submit.disabled).toBe(false);
    submit.click();
    await nextTick();
    expect(applied).toEqual([{ column: "status", operator: "=", value: 300 }]);
  });

  it("keeps Apply disabled until a numeric column has a number", async () => {
    const { submit, type } = await openFilter("number");
    expect(submit.disabled).toBe(true);
    await type("abc");
    expect(submit.disabled).toBe(true);
  });

  it("applies trimmed text for a text column", async () => {
    const { applied, submit, type } = await openFilter("text");
    await type("  POST ");
    submit.click();
    await nextTick();
    expect(applied).toEqual([{ column: "status", operator: "=", value: "POST" }]);
  });
});
