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
  labelKey: string;
  descriptionKey: string;
  groupKey: string;
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
  { value: "profile:read", labelKey: "tokens.scopes.profileRead", descriptionKey: "tokens.scopeDescriptions.profileRead", groupKey: "tokens.groups.account" },
  { value: "profile:write", labelKey: "tokens.scopes.profileWrite", descriptionKey: "tokens.scopeDescriptions.profileWrite", groupKey: "tokens.groups.account" },
  { value: "tokens:read", labelKey: "tokens.scopes.tokensRead", descriptionKey: "tokens.scopeDescriptions.tokensRead", groupKey: "tokens.groups.account" },
  { value: "tokens:write", labelKey: "tokens.scopes.tokensWrite", descriptionKey: "tokens.scopeDescriptions.tokensWrite", groupKey: "tokens.groups.account" },
  { value: "users:read", labelKey: "tokens.scopes.usersRead", descriptionKey: "tokens.scopeDescriptions.usersRead", groupKey: "tokens.groups.administration" },
  { value: "users:write", labelKey: "tokens.scopes.usersWrite", descriptionKey: "tokens.scopeDescriptions.usersWrite", groupKey: "tokens.groups.administration" },
  { value: "teams:read", labelKey: "tokens.scopes.teamsRead", descriptionKey: "tokens.scopeDescriptions.teamsRead", groupKey: "tokens.groups.access" },
  { value: "teams:write", labelKey: "tokens.scopes.teamsWrite", descriptionKey: "tokens.scopeDescriptions.teamsWrite", groupKey: "tokens.groups.access" },
  { value: "sources:read", labelKey: "tokens.scopes.sourcesRead", descriptionKey: "tokens.scopeDescriptions.sourcesRead", groupKey: "tokens.groups.logs" },
  { value: "sources:write", labelKey: "tokens.scopes.sourcesWrite", descriptionKey: "tokens.scopeDescriptions.sourcesWrite", groupKey: "tokens.groups.logs" },
  { value: "logs:read", labelKey: "tokens.scopes.logsRead", descriptionKey: "tokens.scopeDescriptions.logsRead", groupKey: "tokens.groups.logs" },
  { value: "saved_queries:read", labelKey: "tokens.scopes.savedQueriesRead", descriptionKey: "tokens.scopeDescriptions.savedQueriesRead", groupKey: "tokens.groups.logs" },
  { value: "saved_queries:write", labelKey: "tokens.scopes.savedQueriesWrite", descriptionKey: "tokens.scopeDescriptions.savedQueriesWrite", groupKey: "tokens.groups.logs" },
  { value: "collections:read", labelKey: "tokens.scopes.collectionsRead", descriptionKey: "tokens.scopeDescriptions.collectionsRead", groupKey: "tokens.groups.collections" },
  { value: "collections:write", labelKey: "tokens.scopes.collectionsWrite", descriptionKey: "tokens.scopeDescriptions.collectionsWrite", groupKey: "tokens.groups.collections" },
  { value: "alerts:read", labelKey: "tokens.scopes.alertsRead", descriptionKey: "tokens.scopeDescriptions.alertsRead", groupKey: "tokens.groups.alerts" },
  { value: "alerts:write", labelKey: "tokens.scopes.alertsWrite", descriptionKey: "tokens.scopeDescriptions.alertsWrite", groupKey: "tokens.groups.alerts" },
  { value: "dashboards:read", labelKey: "tokens.scopes.dashboardsRead", descriptionKey: "tokens.scopeDescriptions.dashboardsRead", groupKey: "tokens.groups.dashboards" },
  { value: "dashboards:write", labelKey: "tokens.scopes.dashboardsWrite", descriptionKey: "tokens.scopeDescriptions.dashboardsWrite", groupKey: "tokens.groups.dashboards" },
  { value: "query_shares:read", labelKey: "tokens.scopes.querySharesRead", descriptionKey: "tokens.scopeDescriptions.querySharesRead", groupKey: "tokens.groups.sharing" },
  { value: "query_shares:write", labelKey: "tokens.scopes.querySharesWrite", descriptionKey: "tokens.scopeDescriptions.querySharesWrite", groupKey: "tokens.groups.sharing" },
  { value: "settings:read", labelKey: "tokens.scopes.settingsRead", descriptionKey: "tokens.scopeDescriptions.settingsRead", groupKey: "tokens.groups.administration" },
  { value: "settings:write", labelKey: "tokens.scopes.settingsWrite", descriptionKey: "tokens.scopeDescriptions.settingsWrite", groupKey: "tokens.groups.administration" },
];

export interface TokenScopePreset {
  id: string;
  labelKey: string;
  descriptionKey: string;
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
    labelKey: "tokens.presets.readOnly",
    descriptionKey: "tokens.presetDescriptions.readOnly",
    scopes: READ_ONLY_SCOPES,
  },
  {
    id: "logs-viewer",
    labelKey: "tokens.presets.logsViewer",
    descriptionKey: "tokens.presetDescriptions.logsViewer",
    scopes: LOGS_VIEWER_SCOPES,
  },
  {
    id: "logs-analyst",
    labelKey: "tokens.presets.logsAnalyst",
    descriptionKey: "tokens.presetDescriptions.logsAnalyst",
    scopes: LOGS_ANALYST_SCOPES,
  },
  {
    id: "alerts-manager",
    labelKey: "tokens.presets.alertsManager",
    descriptionKey: "tokens.presetDescriptions.alertsManager",
    scopes: ALERTS_MANAGER_SCOPES,
  },
  {
    id: "source-admin",
    labelKey: "tokens.presets.sourceAdmin",
    descriptionKey: "tokens.presetDescriptions.sourceAdmin",
    scopes: SOURCE_ADMIN_SCOPES,
  },
  {
    id: "full-access",
    labelKey: "tokens.presets.fullAccess",
    descriptionKey: "tokens.presetDescriptions.fullAccess",
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

export function formatScopes(scopes: TokenScope[] | undefined, translate: (key: string, count?: number) => string): string {
  if (!scopes || scopes.length === 0) return translate("tokens.noAccess");
  if (scopes.includes("*")) return translate("tokens.presets.fullAccess");
  const preset = matchingPreset(scopes);
  if (preset) return translate(preset.labelKey);
  return translate("tokens.scopeCount", scopes.length);
}
