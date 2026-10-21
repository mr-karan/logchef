import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'
import { i18n } from '@/i18n'
import { useAlertForm, type UseAlertFormProps } from '../useAlertForm'
import { controlledApi } from '@/test/controlledApi'

function translation(sql: string) {
  return { valid: true, sql, conditions: [], fields_used: [] }
}

function mountForm() {
  const props = reactive<UseAlertFormProps>({ open: true, mode: 'create', teamId: 1, sourceId: 1, alert: null })
  const host = document.createElement('div')
  let form: ReturnType<typeof useAlertForm> | undefined
  const app = createApp({ setup() { form = useAlertForm(props, vi.fn()); return () => h('div') } })
  app.use(createPinia()).use(i18n).mount(host)
  if (!form) throw new Error('Form did not mount')
  return { form, props, unmount: () => app.unmount() }
}

describe('alert condition translation ownership', () => {
  let network: ReturnType<typeof controlledApi>
  let mounted: ReturnType<typeof mountForm>
  beforeEach(() => {
    vi.useFakeTimers()
    network = controlledApi(config => config.url?.endsWith('/translate') === true)
    mounted = mountForm()
    mounted.form.form.name = 'Error count'
  })
  afterEach(() => { mounted.unmount(); network.restore(); vi.useRealTimers() })

  async function edit(condition: string) {
    mounted.form.form.condition_json = condition
    await nextTick()
    await vi.advanceTimersByTimeAsync(300)
  }

  it('keeps the newer SQL when translations finish in reverse order', async () => {
    await edit('severity = "old"')
    await edit('severity = "new"')
    expect(network.requests).toHaveLength(2)
    network.requests[1].succeed(translation("severity = 'new'"))
    await vi.advanceTimersByTimeAsync(0)
    const query = mounted.form.form.query
    expect(query).toContain("severity = 'new'")
    network.requests[0].succeed(translation("severity = 'old'"))
    await vi.advanceTimersByTimeAsync(0)
    expect(mounted.form.form.query).toBe(query)
    expect(mounted.form.isValid.value).toBe(true)
  })

  it('invalidates pending SQL immediately during the next debounce', async () => {
    await edit('severity = "old"')
    mounted.form.form.condition_json = 'severity = "new"'
    await nextTick()
    network.requests[0].succeed(translation("severity = 'old'"))
    await vi.advanceTimersByTimeAsync(0)
    expect(mounted.form.generatedQuery.value).toBe('')
    expect(mounted.form.isValid.value).toBe(false)
  })

  it('ignores an old failure after a successful newer translation', async () => {
    await edit('severity = "old"')
    await edit('severity = "new"')
    network.requests[1].succeed(translation("severity = 'new'"))
    await vi.advanceTimersByTimeAsync(0)
    network.requests[0].fail('Old translation failed')
    await vi.advanceTimersByTimeAsync(0)
    expect(mounted.form.conditionError.value).toBeNull()
    expect(mounted.form.isValid.value).toBe(true)
  })

  it('does not leave a translation timer after unmount', async () => {
    mounted.form.form.condition_json = 'severity = "new"'
    await nextTick()
    mounted.unmount()
    await vi.advanceTimersByTimeAsync(300)
    expect(network.requests).toHaveLength(0)
  })

  it.each(['close', 'source', 'native'] as const)('invalidates an in-flight translation on %s', async change => {
    await edit('severity = "old"')
    if (change === 'close') mounted.props.open = false
    if (change === 'source') mounted.props.sourceId = 2
    if (change === 'native') { mounted.form.form.editor_mode = 'native'; mounted.form.form.query = 'SELECT 42' }
    await nextTick()
    network.requests[0].succeed(translation("severity = 'old'"))
    await vi.advanceTimersByTimeAsync(0)
    expect(mounted.form.generatedQuery.value).toBe('')
    if (change === 'native') expect(mounted.form.form.query).toBe('SELECT 42')
  })
})
