import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { toast } from "vue-sonner";
import { api } from "@/api/config";
import { setLocale } from "@/i18n";
import { useSourcesStore } from "../sources";

vi.mock("vue-sonner", () => ({
  toast: Object.assign(vi.fn(), { error: vi.fn(), success: vi.fn() }),
}));

const originalAdapter = api.defaults.adapter;

const payload = {
  name: "Logs",
  meta_is_auto_created: true,
  ttl_days: 7,
  connection: { base_url: "http://localhost:9428" },
};

describe("source success notifications", () => {
  beforeEach(() => {
    localStorage.clear();
    setActivePinia(createPinia());
    vi.clearAllMocks();
    api.defaults.adapter = async (config) => ({
      config,
      status: 200,
      statusText: "OK",
      headers: {},
      data: { status: "success", data: config.method === "get" ? [] : { ...payload, id: 7 } },
    });
  });

  afterEach(async () => {
    api.defaults.adapter = originalAdapter;
    await setLocale("en");
  });

  it.each([
    { locale: "en" as const, messages: ["Source created successfully", "Source updated successfully", "Source deleted successfully"] },
    { locale: "zh-CN" as const, messages: ["数据源已成功创建", "数据源已成功更新", "数据源已成功删除"] },
  ])("uses the active $locale language for source mutations", async ({ locale, messages }) => {
    await setLocale(locale);
    const sources = useSourcesStore();

    expect((await sources.createSource(payload)).success).toBe(true);
    expect((await sources.updateSource(7, { name: "Updated logs" })).success).toBe(true);
    expect((await sources.deleteSource(7)).success).toBe(true);

    expect(vi.mocked(toast).mock.calls.map(([message]) => message)).toEqual(messages);
    expect(toast.error).not.toHaveBeenCalled();
  });
});
