import { describe, it, expect, beforeEach, vi } from "vitest";
import { setActivePinia, createPinia } from "pinia";
import { nextTick } from "vue";

const mocks = vi.hoisted(() => ({ get: vi.fn() }));

vi.mock("@/api/alerts", () => ({
  alertsApi: { get: mocks.get },
}));

import { useAlertsStore } from "../alerts";
import { useContextStore } from "../context";

describe("useAlertsStore", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    mocks.get.mockReset();
  });

  it("keeps loaded alerts when the team or source changes", async () => {
    mocks.get.mockResolvedValue({
      status: "success",
      data: { id: 3, source_id: 7, name: "High error rate", last_state: "resolved" },
    });
    const alerts = useAlertsStore();
    await alerts.refreshAlert(3);

    const context = useContextStore();
    context.selectTeam(2);
    context.selectSource(17);
    await nextTick();

    expect(alerts.alerts.map((alert) => alert.id)).toEqual([3]);
  });
});
