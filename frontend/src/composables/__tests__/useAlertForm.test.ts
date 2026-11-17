import { describe, expect, it, vi } from 'vitest'
import { createApp, h, reactive } from 'vue'
import { createPinia } from 'pinia'
import { i18n } from '@/i18n'
import { conditionQuery, parseGroupBy, useAlertForm, type UseAlertFormProps } from '../useAlertForm'
import { controlledApi } from '@/test/controlledApi'

function mountForm() {
  const props = reactive<UseAlertFormProps>({ open: true, mode: 'create', teamId: 1, sourceId: 1, alert: null })
  const host = document.createElement('div')
  let form: ReturnType<typeof useAlertForm> | undefined
  const app = createApp({ setup() { form = useAlertForm(props, vi.fn()); return () => h('div') } })
  app.use(createPinia()).use(i18n).mount(host)
  if (!form) throw new Error('Form did not mount')
  let unmounted = false
  return { form, props, unmount: () => { if (!unmounted) app.unmount(); unmounted = true } }
}

describe('condition rule', () => {
  it('writes the filter, one aggregate named value and the by keys as LogchefQL', () => {
    expect(conditionQuery('level="error"', 'count', '', [])).toBe('level="error" | count() as value')
    expect(conditionQuery('level="error"', 'avg', 'dur', [])).toBe('level="error" | avg(dur) as value')
    expect(conditionQuery('status >= 500', 'sum', 'log.bytes', ['service', 'region'])).toBe('status >= 500 | sum(log.bytes) as value by service, region')
  })

  it('tests the LogchefQL rule and leaves its compile to the server', async () => {
    const network = controlledApi(config => config.url === '/alerts/test')
    const mounted = mountForm()
    try {
      mounted.form.form.name = 'Slow requests'
      mounted.form.form.condition_json = 'status >= 500'
      mounted.form.form.aggregate_function = 'max'
      expect(mounted.form.isValid.value).toBe(false)
      mounted.form.form.aggregate_field = 'dur'
      expect(mounted.form.isValid.value).toBe(true)
      void mounted.form.handleTestQuery()
      await vi.waitFor(() => expect(network.requests).toHaveLength(1))
      const body = JSON.parse(String(network.requests[0].config.data))
      expect(body).toMatchObject({
        query_language: 'logchefql', editor_mode: 'condition', condition_json: 'status >= 500',
        query: 'status >= 500 | max(dur) as value', group_by: [],
      })
      network.requests[0].succeed({ value: 0, threshold_met: false, execution_time_ms: 1, rows_returned: 0, warnings: [], samples: [], groups_total: 0 })
    } finally {
      mounted.unmount()
      network.restore()
    }
  })
})

describe('group by field', () => {
  it('splits column names and drops blanks', () => {
    expect(parseGroupBy(' service, ,region ,')).toEqual(['service', 'region'])
    expect(parseGroupBy('')).toEqual([])
  })

  it('sends the parsed group_by with a native test query', async () => {
    const network = controlledApi(config => config.url === '/alerts/test')
    const mounted = mountForm()
    try {
      mounted.form.form.editor_mode = 'native'
      mounted.form.form.query = 'SELECT service, count() AS value FROM logs GROUP BY service'
      mounted.form.form.group_by = 'service'
      void mounted.form.handleTestQuery()
      await vi.waitFor(() => expect(network.requests).toHaveLength(1))
      expect(JSON.parse(String(network.requests[0].config.data)).group_by).toEqual(['service'])
      network.requests[0].succeed({ value: 0, threshold_met: false, execution_time_ms: 1, rows_returned: 0, warnings: [], samples: [], groups_total: 0 })
    } finally {
      mounted.unmount()
      network.restore()
    }
  })
})
