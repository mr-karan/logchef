export interface Log {
  id: string
  timestamp: string
  trace_id: string
  span_id: string
  trace_flags: number
  severity_text: string
  severity_number: number
  service_name: string
  namespace: string
  body: string
  log_attributes: Record<string, string>
  [key: string]: any // For dynamic fields
}

export interface LogQueryParams {
  start_timestamp: number
  end_timestamp: number
  limit: number
}

export interface LogResponse {
  logs: Log[]
  params: {
    end_timestamp: number
    filter_groups: null
    limit: number
    sort: null
    source_id: string
    start_timestamp: number
  }
  stats: {
    execution_time_ms: number
  }
}

export interface Source {
  id: string
  schema_type: 'managed' | 'unmanaged'
  connection: ConnectionInfo
  description?: string
  ttl_days: number
  created_at: string
  updated_at: string
  is_connected: boolean
  schema?: string
  columns?: Array<{ name: string; type: string }>
}

export interface ConnectionInfo {
  host: string  // format: "host:port"
  username: string
  password: string
  database: string
  table_name: string
}

export interface CreateSourcePayload {
  schema_type: string
  connection: ConnectionInfo
  description?: string
  ttl_days: number
}
