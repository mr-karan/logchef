import type { VariableState } from '@/stores/variables'

export function hasVariableValue(variable: VariableState): boolean {
  if (variable.value === null || variable.value === undefined) return false
  if (Array.isArray(variable.value)) return variable.value.length > 0
  if (typeof variable.value === 'string') return variable.value.trim() !== ''
  return true
}

export function inputTypeFor(type: string): string {
  if (type === 'number') return 'number'
  return 'text'
}

export function getPlaceholderForType(type: string): string {
  switch (type) {
    case 'number':
      return 'Enter a number...'
    case 'date':
      return '2026-01-28 14:30:00'
    case 'text':
    default:
      return 'Enter text value...'
  }
}

export function formatVariableValue(variable: VariableState): string {
  if (!variable.value) return ''

  // Handle array values (multi-select)
  if (Array.isArray(variable.value)) {
    if (variable.value.length === 0) return ''
    const displayValues = variable.value.slice(0, 3).map(v => {
      const opt = variable.options?.find(o => o.value === v)
      return opt?.label || v
    })
    const suffix = variable.value.length > 3 ? `, +${variable.value.length - 3} more` : ''
    return displayValues.join(', ') + suffix
  }

  switch (variable.type) {
    case 'number':
      return `Value: ${variable.value} `
    case 'date': {
      const date = new Date(variable.value as string)
      if (isNaN(date.getTime())) {
        return `Date: ${variable.value}`
      }
      return date.toLocaleString('en-US', {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
      })
    }
    case 'text':
    default: {
      const value = String(variable.value)
      return value.length > 20 ? `"${value.substring(0, 20)}..."` : `"${value}"`
    }
  }
}
