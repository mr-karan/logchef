import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'
import { i18n } from '@/i18n'
import type { Alert } from '@/api/alerts'
import { logqlConditionQuery, logqlDuration, logqlHasLookbackRange, useAlertForm, type UseAlertFormProps } from '../useAlertForm'
import { controlledApi } from '@/test/controlledApi'

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
  const host = document.createElement('div')
  let form: ReturnType<typeof useAlertForm> | undefined
  const app = createApp({ setup() { form = useAlertForm(props, vi.fn()); return () => h('div') } })
  app.use(createPinia()).use(i18n).mount(host)
  if (!form) throw new Error('Form did not mount')
  return { form, unmount: () => app.unmount() }
}

describe('LogQL alert queries', () => {
  it('writes the lookback as a LogQL duration', () => {
    expect(logqlDuration(300)).toBe('5m')
    expect(logqlDuration(3600)).toBe('1h')
    expect(logqlDuration(5430)).toBe('1h30m30s')
    expect(logqlDuration(45)).toBe('45s')
  })

  it('wraps the compiled filter per aggregate, as RFC 0011 specifies', () => {
    const q = '{app="api"} | json'
    expect(logqlConditionQuery(q, 'count', '', 300)).toBe('sum(count_over_time({app="api"} | json [300s]))')
    expect(logqlConditionQuery(q, 'sum', 'latency', 300)).toBe('sum(sum_over_time({app="api"} | json | unwrap latency | __error__="" [300s]))')
    expect(logqlConditionQuery(q, 'max', 'latency', 60)).toBe('max(max_over_time({app="api"} | json | unwrap latency | __error__="" [60s]))')
    expect(logqlConditionQuery(q, 'avg', 'latency', 300)).toBe('avg_over_time({app="api"} | json | unwrap latency | __error__="" [300s]) by ()')
  })

  it('finds the lookback range in seconds or duration form only', () => {
    expect(logqlHasLookbackRange('sum(count_over_time({a="b"}[300s]))', 300)).toBe(true)
    expect(logqlHasLookbackRange('sum(count_over_time({a="b"}[5m]))', 300)).toBe(true)
    expect(logqlHasLookbackRange('sum(count_over_time({a="b"}[10m]))', 300)).toBe(false)
  })
})

describe('alert form on a Loki source', () => {
  let network: ReturnType<typeof controlledApi>
  let mounted: ReturnType<typeof mountForm>
  beforeEach(() => {
    vi.useFakeTimers()
    network = controlledApi(config => config.url?.endsWith('/translate') === true)
    mounted = mountForm()
  })
  afterEach(() => { mounted.unmount(); network.restore(); vi.useRealTimers() })

  function translateTo(query: string) {
    network.requests.at(-1)!.succeed({ valid: true, generated_query: query, generated_query_language: 'logql', conditions: [], fields_used: [] })
  }

  it('builds the count query and rebuilds it when the lookback changes', async () => {
    const { form } = mounted
    form.form.condition_json = 'level="error"'
    await nextTick()
    await vi.advanceTimersByTimeAsync(300)
    translateTo('{app="api", level="error"}')
    await vi.advanceTimersByTimeAsync(0)
    expect(form.form.query).toBe('sum(count_over_time({app="api", level="error"} [300s]))')
    expect(form.generatedQueryLanguageLabel.value).toContain('LogQL')

    form.form.lookback_seconds = 600
    await nextTick()
    expect(form.form.query).toBe('')
    await vi.advanceTimersByTimeAsync(300)
    translateTo('{app="api", level="error"}')
    await vi.advanceTimersByTimeAsync(0)
    expect(form.form.query).toBe('sum(count_over_time({app="api", level="error"} [600s]))')
  })

  it('refuses to unwrap a field LogQL cannot name', async () => {
    const { form } = mounted
    form.form.aggregate_function = 'sum'
    form.form.aggregate_field = 'http.latency'
    form.form.condition_json = 'level="error"'
    await nextTick()
    await vi.advanceTimersByTimeAsync(300)
    translateTo('{app="api", level="error"}')
    await vi.advanceTimersByTimeAsync(0)
    expect(form.conditionError.value).toContain('http.latency')
    expect(form.form.query).toBe('')
  })

  it('warns when a native query has no range equal to the lookback, without blocking a save', async () => {
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
  })
})

describe('editing a saved Loki condition alert', () => {
  it('reads the aggregate and field back from the LogQL query', () => {
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
