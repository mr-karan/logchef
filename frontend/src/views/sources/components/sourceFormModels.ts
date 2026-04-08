import type {
  ClickHouseConnectionInfo,
  Source,
  ValidateConnectionRequestInfo,
  VictoriaLogsConnectionInfo,
} from "@/api/sources";

export type DatasourceType = "clickhouse" | "victorialogs";
export type ClickHouseTableMode = "create" | "connect";
export type VictoriaLogsAuthMode = "none" | "basic" | "bearer";

export interface ClickHouseSourceFormState {
  host: string;
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

export function createDefaultClickHouseFormState(): ClickHouseSourceFormState {
  return {
    host: "",
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

export function buildClickHouseConnection(state: ClickHouseSourceFormState): ClickHouseConnectionInfo {
  return {
    host: state.host.trim(),
    username: state.enableAuth ? state.username.trim() : "",
    password: state.enableAuth ? state.password : "",
    database: state.database.trim(),
    table_name: state.tableName.trim(),
  };
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

export function clickHouseFormStateFromSource(source: Source): ClickHouseSourceFormState {
  const connection = (source.connection || {}) as Partial<ClickHouseConnectionInfo>;
  const enableAuth = Boolean(connection.username);

  return {
    host: connection.host || "",
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
