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
}

export interface LogResponse {
  logs: Log[]
  total_count: number
  has_more: boolean
}
