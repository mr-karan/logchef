import type { Alert, AlertHistoryEntry, AlertInstance, AlertSeverity } from "@/api/alerts";
import type { MessageKey } from "./index";

export const severityLabels: Record<AlertSeverity, MessageKey> = {
  info: "ui.info",
  warning: "ui.warning",
  critical: "ui.critical",
};

export const alertStateLabels: Record<Alert["last_state"] | AlertHistoryEntry["status"], MessageKey> = {
  firing: "alerts.stateFiring",
  triggered: "ui.triggered",
  resolved: "ui.resolved",
  error: "ui.error",
  no_data: "alerts.stateNoData",
};

export const instanceStateLabels: Record<AlertInstance["state"], MessageKey> = {
  firing: "alerts.stateFiring",
  no_data: "alerts.stateNoData",
  ok: "alerts.stateOk",
};
