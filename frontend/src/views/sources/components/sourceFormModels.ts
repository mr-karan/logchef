import type {
  ClickHouseConnectionInfo,
  ClickHouseQuerySettings,
  Source,
  ValidateConnectionRequestInfo,
  VictoriaLogsConnectionInfo,
} from "@/api/sources";

export type DatasourceType = "clickhouse" | "victorialogs";
export type ClickHouseTableMode = "create" | "connect";
export type VictoriaLogsAuthMode = "none" | "basic" | "bearer";

// UI representation of the optional per-source ClickHouse query settings. Every
// field is stored as a string so a blank input maps cleanly to "unset": numeric
// fields hold the raw text, `readonly` is "" | "2", and `resultOverflowMode` is
// "" | "throw" | "break". A blank field is omitted from the wire JSON entirely.
export interface ClickHouseSettingsFormState {
  maxExecutionTime: string;
  maxResultRows: string;
  maxResultBytes: string;
  maxRowsToRead: string;
  maxBytesToRead: string;
  readonly: string;
  resultOverflowMode: string;
}

export interface ClickHouseSourceFormState {
  host: string;
  tlsEnable: boolean;
  enableAuth: boolean;
  username: string;
  password: string;
  database: string;
  tableName: string;
  tableMode: ClickHouseTableMode;
  ttlDays: string;
  metaTSField: string;
  metaSeverityField: string;
  schema: string;
  settings: ClickHouseSettingsFormState;
}

export interface VictoriaLogsSourceFormState {
  baseURL: string;
  authMode: VictoriaLogsAuthMode;
  username: string;
  password: string;
  token: string;
  accountID: string;
  projectID: string;
  scopeQuery: string;
  metaTSField: string;
  metaSeverityField: string;
}

export const clickHouseSchemaTemplate = `CREATE TABLE IF NOT EXISTS {{database_name}}.{{table_name}}
(
    timestamp DateTime64(3) CODEC(DoubleDelta, LZ4),
    trace_id String CODEC(ZSTD(1)),
    span_id String CODEC(ZSTD(1)),
    trace_flags UInt32 CODEC(ZSTD(1)),
    severity_text LowCardinality(String) CODEC(ZSTD(1)),
    severity_number Int32 CODEC(ZSTD(1)),
    service_name LowCardinality(String) CODEC(ZSTD(1)),
    namespace LowCardinality(String) CODEC(ZSTD(1)),
    body String CODEC(ZSTD(1)),
    log_attributes Map(LowCardinality(String), String) CODEC(ZSTD(1)),

    INDEX idx_trace_id trace_id TYPE bloom_filter(0.001) GRANULARITY 1,
    INDEX idx_severity_text severity_text TYPE set(100) GRANULARITY 4,
    INDEX idx_log_attributes_keys mapKeys(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_log_attributes_values mapValues(log_attributes) TYPE bloom_filter(0.01) GRANULARITY 1,
    INDEX idx_body body TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
)
ENGINE = MergeTree()
PARTITION BY toDate(timestamp)
ORDER BY (namespace, service_name, timestamp)
TTL toDateTime(timestamp) + INTERVAL {{ttl_day}} DAY
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;`;

export function createDefaultClickHouseSettingsState(): ClickHouseSettingsFormState {
  return {
    maxExecutionTime: "",
    maxResultRows: "",
    maxResultBytes: "",
    maxRowsToRead: "",
    maxBytesToRead: "",
    readonly: "",
    resultOverflowMode: "",
  };
}

export function createDefaultClickHouseFormState(): ClickHouseSourceFormState {
  return {
    host: "",
    tlsEnable: false,
    enableAuth: false,
    username: "",
    password: "",
    database: "",
    tableName: "",
    tableMode: "create",
    ttlDays: "90",
    metaTSField: "timestamp",
    metaSeverityField: "severity_text",
    schema: "",
    settings: createDefaultClickHouseSettingsState(),
  };
}

export function createDefaultVictoriaLogsFormState(): VictoriaLogsSourceFormState {
  return {
    baseURL: "",
    authMode: "none",
    username: "",
    password: "",
    token: "",
    accountID: "",
    projectID: "",
    scopeQuery: "",
    metaTSField: "_time",
    metaSeverityField: "",
  };
}

