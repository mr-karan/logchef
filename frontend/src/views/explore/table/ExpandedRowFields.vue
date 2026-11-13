<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Columns3, Copy, Minus, Plus } from 'lucide-vue-next'
import { filterChoices, type FilterChoice } from '@/utils/logchefql/filters'
import { copyText } from '@/utils/clipboard'
import { flattenRow, type RowField } from './rowFields'

const props = defineProps<{
  row: Record<string, unknown>
  canFilter: boolean
  timestampField: string
  shownColumns: string[]
}>()

const emit = defineEmits<{
  (e: 'drill-down', value: { column: string; value: unknown; operator: FilterChoice['operator'] }): void
  (e: 'add-column', value: { field: string; nested: boolean }): void
}>()

const { t } = useI18n()

// Shown columns first, in table order, then the rest.
const fields = computed(() => {
  const keys = [...props.shownColumns.filter(key => key in props.row), ...Object.keys(props.row).filter(key => !props.shownColumns.includes(key))]
  return flattenRow(Object.fromEntries(keys.map(key => [key, props.row[key]])))
})

function displayValue(value: unknown): string {
  if (value === null || value === undefined) return 'null'
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}

// Text the user selected inside this field's value, so "contains" can use it.
function selectionIn(element: EventTarget | null): string {
  const valueElement = element instanceof Element ? element.closest('[data-field-row]')?.querySelector('[data-field-value]') : null
  const selection = window.getSelection()
  if (!valueElement || !selection?.anchorNode || !valueElement.contains(selection.anchorNode)) return ''
  return selection.toString()
}

function choice(value: unknown, event: MouseEvent, kind: 'include' | 'exclude'): FilterChoice {
  return filterChoices(value, selectionIn(event.currentTarget))[kind]
}

function filter(item: RowField, event: MouseEvent, kind: 'include' | 'exclude') {
  const field = item.field
  const value = item.textual && typeof item.value !== 'string' ? JSON.stringify(item.value) : item.value
  const { operator, value: filterValue } = choice(value, event, kind)
  emit('drill-down', { column: field, value: filterValue, operator })
}

function filterTitle(value: unknown, kind: 'include' | 'exclude'): string {
  const { operator, value: term } = filterChoices(value)[kind]
  if (operator === '~' || operator === '!~') {
    return t(kind === 'include' ? 'explore.filterContains' : 'explore.filterNotContains', { term: String(term) })
  }
  return t(kind === 'include' ? 'ui.filterThisValue' : 'ui.filterThisValue2')
}

function copy(value: unknown) {
  copyText(displayValue(value)).catch(error => console.warn('Copy failed', error))
}

function canFilterField(field: string): boolean {
  return props.canFilter && field !== props.timestampField
}
</script>

<template>
  <div class="text-xs font-mono" role="list">
    <div
      v-for="item in fields"
      :key="item.field"
      data-field-row
      role="listitem"
      class="group/field grid grid-cols-[minmax(10rem,18rem)_1fr_auto] items-start gap-3 rounded px-2 py-0.5 hover:bg-muted/50"
    >
      <span
        class="truncate text-sky-700 dark:text-sky-400"
        :style="{ paddingLeft: `${item.depth}rem` }"
        :title="item.field"
      >{{ item.field }}</span>
      <span
        data-field-value
        class="min-w-0 whitespace-pre-wrap break-all text-foreground"
        :class="item.isObject ? 'line-clamp-1 text-muted-foreground' : ''"
      >{{ displayValue(item.value) }}</span>
      <span class="flex items-center gap-0.5 opacity-60 group-hover/field:opacity-100">
        <template v-if="canFilterField(item.field) && !item.isObject">
          <button
            type="button"
            class="rounded p-0.5 text-muted-foreground hover:bg-background hover:text-foreground"
            :title="filterTitle(item.value, 'include')"
            :aria-label="`${filterTitle(item.value, 'include')}: ${item.field}`"
            @mousedown.prevent
            @click.stop="filter(item, $event, 'include')"
          ><Plus class="h-3 w-3" /></button>
          <button
            type="button"
            class="rounded p-0.5 text-muted-foreground hover:bg-background hover:text-foreground"
            :title="filterTitle(item.value, 'exclude')"
            :aria-label="`${filterTitle(item.value, 'exclude')}: ${item.field}`"
            @mousedown.prevent
            @click.stop="filter(item, $event, 'exclude')"
          ><Minus class="h-3 w-3" /></button>
        </template>
        <button
          v-if="!item.isObject && (item.nested ? canFilter : !shownColumns.includes(item.field))"
          type="button"
          class="rounded p-0.5 text-muted-foreground hover:bg-background hover:text-foreground"
          :title="t('explore.addAsColumn')"
          :aria-label="`${t('explore.addAsColumn')}: ${item.field}`"
          @click.stop="emit('add-column', { field: item.field, nested: item.nested })"
        ><Columns3 class="h-3 w-3" /></button>
        <button
          type="button"
          class="rounded p-0.5 text-muted-foreground hover:bg-background hover:text-foreground"
          :title="t('ui.copyValue')"
          :aria-label="`${t('ui.copyValue')}: ${item.field}`"
          @click.stop="copy(item.value)"
        ><Copy class="h-3 w-3" /></button>
      </span>
    </div>
  </div>
</template>
