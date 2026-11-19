import { describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'
import { i18n } from '@/i18n'
import type { Alert } from '@/api/alerts'
import { logqlDuration, logqlHasLookbackRange, useAlertForm, type FormCreatePayload, type UseAlertFormProps } from '../useAlertForm'

const lokiSource = {
  id: 1,
  source_type: 'loki',
  query_languages: ['logchefql', 'logql'],
  alert_editor_modes: ['condition', 'native'],
  connection: { base_url: 'http://loki.test', selector: '{app="api"}' },
  columns: [{ name: 'latency', type: 'int' }],
}

vi.mock('@/stores/sources', () => ({
  useSourcesStore: () => ({ teamSources: [lokiSource], currentSourceDetails: null }),
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

describe('LogQL alert queries', () => {
  it('writes the lookback as a LogQL duration', () => {
    expect(logqlDuration(300)).toBe('5m')
    expect(logqlDuration(3600)).toBe('1h')
    expect(logqlDuration(5430)).toBe('1h30m30s')
    expect(logqlDuration(45)).toBe('45s')
  })

  it('finds the lookback range in seconds or duration form only', () => {
    expect(logqlHasLookbackRange('sum(count_over_time({a="b"}[300s]))', 300)).toBe(true)
    expect(logqlHasLookbackRange('sum(count_over_time({a="b"}[5m]))', 300)).toBe(true)
    expect(logqlHasLookbackRange('sum(count_over_time({a="b"}[10m]))', 300)).toBe(false)
  })
})

describe('alert form on a Loki source', () => {
  it('sends an ungrouped LogchefQL condition, which the server compiles to LogQL', () => {
    const mounted = mountForm()
    try {
      const { form } = mounted
      expect(form.conditionGroupBy.value).toBe(false)
      form.form.name = 'Errors'
      form.form.condition_json = 'level="error"'
      form.form.group_by = 'pod'
      form.handleSubmit()
      expect(mounted.emit.mock.calls[0][1] as FormCreatePayload).toMatchObject({
        query_language: 'logchefql', editor_mode: 'condition', query: 'level="error" | count() as value', group_by: [],
      })
    } finally {
      mounted.unmount()
    }
  })

  it('warns when a native query has no range equal to the lookback, without blocking a save', async () => {
    const mounted = mountForm()
    const { form } = mounted
    form.form.name = 'Errors'
    form.form.editor_mode = 'native'
    form.form.query = 'sum(count_over_time({app="api"} [10m]))'
    await nextTick()
    expect(form.lookbackWarning.value).toContain('[5m]')
    expect(form.isValid.value).toBe(true)
    form.form.query = 'sum(count_over_time({app="api"} [5m]))'
    expect(form.lookbackWarning.value).toBeNull()
    expect(form.queryTemplates.value[0].query).toBe('sum(count_over_time({app="api"} |~ "(?i)error" [5m]))')
    mounted.unmount()
  })
})

describe('editing a saved Loki condition alert', () => {
  it('reads the aggregate and field back from a LogQL query saved before the server compiled conditions', () => {
    const alert = {
      id: 1, name: 'Slow', editor_mode: 'condition', query_language: 'logql', condition_json: 'level="error"',
      query: 'max(max_over_time({app="api"} | json | unwrap latency | __error__="" [300s]))',
      lookback_seconds: 300, threshold_operator: 'gt', threshold_value: 1, frequency_seconds: 60, severity: 'warning',
      no_data_policy: 'zero', notify_on_resolve: true, is_active: true, group_by: [], destination_ids: [],
    } as unknown as Alert
    const mounted = mountForm(alert)
    try {
      expect(mounted.form.form.aggregate_function).toBe('max')
      expect(mounted.form.form.aggregate_field).toBe('latency')
    } finally {
      mounted.unmount()
    }
  })
})