export function sourceTypeFromSource(source?: { source_type?: string | null }): DatasourceType {
  return source?.source_type === "victorialogs" ? "victorialogs" : "clickhouse";
}

export function generateClickHouseSchema(state: Pick<ClickHouseSourceFormState, "database" | "tableName" | "ttlDays">): string {
  const database = state.database.trim() || "your_database";
  const tableName = state.tableName.trim() || "your_table";
  const ttlDays = state.ttlDays.trim() || "90";

  return clickHouseSchemaTemplate
    .replace(/{{database_name}}/g, database)
    .replace(/{{table_name}}/g, tableName)
    .replace(/{{ttl_day}}/g, ttlDays);
}

// Parse a numeric settings input. Blank/whitespace-only maps to undefined
// (unset). Non-numeric or negative values are treated as unset so a malformed
// entry never sends a bad value to the backend.
function parseNonNegativeSetting(value: string): number | undefined {
  const trimmed = value.trim();
  if (trimmed === "") {
    return undefined;
  }
  const parsed = Number(trimmed);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return undefined;
  }
  return parsed;
}

// Build the optional per-source ClickHouse query settings, omitting any blank
// sub-field. Returns undefined when nothing is set so `settings` is omitted
// from the connection entirely (matching Go's `omitempty`).
export function buildClickHouseSettings(
  state: ClickHouseSettingsFormState
): ClickHouseQuerySettings | undefined {
  const settings: ClickHouseQuerySettings = {};

  const maxExecutionTime = parseNonNegativeSetting(state.maxExecutionTime);
  if (maxExecutionTime !== undefined) {
    settings.max_execution_time = maxExecutionTime;
  }
  const maxResultRows = parseNonNegativeSetting(state.maxResultRows);
  if (maxResultRows !== undefined) {
    settings.max_result_rows = maxResultRows;
  }
  const maxResultBytes = parseNonNegativeSetting(state.maxResultBytes);
  if (maxResultBytes !== undefined) {
    settings.max_result_bytes = maxResultBytes;
  }
  const maxRowsToRead = parseNonNegativeSetting(state.maxRowsToRead);
  if (maxRowsToRead !== undefined) {
    settings.max_rows_to_read = maxRowsToRead;
  }
  const maxBytesToRead = parseNonNegativeSetting(state.maxBytesToRead);
  if (maxBytesToRead !== undefined) {
    settings.max_bytes_to_read = maxBytesToRead;
  }

  const readonly = parseNonNegativeSetting(state.readonly);
  if (readonly !== undefined) {
    settings.readonly = readonly;
  }

  const overflowMode = state.resultOverflowMode.trim();
  if (overflowMode !== "") {
    settings.result_overflow_mode = overflowMode;
  }

  return Object.keys(settings).length > 0 ? settings : undefined;
}

export function buildClickHouseConnection(state: ClickHouseSourceFormState): ClickHouseConnectionInfo {
  const connection: ClickHouseConnectionInfo = {
    host: state.host.trim(),
    username: state.enableAuth ? state.username.trim() : "",
    password: state.enableAuth ? state.password : "",
    database: state.database.trim(),
    table_name: state.tableName.trim(),
  };

  const settings = buildClickHouseSettings(state.settings);
  if (settings) {
    connection.settings = settings;
  }

  return connection;
}

export function buildVictoriaLogsConnection(state: VictoriaLogsSourceFormState): VictoriaLogsConnectionInfo {
  const connection: VictoriaLogsConnectionInfo = {
    base_url: state.baseURL.trim(),
  };

  if (state.authMode !== "none") {
    connection.auth = {
      mode: state.authMode,
    };
    if (state.authMode === "basic") {
      connection.auth.username = state.username.trim();
      connection.auth.password = state.password;
    }
    if (state.authMode === "bearer") {
      connection.auth.token = state.token;
    }
  }

  if (state.accountID.trim() || state.projectID.trim()) {
    connection.tenant = {
      account_id: state.accountID.trim(),
      project_id: state.projectID.trim(),
    };
  }

  if (state.scopeQuery.trim()) {
    connection.scope = {
      query: state.scopeQuery.trim(),
    };
  }

  return connection;
}

