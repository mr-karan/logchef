export type TokenScope =
  | "*"
  | "profile:read"
  | "profile:write"
  | "tokens:read"
  | "tokens:write"
  | "users:read"
  | "users:write"
  | "teams:read"
  | "teams:write"
  | "sources:read"
  | "sources:write"
  | "logs:read"
  | "saved_queries:read"
  | "saved_queries:write"
  | "collections:read"
  | "collections:write"
  | "alerts:read"
  | "alerts:write"
  | "dashboards:read"
  | "dashboards:write"
  | "query_shares:read"
  | "query_shares:write"
  | "settings:read"
  | "settings:write";

export interface TokenScopeOption {
  value: TokenScope;
  label: string;
  description: string;
  group: string;
}

export const READ_ONLY_SCOPES: TokenScope[] = [
  "profile:read",
  "tokens:read",
  "users:read",
  "teams:read",
  "sources:read",
  "logs:read",
  "saved_queries:read",
  "collections:read",
  "alerts:read",
  "dashboards:read",
  "query_shares:read",
  "settings:read",
];

export const TOKEN_SCOPE_OPTIONS: TokenScopeOption[] = [
  { value: "profile:read", label: "Profile read", description: "Read the principal profile and preferences.", group: "Account" },
  { value: "profile:write", label: "Profile write", description: "Update preferences and profile settings.", group: "Account" },
  { value: "tokens:read", label: "Tokens read", description: "List token metadata.", group: "Account" },
  { value: "tokens:write", label: "Tokens write", description: "Create and revoke tokens.", group: "Account" },
  { value: "users:read", label: "Users read", description: "List users and service accounts.", group: "Administration" },
  { value: "users:write", label: "Users write", description: "Create, update, and delete users or service accounts.", group: "Administration" },
  { value: "teams:read", label: "Teams read", description: "Read teams, members, and team source links.", group: "Access" },
  { value: "teams:write", label: "Teams write", description: "Manage teams, members, and source links.", group: "Access" },
  { value: "sources:read", label: "Sources read", description: "Read source metadata, schema, and stats.", group: "Logs" },
  { value: "sources:write", label: "Sources write", description: "Create, validate, update, and delete sources.", group: "Logs" },
  { value: "logs:read", label: "Logs read", description: "Run queries, histograms, exports, context lookup, and LogchefQL translation.", group: "Logs" },
  { value: "saved_queries:read", label: "Saved queries read", description: "List and resolve saved queries.", group: "Logs" },
  { value: "saved_queries:write", label: "Saved queries write", description: "Create, update, and delete saved queries.", group: "Logs" },
  { value: "collections:read", label: "Collections read", description: "List collections, members, and items.", group: "Collections" },
  { value: "collections:write", label: "Collections write", description: "Create and manage collections, members, and items.", group: "Collections" },
  { value: "alerts:read", label: "Alerts read", description: "List alerts and alert history.", group: "Alerts" },
  { value: "alerts:write", label: "Alerts write", description: "Create, test, update, delete, and resolve alerts.", group: "Alerts" },
  { value: "dashboards:read", label: "Dashboards read", description: "List and view dashboards and their panels.", group: "Dashboards" },
  { value: "dashboards:write", label: "Dashboards write", description: "Create, update, and delete dashboards.", group: "Dashboards" },
  { value: "query_shares:read", label: "Query shares read", description: "Open existing query share links.", group: "Sharing" },
  { value: "query_shares:write", label: "Query shares write", description: "Create and delete query share links.", group: "Sharing" },
  { value: "settings:read", label: "Settings read", description: "Read system settings and provisioning export.", group: "Administration" },
  { value: "settings:write", label: "Settings write", description: "Update system settings and test notifications.", group: "Administration" },
];

export interface TokenScopePreset {
  id: string;
  label: string;
  description: string;
  scopes: TokenScope[];
}

export const LOGS_VIEWER_SCOPES: TokenScope[] = [
  "profile:read",
  "sources:read",
  "logs:read",
  "saved_queries:read",
  "collections:read",
];

export const LOGS_ANALYST_SCOPES: TokenScope[] = [
  ...LOGS_VIEWER_SCOPES,
  "saved_queries:write",
  "collections:write",
  "query_shares:read",
  "query_shares:write",
];

export const ALERTS_MANAGER_SCOPES: TokenScope[] = [
  "profile:read",
  "sources:read",
  "logs:read",
  "saved_queries:read",
  "alerts:read",
  "alerts:write",
];

export const SOURCE_ADMIN_SCOPES: TokenScope[] = [
  "profile:read",
  "sources:read",
  "sources:write",
  "settings:read",
];

export const TOKEN_SCOPE_PRESETS: TokenScopePreset[] = [
  {
    id: "read-only",
    label: "Read-only",
    description: "Read across every resource the principal can see.",
    scopes: READ_ONLY_SCOPES,
  },
  {
    id: "logs-viewer",
    label: "Logs viewer",
    description: "Query logs and read saved queries / collections.",
    scopes: LOGS_VIEWER_SCOPES,
  },
  {
    id: "logs-analyst",
    label: "Logs analyst",
    description: "Logs viewer + save & share queries.",
    scopes: LOGS_ANALYST_SCOPES,
  },
  {
    id: "alerts-manager",
    label: "Alerts manager",
    description: "Manage alerts end-to-end plus read logs/sources.",
    scopes: ALERTS_MANAGER_SCOPES,
  },
  {
    id: "source-admin",
    label: "Source admin",
    description: "Create and update sources and read settings.",
    scopes: SOURCE_ADMIN_SCOPES,
  },
  {
    id: "full-access",
    label: "Full access",
    description: "All scopes. Equivalent to a session token.",
    scopes: ["*"],
  },
];

function scopesEqual(a: TokenScope[], b: TokenScope[]): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(a);
  return b.every((scope) => set.has(scope));
}

export function matchingPreset(scopes: TokenScope[]): TokenScopePreset | null {
  for (const preset of TOKEN_SCOPE_PRESETS) {
    if (scopesEqual(scopes, preset.scopes)) return preset;
  }
  return null;
}

export function formatScopes(scopes: TokenScope[] | undefined): string {
  if (!scopes || scopes.length === 0) return "No access";
  if (scopes.includes("*")) return "Full access";
  const preset = matchingPreset(scopes);
  if (preset) return preset.label;
  return `${scopes.length} scope${scopes.length === 1 ? "" : "s"}`;
}
