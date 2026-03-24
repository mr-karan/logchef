import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { extractVariableNames, extractVariablesWithOptional, useVariables } from '../useVariables'
import { useVariableStore } from '@/stores/variables'

describe('extractVariableNames', () => {
  it('extracts unique variable names', () => {
    const sql = 'SELECT * FROM logs WHERE env = {{ environment }} AND host = {{ hostname }}'
    expect(extractVariableNames(sql)).toEqual(['environment', 'hostname'])
  })

  it('returns empty array for no variables', () => {
    expect(extractVariableNames('SELECT * FROM logs')).toEqual([])
  })

  it('deduplicates repeated variable names', () => {
    const sql = '{{ env }} AND {{ env }} AND {{ host }}'
    expect(extractVariableNames(sql)).toEqual(['env', 'host'])
  })

  it('handles whitespace inside braces', () => {
    const sql = '{{  env  }} AND {{env}}'
    expect(extractVariableNames(sql)).toEqual(['env'])
  })

  it('matches valid identifier patterns only', () => {
    const sql = '{{ _private }} AND {{ var_1 }}'
    expect(extractVariableNames(sql)).toEqual(['_private', 'var_1'])
  })
})

describe('extractVariablesWithOptional', () => {
  it('marks variables inside [[ ]] as optional', () => {
    const sql = 'SELECT * FROM logs [[ AND env = {{ env }} ]]'
    const result = extractVariablesWithOptional(sql)
    expect(result).toEqual([{ name: 'env', isOptional: true }])
  })

  it('marks variables outside [[ ]] as required', () => {
    const sql = 'SELECT * FROM logs WHERE host = {{ host }}'
    const result = extractVariablesWithOptional(sql)
    expect(result).toEqual([{ name: 'host', isOptional: false }])
  })

  it('handles mixed optional and required variables', () => {
    const sql = 'WHERE host = {{ host }} [[ AND env = {{ env }} ]]'
    const result = extractVariablesWithOptional(sql)
    expect(result).toEqual([
      { name: 'host', isOptional: false },
      { name: 'env', isOptional: true },
    ])
  })

  it('returns empty array for no variables', () => {
    expect(extractVariablesWithOptional('SELECT 1')).toEqual([])
  })
})

describe('useVariables', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  describe('convertVariables', () => {
    it('replaces text variable with quoted value', () => {
      const variableStore = useVariableStore()
      variableStore.upsertVariable({
        name: 'env', type: 'text', label: 'env',
        inputType: 'input', value: 'production',
      })

      const { convertVariables } = useVariables()
      const result = convertVariables('WHERE env = {{ env }}')
      expect(result).toBe("WHERE env = 'production'")
    })

    it('replaces number variable without quotes', () => {
      const variableStore = useVariableStore()
      variableStore.upsertVariable({
        name: 'limit_val', type: 'number', label: 'limit',
        inputType: 'input', value: 100,
      })

      const { convertVariables } = useVariables()
      const result = convertVariables('LIMIT {{ limit_val }}')
      expect(result).toBe('LIMIT 100')
    })

    it('uses defaultValue when value is empty', () => {
      const variableStore = useVariableStore()
      variableStore.upsertVariable({
        name: 'env', type: 'text', label: 'env',
        inputType: 'input', value: '', defaultValue: 'staging',
      })

      const { convertVariables } = useVariables()
      const result = convertVariables('WHERE env = {{ env }}')
      expect(result).toBe("WHERE env = 'staging'")
    })

    it('escapes single quotes in SQL strings', () => {
      const variableStore = useVariableStore()
      variableStore.upsertVariable({
        name: 'msg', type: 'text', label: 'msg',
        inputType: 'input', value: "it's a test",
      })

      const { convertVariables } = useVariables()
      const result = convertVariables('WHERE msg = {{ msg }}')
      expect(result).toBe("WHERE msg = 'it''s a test'")
    })
  })

  describe('ensureVariablesFromSql', () => {
    it('creates missing variables in store', () => {
      const variableStore = useVariableStore()
      const { ensureVariablesFromSql } = useVariables()

      const newNames = ensureVariablesFromSql('WHERE env = {{ env }} AND host = {{ host }}')
      expect(newNames).toEqual(['env', 'host'])
      expect(variableStore.getVariableByName('env')).toBeDefined()
      expect(variableStore.getVariableByName('host')).toBeDefined()
    })

    it('does not recreate existing variables', () => {
      const variableStore = useVariableStore()
      variableStore.upsertVariable({
        name: 'env', type: 'text', label: 'env',
        inputType: 'dropdown', value: 'prod',
      })

      const { ensureVariablesFromSql } = useVariables()
      const newNames = ensureVariablesFromSql('WHERE env = {{ env }}')
      expect(newNames).toEqual([])
      expect(variableStore.getVariableByName('env')!.inputType).toBe('dropdown')
    })

    it('updates isOptional flag for existing variables', () => {
      const variableStore = useVariableStore()
      variableStore.upsertVariable({
        name: 'env', type: 'text', label: 'env',
        inputType: 'input', value: '', isOptional: false,
      })

      const { ensureVariablesFromSql } = useVariables()
      ensureVariablesFromSql('[[ AND env = {{ env }} ]]')
      expect(variableStore.getVariableByName('env')!.isOptional).toBe(true)
    })
  })

  describe('validateVariablesForSql', () => {
    it('returns no missing when all required vars have values', () => {
      const variableStore = useVariableStore()
      variableStore.upsertVariable({
        name: 'env', type: 'text', label: 'env',
        inputType: 'input', value: 'prod',
      })

      const { validateVariablesForSql } = useVariables()
      const result = validateVariablesForSql('WHERE env = {{ env }}')
      expect(result.hasVariables).toBe(true)
      expect(result.missingValues).toEqual([])
    })

    it('reports missing for required vars with empty values', () => {
      const variableStore = useVariableStore()
      variableStore.upsertVariable({
        name: 'env', type: 'text', label: 'env',
        inputType: 'input', value: '',
      })

      const { validateVariablesForSql } = useVariables()
      const result = validateVariablesForSql('WHERE env = {{ env }}')
      expect(result.missingValues).toEqual(['env'])
    })

    it('skips optional variables in validation', () => {
      const variableStore = useVariableStore()
      variableStore.upsertVariable({
        name: 'env', type: 'text', label: 'env',
        inputType: 'input', value: '', isOptional: true,
      })

      const { validateVariablesForSql } = useVariables()
      const result = validateVariablesForSql('[[ AND env = {{ env }} ]]')
      expect(result.hasVariables).toBe(true)
      expect(result.missingValues).toEqual([])
    })

    it('returns hasVariables false when no variables exist', () => {
      const { validateVariablesForSql } = useVariables()
      const result = validateVariablesForSql('SELECT 1')
      expect(result.hasVariables).toBe(false)
      expect(result.missingValues).toEqual([])
    })
  })
})
