import { beforeEach, describe, expect, it, vi } from "vitest";

// The real request helper runs; only the axios instance is replaced, so the
// test sees the config that an outgoing request would carry.
const transport = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn(),
}));

vi.mock("@/api/config", () => ({ api: transport }));

import {
  DASHBOARD_CLIENT_FEATURES,
  DASHBOARD_FEATURES_HEADER,
  DASHBOARD_PANELS_VERSION,
  DASHBOARD_SCHEMA_HEADER,
  dashboardsApi,
} from "../dashboards";
import { emptyDashboardPanels } from "@/utils/dashboardPanels";

type Config = { headers?: Record<string, string>; signal?: AbortSignal; timeout?: number };

describe("dashboardsApi client contract headers", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    for (const method of Object.values(transport)) {
      method.mockResolvedValue({ data: { status: "success", data: null } });
    }
  });

  function configOf(call: unknown[]): Config {
    // get/delete take (url, config); post/put take (url, body, config).
    return (call.length === 3 ? call[2] : call[1]) as Config;
  }

  it("sends the schema and features headers on every dashboard request", async () => {
    const body = { name: "d", description: "", panels: emptyDashboardPanels() };
    await dashboardsApi.list();
    await dashboardsApi.get(7);
    await dashboardsApi.create(body);
    await dashboardsApi.update(7, { ...body, revision: 1 });
    await dashboardsApi.remove(7);

    const configs = [
      configOf(transport.get.mock.calls[0]),
      configOf(transport.get.mock.calls[1]),
      configOf(transport.post.mock.calls[0]),
      configOf(transport.put.mock.calls[0]),
      configOf(transport.delete.mock.calls[0]),
    ];
    expect(configs).toHaveLength(5);
    for (const config of configs) {
      expect(config.headers?.[DASHBOARD_SCHEMA_HEADER]).toBe("2");
      expect(config.headers?.[DASHBOARD_FEATURES_HEADER]).toBe(DASHBOARD_CLIENT_FEATURES.join(","));
    }
    expect(transport.get.mock.calls[1][0]).toBe("/dashboards/7");
    expect(transport.put.mock.calls[0][1]).toMatchObject({ revision: 1 });
  });

  it("declares the schema it reads and only lowercase feature names its code keeps", () => {
    expect(DASHBOARD_PANELS_VERSION).toBe(2);
    for (const name of DASHBOARD_CLIENT_FEATURES) {
      expect(name).toMatch(/^[a-z_]+$/);
    }
    expect(new Set(DASHBOARD_CLIENT_FEATURES).size).toBe(DASHBOARD_CLIENT_FEATURES.length);
  });

  it("keeps the caller's options next to the headers", async () => {
    const controller = new AbortController();
    await dashboardsApi.list({ signal: controller.signal, timeout: 5 });
    const config = configOf(transport.get.mock.calls[0]);
    expect(config.signal).toBe(controller.signal);
    expect(config.timeout).toBe(5000);
    expect(config.headers?.[DASHBOARD_SCHEMA_HEADER]).toBe("2");
  });
});
