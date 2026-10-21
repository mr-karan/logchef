import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { computed } from 'vue'
import type { FieldValuesResult } from '@/api/sources'
import { useFieldValuesStore } from '../exploreFieldValues'

function result(value: string): FieldValuesResult {
  return {
    field_name: 'level', field_type: 'String', total_distinct: 2,
    is_low_cardinality: true,
    values: [{ value, count: 1 }, { value: 'other', count: 3 }],
  }
}

beforeEach(() => setActivePinia(createPinia()))

describe('field values cache', () => {
  it('retains only the newest result per source and field across time ranges', () => {
    const store = useFieldValuesStore()
    for (let range = 0; range < 50; range++) {
      for (let field = 0; field < 200; field++) {
        store.populateFromFetch(1, 2, `field${field}`, `start${range}`, `end${range}`, `q${range}`, result(`value${range}`))
      }
    }
    expect(store.entries.size).toBe(200)
    expect(store.findCachedValues(2, 'field0')?.values).toEqual([
      { value: 'other', count: 3 }, { value: 'value49', count: 1 },
    ])
  })

  it('keeps sources isolated and updates reactive lookups and summaries', () => {
    const store = useFieldValuesStore()
    const values = computed(() => store.findCachedValues(2, 'level'))
    const summary = computed(() => store.getFieldSummary(2, 'level'))
    expect(values.value).toBeNull()
    expect(summary.value).toBeNull()
    store.populateFromFetch(1, 2, 'level', 's', 'e', '', result('first'))
    expect(values.value?.values[1].value).toBe('first')
    expect(summary.value).toEqual({ type: 'String', totalDistinct: 2, isLowCardinality: true })
    store.populateFromFetch(1, 3, 'level', 's', 'e', '', result('different source'))
    store.populateFromFetch(1, 2, 'level', 's2', 'e2', 'query', result('latest'))
    expect(store.entries.size).toBe(2)
    expect(store.entries.get('2|level')).toMatchObject({
      teamId: 1, startTime: 's2', endTime: 'e2', contextQuery: 'query',
    })
    expect(values.value?.values[1].value).toBe('latest')
    expect(store.findCachedValues(3, 'level')?.values[1].value).toBe('different source')
  })
})
