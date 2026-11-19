import type { Source } from "@/api/sources";
import { hasSourceCapability } from "@/lib/queryMetadata";

export type LogContextSource = Pick<Source, "source_type" | "capabilities" | "columns">;

/**
 * Whether a row offers log context: "none" hides the action, "native-loki"
 * shows it disabled. A Loki row identifies its stream only when a LogchefQL
 * query produced it, because a native pipeline can drop or rewrite stream
 * labels (RFC 0011 "Log context").
 */
export function logContextState(
  source: LogContextSource | null | undefined,
  activeMode: string,
): "none" | "native-loki" | "available" {
  if (!hasSourceCapability(source, "log_context")) return "none";
  if (source?.source_type === "loki" && activeMode !== "logchefql") return "native-loki";
  return "available";
}

/** The row's value for each stream label of a Loki source's schema. */
export function rowStreamLabels(
  source: LogContextSource | null | undefined,
  row: Record<string, unknown>,
): Record<string, string> | undefined {
  if (source?.source_type !== "loki") return undefined;
  const labels: Record<string, string> = {};
  for (const column of source.columns ?? []) {
    const value = row[column.name];
    if (column.type === "label" && typeof value === "string") labels[column.name] = value;
  }
  return labels;
}
