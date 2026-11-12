import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, nextTick, type App } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { i18n } from "@/i18n";
import { metaApi } from "@/api/meta";
import { useMetaStore } from "@/stores/meta";
import VersionBanner from "../VersionBanner.vue";

vi.mock("@/api/meta", () => ({ metaApi: { getMeta: vi.fn() } }));
vi.mock("@/composables/useToast", () => ({ useToast: () => ({ toast: vi.fn() }) }));

const response = (version: string) => ({
  status: "success" as const,
  data: { version, http_server_timeout: "30s", max_query_limit: 100000 },
});

async function settle() {
  await new Promise((resolve) => setTimeout(resolve, 0));
  await nextTick();
}

describe("server version banner", () => {
  let app: App | undefined;
  let host: HTMLDivElement;
  let stamp: HTMLMetaElement;

  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    stamp = document.createElement("meta");
    stamp.name = "logchef-version";
    stamp.content = "v1";
    document.head.append(stamp);
    host = document.createElement("div");
    document.body.append(host);
  });

  afterEach(() => {
    app?.unmount();
    app = undefined;
    host.remove();
    stamp.remove();
  });

  function mount() {
    app = createApp(VersionBanner).use(createPinia()).use(i18n);
    app.mount(host);
  }

  it("rechecks on focus at most every 30 seconds and stops once the banner shows", async () => {
    vi.useFakeTimers({ toFake: ["Date"] });
    try {
      vi.mocked(metaApi.getMeta).mockResolvedValue(response("v1"));
      mount();
      await settle();
      expect(host.querySelector('[role="status"]')).toBeNull();
      vi.mocked(metaApi.getMeta).mockResolvedValue(response("v2"));
      window.dispatchEvent(new Event("focus"));
      await settle();
      expect(metaApi.getMeta).toHaveBeenCalledTimes(1);
      vi.advanceTimersByTime(30_000);
      window.dispatchEvent(new Event("focus"));
      await settle();
      expect(metaApi.getMeta).toHaveBeenCalledTimes(2);
      expect(host.textContent).toContain("Logchef was updated. Reload to continue.");
      expect(host.querySelector("button")?.textContent).toBe("Reload Page");
      vi.mocked(metaApi.getMeta).mockResolvedValue(response("v1"));
      vi.advanceTimersByTime(30_000);
      window.dispatchEvent(new Event("focus"));
      await settle();
      expect(metaApi.getMeta).toHaveBeenCalledTimes(2);
      expect(host.querySelector('[role="status"]')).not.toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("shares one request between concurrent forced loads", async () => {
    let resolve: (value: ReturnType<typeof response>) => void = () => {};
    vi.mocked(metaApi.getMeta).mockReturnValue(new Promise((r) => { resolve = r; }));
    const meta = useMetaStore();
    const loads = [meta.loadMeta({ force: true }), meta.loadMeta({ force: true })];
    resolve(response("v1"));
    await Promise.all(loads);
    expect(metaApi.getMeta).toHaveBeenCalledTimes(1);
    await meta.loadMeta({ force: true });
    expect(metaApi.getMeta).toHaveBeenCalledTimes(2);
  });

  it("detects a mismatch on initial load", async () => {
    vi.mocked(metaApi.getMeta).mockResolvedValue(response("v2"));
    mount();
    await settle();
    expect(host.querySelector('[role="status"]')).not.toBeNull();
  });

  it("disables the check without a page stamp, including on focus", async () => {
    stamp.remove();
    mount();
    await useMetaStore().loadMeta({ force: true });
    vi.mocked(metaApi.getMeta).mockClear();
    window.dispatchEvent(new Event("focus"));
    await settle();
    expect(host.querySelector('[role="status"]')).toBeNull();
    expect(metaApi.getMeta).not.toHaveBeenCalled();
  });
});
