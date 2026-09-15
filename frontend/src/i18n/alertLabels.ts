import type { Alert, AlertHistoryEntry, AlertSeverity } from "@/api/alerts";
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
};
