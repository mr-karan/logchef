import { describe, expect, it, vi } from 'vitest'
import { createApp, h, reactive } from 'vue'
import { createPinia } from 'pinia'
import { i18n } from '@/i18n'
import type { Alert } from '@/api/alerts'
import { useAlertForm, type FormCreatePayload, type UseAlertFormProps } from '../useAlertForm'

const clickhouseSource = {
  id: 1,
  source_type: 'clickhouse',
  query_languages: ['logchefql', 'clickhouse-sql'],
  alert_editor_modes: ['condition', 'native'],
  capabilities: ['logchefql_aggregation'],
  connection: { database: 'logs', table_name: 'app' },
  columns: [{ name: 'dur', type: 'Float64' }],
}

vi.mock('@/stores/sources', () => ({
  useSourcesStore: () => ({ teamSources: [clickhouseSource], currentSourceDetails: null }),
}))

function mountForm(alert: Alert | null = null) {
  const props = reactive<UseAlertFormProps>({ open: true, mode: alert ? 'edit' : 'create', teamId: 1, sourceId: 1, alert })
  const emit = vi.fn()
  const host = document.createElement('div')
  let form: ReturnType<typeof useAlertForm> | undefined
  const app = createApp({ setup() { form = useAlertForm(props, emit); return () => h('div') } })
  app.use(createPinia()).use(i18n).mount(host)
  if (!form) throw new Error('Form did not mount')
  return { form, emit, unmount: () => app.unmount() }
}

function savedCondition(fields: Partial<Alert>): Alert {
  return {
    id: 1, source_id: 1, name: 'Slow', editor_mode: 'condition', condition_json: 'level="error"',
    lookback_seconds: 300, threshold_operator: 'gt', threshold_value: 1, frequency_seconds: 60, severity: 'warning',
    no_data_policy: 'zero', notify_on_resolve: true, is_active: true, group_by: [], destination_ids: [],
    ...fields,
  } as Alert
}

describe('a condition on a source that compiles by keys', () => {
  it('sends the Group by keys inside the LogchefQL rule and as group_by', () => {
    const mounted = mountForm()
    try {
      const { form } = mounted
      expect(form.conditionGroupBy.value).toBe(true)
      form.form.name = 'Errors per service'
      form.form.condition_json = 'level="error"'
      form.form.group_by = 'service, region'
      form.handleSubmit()
      const payload = mounted.emit.mock.calls[0][1] as FormCreatePayload
      expect(payload).toMatchObject({
        query_language: 'logchefql', editor_mode: 'condition',
        query: 'level="error" | count() as value by service, region', group_by: ['service', 'region'],
      })
    } finally {
      mounted.unmount()
    }
  })

  it('reads the aggregate, field and groups back from a saved LogchefQL rule', () => {
    const mounted = mountForm(savedCondition({
      query_language: 'logchefql', query: 'level="error" | avg(dur) as value by service', group_by: ['service'],
    }))
    try {
      const { form } = mounted.form
      expect([form.aggregate_function, form.aggregate_field, form.group_by, form.query]).toEqual(['avg', 'dur', 'service', ''])
    } finally {
      mounted.unmount()
    }
  })

  it('reads a condition saved before the server compiled it and saves it as LogchefQL', () => {
    const mounted = mountForm(savedCondition({
      query_language: 'clickhouse-sql', query: "SELECT sum(`dur`) as value\nFROM logs.app\nWHERE (`level` = 'error') AND {{__time_filter}}",
    }))
    try {
      const { form } = mounted
      expect([form.form.aggregate_function, form.form.aggregate_field]).toEqual(['sum', 'dur'])
      form.handleSubmit()
      expect(mounted.emit.mock.calls[0][1]).toMatchObject({ query_language: 'logchefql', query: 'level="error" | sum(dur) as value' })
    } finally {
      mounted.unmount()
    }
  })
})
