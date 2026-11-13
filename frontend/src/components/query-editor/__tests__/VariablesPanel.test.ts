import { expect, it } from 'vitest'
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import { i18n } from '@/i18n'
import { useVariableStore, type VariableState } from '@/stores/variables'
import VariablesPanel from '../VariablesPanel.vue'

it('shows lowercase badges for every variable type', () => {
  const pinia = createPinia()
  const types: VariableState['type'][] = ['text', 'number', 'date', 'day']
  useVariableStore(pinia).setAllVariable(types.map(type => ({ name: type, label: type, type, value: '', inputType: 'input' })))
  const host = document.createElement('div')
  const app = createApp(VariablesPanel)
  app.use(pinia).use(i18n).mount(host)
  try {
    expect([...host.querySelectorAll('span.font-mono')].map(el => el.textContent?.trim())).toEqual(['t', 'n', 'dt', 'd'])
  } finally {
    app.unmount()
  }
})
