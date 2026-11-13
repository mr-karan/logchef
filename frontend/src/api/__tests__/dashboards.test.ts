import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { InternalAxiosRequestConfig } from "axios";
import { api } from "../config";

import {
  DASHBOARD_CLIENT_FEATURES,
  DASHBOARD_FEATURES_HEADER,
  DASHBOARD_PANELS_VERSION,
  DASHBOARD_SCHEMA_HEADER,
  dashboardsApi,
} from "../dashboards";
import { emptyDashboardPanels } from "@/utils/dashboardPanels";

describe("dashboardsApi client contract headers", () => {
  const requests: InternalAxiosRequestConfig[] = [];
  const originalAdapter = api.defaults.adapter;
  afterEach(() => { api.defaults.adapter = originalAdapter; });
  beforeEach(() => {
    requests.length = 0;
    api.defaults.adapter = async (config) => {
      requests.push(config);
      return { data: { status: "success", data: null }, status: 200, statusText: "OK", headers: {}, config };
    };
  });

  it("sends the schema and features headers on every dashboard request", async () => {
    const body = { name: "d", description: "", panels: emptyDashboardPanels() };
    await dashboardsApi.list();
    await dashboardsApi.get(7);
    await dashboardsApi.create(body);
    await dashboardsApi.update(7, { ...body, revision: 1 });
    await dashboardsApi.remove(7);

    const configs = requests;
    expect(configs).toHaveLength(5);
    for (const config of configs) {
      expect(config.headers?.[DASHBOARD_SCHEMA_HEADER]).toBe("2");
      expect(config.headers?.[DASHBOARD_FEATURES_HEADER]).toBe(DASHBOARD_CLIENT_FEATURES.join(","));
    }
    expect(requests[1].url).toBe("/dashboards/7");
    expect(JSON.parse(requests[3].data)).toMatchObject({ revision: 1 });
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
    const config = requests[0];
    expect(config.signal).toBe(controller.signal);
    expect(config.timeout).toBe(5000);
    expect(config.headers?.[DASHBOARD_SCHEMA_HEADER]).toBe("2");
  });
});
