import { format } from "date-fns";
import type { Source } from "@/api/sources";

export function normalizeDateString(value: string): string {
  return value.includes('T') || value.includes('Z') || value.includes('+')
    ? value
    : value.replace(' ', 'T') + 'Z';
}

/**
 * Format a date string using date-fns
 * @param dateString - The date string to format
 * @param formatStr - The format string to use (defaults to 'PPp' - "Feb 17, 2025, 3:17 PM")
 * @returns Formatted date string or 'Never' if date is invalid/undefined
 */
export function formatDate(
  dateString: string | undefined,
  formatStr = "PPp"
): string {
  if (!dateString) return "Never";
  try {
    const normalizedDateStr = normalizeDateString(dateString);
    const date = new Date(normalizedDateStr);
    return format(date, formatStr);
  } catch {
    return "Invalid date";
  }
}

/**
 * Format a source's display name.
 * @param source - The source object containing connection info
 * @returns Formatted source name string
 */
export function formatSourceName(source: Source): string {
  const connection = source.connection as Record<string, unknown>;
  const database = typeof connection.database === "string" ? connection.database : null;
  const tableName = typeof connection.table_name === "string" ? connection.table_name : null;
  const baseURL = typeof connection.base_url === "string" ? connection.base_url : null;

  if (source.source_type === "clickhouse" && database && tableName) {
    return `${database}.${tableName}`;
  }
  if (source.name?.trim()) {
    return source.name;
  }
  if (source.source_type === "victorialogs" && baseURL) {
    return baseURL;
  }
  return `Source ${source.id}`;
}

interface SourceConnectionDetail {
  label: string;
  value: string;
  monospace?: boolean;
}

export function getSourceConnectionDetails(source: Source): SourceConnectionDetail[] {
  const connection = source.connection as Record<string, unknown>;

  if (source.source_type === "clickhouse") {
    const details: SourceConnectionDetail[] = [];
    if (typeof connection.host === "string" && connection.host) {
      details.push({ label: "Host", value: connection.host, monospace: true });
    }
    if (typeof connection.database === "string" && connection.database) {
      details.push({ label: "Database", value: connection.database, monospace: true });
    }
    if (typeof connection.table_name === "string" && connection.table_name) {
      details.push({ label: "Table", value: connection.table_name, monospace: true });
    }
    return details;
  }

  if (source.source_type === "victorialogs") {
    const details: SourceConnectionDetail[] = [];
    if (typeof connection.base_url === "string" && connection.base_url) {
      details.push({ label: "Base URL", value: connection.base_url, monospace: true });
    }

    const tenant = connection.tenant as Record<string, unknown> | undefined;
    const accountId = typeof tenant?.account_id === "string" ? tenant.account_id : "";
    const projectId = typeof tenant?.project_id === "string" ? tenant.project_id : "";
    if (accountId || projectId) {
      details.push({
        label: "Tenant",
        value: `account=${accountId || "-"} project=${projectId || "-"}`,
        monospace: true,
      });
    }

    const scope = connection.scope as Record<string, unknown> | undefined;
    const scopeQuery = typeof scope?.query === "string" ? scope.query : "";
    if (scopeQuery) {
      details.push({ label: "Scope", value: scopeQuery, monospace: true });
    }

    return details;
  }

  return [];
}
