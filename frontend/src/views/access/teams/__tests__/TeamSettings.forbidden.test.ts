import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createApp, defineComponent, type App } from "vue";
import { createPinia } from "pinia";
import { createMemoryHistory, createRouter, type Router } from "vue-router";
import { i18n } from "@/i18n";
import { setForbiddenHandler } from "@/api/config";
import { controlledApi } from "@/test/controlledApi";
import TeamSettings from "../TeamSettings.vue";

const toasts = vi.hoisted((): string[] => []);
vi.mock("vue-sonner", () => {
  const record = (kind: string) => () => { toasts.push(kind); };
  return { toast: Object.assign(record("default"), { error: record("error"), success: record("success") }) };
});

describe("TeamSettings leaving a team", () => {
  let host: HTMLDivElement;
  let app: App;
  let router: Router;
  let network: ReturnType<typeof controlledApi>;
  const forbidden = vi.fn<() => void>();

  beforeEach(async () => {
    toasts.length = 0;
    forbidden.mockClear();
    setForbiddenHandler(forbidden);
    network = controlledApi((config) => /^\/teams\/\d+$/.test(config.url ?? ""));
    host = document.createElement("div");
    document.body.appendChild(host);
    router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: "/admin/teams/:id", name: "TeamSettings", component: TeamSettings }],
    });
    app = createApp(defineComponent({ template: "<router-view />" }));
    app.use(createPinia()).use(i18n).use(router);
    await router.push("/admin/teams/1");
    app.mount(host);
  });

  afterEach(() => {
    app.unmount();
    host.remove();
    network.restore();
  });

  it("ignores a 403 for the team it left", async () => {
    await vi.waitFor(() => expect(network.requests.map((r) => r.config.url)).toContain("/teams/1"));
    const teamOne = network.requests[0];

    await router.push("/admin/teams/2");
    await vi.waitFor(() => expect(network.requests.map((r) => r.config.url)).toContain("/teams/2"));
    teamOne.respond(403, { status: "error", message: "forbidden", error_type: "Forbidden" });
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(forbidden).not.toHaveBeenCalled();
    expect(toasts).toEqual([]);
  });

  it("still redirects on a 403 for the team it shows", async () => {
    await vi.waitFor(() => expect(network.requests.map((r) => r.config.url)).toContain("/teams/1"));
    network.requests[0].respond(403, { status: "error", message: "forbidden", error_type: "Forbidden" });

    await vi.waitFor(() => expect(forbidden).toHaveBeenCalledTimes(1));
  });
});