export function buildClickHouseValidationRequest(state: ClickHouseSourceFormState): ValidateConnectionRequestInfo {
  const request: ValidateConnectionRequestInfo = {
    source_type: "clickhouse",
    connection: buildClickHouseConnection(state),
  };

  if (state.tableMode === "connect") {
    request.timestamp_field = state.metaTSField.trim();
    request.severity_field = state.metaSeverityField.trim();
  }

  return request;
}

export function buildVictoriaLogsValidationRequest(state: VictoriaLogsSourceFormState): ValidateConnectionRequestInfo {
  return {
    source_type: "victorialogs",
    connection: buildVictoriaLogsConnection(state),
  };
}

// Hydrate the UI settings state from a persisted connection. Unset numeric
// settings become blank strings; readonly and result_overflow_mode fall back to
// "" (Default) when absent.
function clickHouseSettingsStateFromConnection(
  settings: ClickHouseQuerySettings | undefined
): ClickHouseSettingsFormState {
  const state = createDefaultClickHouseSettingsState();
  if (!settings) {
    return state;
  }

  if (settings.max_execution_time !== undefined && settings.max_execution_time !== null) {
    state.maxExecutionTime = String(settings.max_execution_time);
  }
  if (settings.max_result_rows !== undefined && settings.max_result_rows !== null) {
    state.maxResultRows = String(settings.max_result_rows);
  }
  if (settings.max_result_bytes !== undefined && settings.max_result_bytes !== null) {
    state.maxResultBytes = String(settings.max_result_bytes);
  }
  if (settings.max_rows_to_read !== undefined && settings.max_rows_to_read !== null) {
    state.maxRowsToRead = String(settings.max_rows_to_read);
  }
  if (settings.max_bytes_to_read !== undefined && settings.max_bytes_to_read !== null) {
    state.maxBytesToRead = String(settings.max_bytes_to_read);
  }
  if (settings.readonly !== undefined && settings.readonly !== null) {
    state.readonly = String(settings.readonly);
  }
  if (settings.result_overflow_mode) {
    state.resultOverflowMode = settings.result_overflow_mode;
  }

  return state;
}

export function clickHouseFormStateFromSource(source: Source): ClickHouseSourceFormState {
  const connection = (source.connection || {}) as Partial<ClickHouseConnectionInfo>;
  const enableAuth = Boolean(connection.username);

  return {
    host: connection.host || "",
    tlsEnable: Boolean(connection.tls_enable),
    enableAuth,
    username: connection.username || "",
    password: connection.password || "",
    database: connection.database || "",
    tableName: connection.table_name || "",
    tableMode: source._meta_is_auto_created ? "create" : "connect",
    ttlDays: String(source.ttl_days ?? 90),
    metaTSField: source._meta_ts_field || "timestamp",
    metaSeverityField: source._meta_severity_field || "",
    schema: source.schema || "",
    settings: clickHouseSettingsStateFromConnection(connection.settings),
  };
}

export function victoriaLogsFormStateFromSource(source: Source): VictoriaLogsSourceFormState {
  const connection = (source.connection || {}) as Partial<VictoriaLogsConnectionInfo>;
  const auth = connection.auth || {};
  const tenant = connection.tenant || {};
  const scope = connection.scope || {};
  const authMode = auth.mode === "basic" || auth.mode === "bearer" ? auth.mode : "none";

  return {
    baseURL: connection.base_url || "",
    authMode,
    username: auth.username || "",
    password: auth.password || "",
    token: auth.token || "",
    accountID: tenant.account_id || "",
    projectID: tenant.project_id || "",
    scopeQuery: scope.query || "",
    metaTSField: source._meta_ts_field || "_time",
    metaSeverityField: source._meta_severity_field || "",
  };
}

export function serializeConnectionSnapshot(connection: unknown): string {
  return JSON.stringify(connection ?? {});
}
